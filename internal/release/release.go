// Package release finds newer versions of pinned release binaries and the checksums
// of their downloads.
package release

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/giocaizzi/chezmoi-dots/internal/archive"
	"github.com/giocaizzi/chezmoi-dots/internal/chez"
	"github.com/giocaizzi/chezmoi-dots/internal/pkgreg"
)

// Finder looks up the newest published version and checksums downloads.
type Finder struct {
	Runner chez.Runner         // runs git ls-remote
	HTTP   *http.Client        // go.dev and the downloads; needs a timeout
	Getenv func(string) string // nil reads nothing

	MaxBytes        int64         // largest download SHA256 accepts; 0 means 1 GiB
	DownloadTimeout time.Duration // per download, body included; 0 means 5 minutes
}

const (
	defaultMaxBytes        = 1 << 30
	defaultDownloadTimeout = 5 * time.Minute
)

func (f Finder) maxBytes() int64 {
	if f.MaxBytes > 0 {
		return f.MaxBytes
	}
	return defaultMaxBytes
}

func (f Finder) downloadTimeout() time.Duration {
	if f.DownloadTimeout > 0 {
		return f.DownloadTimeout
	}
	return defaultDownloadTimeout
}

const gitBin = "git"

var commit = regexp.MustCompile(`^[0-9a-f]{40}$`)

var numeric = regexp.MustCompile(`^[0-9]+(\.[0-9]+){1,2}$`)

// Latest returns the newest stable version published upstream.
func (f Finder) Latest(ctx context.Context, up pkgreg.Upstream) (string, error) {
	switch up.Kind {
	case "git":
		out, err := chez.Output(ctx, f.Runner, chez.Cmd{
			Args: []string{gitBin, "ls-remote", "--tags", "--refs", up.Repo},
			Env:  chez.GitNoPromptEnv(ctx, f.Runner, f.Getenv, ""), // never ask for credentials or a passphrase
		})
		if err != nil {
			return "", fmt.Errorf("list tags of %s: %w", up.Repo, err)
		}
		return newestTag(string(out), up.TagPrefix)
	case "godev":
		return f.latestGo(ctx)
	default:
		return "", fmt.Errorf("unknown upstream kind %q", up.Kind)
	}
}

// Head returns the commit the default branch (HEAD) of remote points at.
func (f Finder) Head(ctx context.Context, remote string) (string, error) {
	out, err := chez.Output(ctx, f.Runner, chez.Cmd{
		Args: []string{gitBin, "ls-remote", remote, "HEAD"},
		Env:  chez.GitNoPromptEnv(ctx, f.Runner, f.Getenv, ""), // never ask for credentials or a passphrase
	})
	if err != nil {
		return "", fmt.Errorf("read HEAD of %s: %w", remote, err)
	}
	for line := range strings.SplitSeq(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[1] == "HEAD" && commit.MatchString(fields[0]) {
			return fields[0], nil
		}
	}
	return "", fmt.Errorf("no HEAD in git ls-remote of %s", remote)
}

// newestTag picks the highest numeric version among tags named <prefix><version>.
func newestTag(lsRemote, prefix string) (string, error) {
	best := ""
	for line := range strings.SplitSeq(lsRemote, "\n") {
		_, ref, ok := strings.Cut(strings.TrimSpace(line), "\trefs/tags/")
		if !ok || !strings.HasPrefix(ref, prefix) {
			continue
		}
		v := strings.TrimPrefix(ref, prefix)
		if numeric.MatchString(v) && (best == "" || pkgreg.Compare(v, best) > 0) {
			best = v
		}
	}
	if best == "" {
		return "", fmt.Errorf("no %s<version> tag", prefix)
	}
	return best, nil
}

func (f Finder) latestGo(ctx context.Context) (string, error) {
	body, err := f.get(ctx, "https://go.dev/dl/?mode=json")
	if err != nil {
		return "", err
	}
	defer body.Close()
	var rels []struct {
		Version string `json:"version"`
		Stable  bool   `json:"stable"`
	}
	if err := json.NewDecoder(body).Decode(&rels); err != nil {
		return "", fmt.Errorf("decode go.dev releases: %w", err)
	}
	for _, r := range rels {
		if v := strings.TrimPrefix(r.Version, "go"); r.Stable && numeric.MatchString(v) {
			return v, nil
		}
	}
	return "", errors.New("go.dev lists no stable release")
}

// SHA256 downloads url and returns the SHA-256 of its body. It fails when the
// body exceeds MaxBytes or the whole download outlasts DownloadTimeout.
func (f Finder) SHA256(ctx context.Context, url string) (string, error) {
	sum, _, err := f.fetch(ctx, url, nil)
	return sum, err
}

// countWriter counts the bytes written through it.
type countWriter struct {
	w io.Writer
	n int64
}

func (c *countWriter) Write(p []byte) (int, error) {
	n, err := c.w.Write(p)
	c.n += int64(n)
	return n, err //nolint:wrapcheck // passes the writer's own error through
}

// fetch downloads url and returns the SHA-256 of its body. When consume is set it
// reads the body through consume first; the rest of the body is then drained into the
// hash, so the sum covers the whole body whatever consume read. consume's own error
// is returned apart from fetch's: the download still succeeds.
func (f Finder) fetch(ctx context.Context, url string, consume func(io.Reader) error) (sum string, consumeErr, err error) {
	ctx, cancel := context.WithTimeout(ctx, f.downloadTimeout())
	defer cancel()
	resp, err := f.do(ctx, url)
	if err != nil {
		return "", nil, err
	}
	defer resp.Body.Close()
	limit := f.maxBytes()
	if resp.ContentLength > limit {
		return "", nil, fmt.Errorf("download %s: larger than %d bytes", url, limit)
	}
	h := sha256.New()
	cw := &countWriter{w: h}
	body := io.TeeReader(io.LimitReader(resp.Body, limit+1), cw)
	if consume != nil {
		consumeErr = consume(body)
	}
	if _, err := io.Copy(io.Discard, body); err != nil {
		return "", nil, fmt.Errorf("download %s: %w", url, err)
	}
	if cw.n > limit {
		return "", nil, fmt.Errorf("download %s: larger than %d bytes", url, limit)
	}
	return hex.EncodeToString(h.Sum(nil)), consumeErr, nil
}

func (f Finder) get(ctx context.Context, url string) (io.ReadCloser, error) {
	resp, err := f.do(ctx, url)
	if err != nil {
		return nil, err
	}
	return resp.Body, nil
}

func (f Finder) do(ctx context.Context, url string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("request %s: %w", url, err)
	}
	resp, err := f.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("get %s: %w", url, err)
	}
	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		return nil, fmt.Errorf("get %s: %s", url, resp.Status)
	}
	return resp, nil
}

// Status is the outcome of looking up one release.
type Status struct {
	Release pkgreg.Release
	Latest  string
	Err     error
}

// Outdated reports whether a newer version exists.
func (s Status) Outdated() bool {
	return s.Err == nil && pkgreg.Compare(s.Latest, s.Release.Version) > 0
}

// Check looks up the newest version of every release, a few at a time.
func (f Finder) Check(ctx context.Context, rels []pkgreg.Release) []Status {
	out := make([]Status, len(rels))
	var g errgroup.Group
	g.SetLimit(6)
	for i, r := range rels {
		g.Go(func() error {
			out[i] = f.status(ctx, r)
			return nil
		})
	}
	_ = g.Wait()
	return out
}

// status looks up one release; a failure is recorded in the Status, not returned.
func (f Finder) status(ctx context.Context, r pkgreg.Release) Status {
	st := Status{Release: r}
	up, err := r.Upstream()
	if err != nil {
		st.Err = err
		return st
	}
	st.Latest, st.Err = f.Latest(ctx, up)
	return st
}

// Sums downloads version of r for every platform and returns the checksums.
func (f Finder) Sums(ctx context.Context, r pkgreg.Release, version string) (map[string]string, error) {
	sums, _, err := f.inspect(ctx, r, version, false)
	return sums, err
}

// Inspect downloads version of r for every platform once and returns the checksums
// and, for the platforms whose archive is a .tar.gz, .tgz or .zip, the regular files
// inside it. files[platform] is nil when the archive is of another kind or cannot be
// read, including one over the archive package's limits (name length, decompressed
// size, zip spool size); the checksum is of the whole download either way, because
// fetch drains the body after the listing stops. Zips are spooled one at a time.
func (f Finder) Inspect(ctx context.Context, r pkgreg.Release, version string) (sums map[string]string, files map[string][]archive.File, err error) {
	return f.inspect(ctx, r, version, true)
}

func (f Finder) inspect(ctx context.Context, r pkgreg.Release, version string, list bool) (map[string]string, map[string][]archive.File, error) {
	platforms := r.Platforms()
	sums := make([]string, len(platforms))
	listed := make([][]archive.File, len(platforms))
	g, ctx := errgroup.WithContext(ctx)
	g.SetLimit(3)
	for i, p := range platforms {
		g.Go(func() error {
			url := r.AssetURL(version, p)
			var consume func(io.Reader) error
			if kind := archive.KindOf(url); list && kind != archive.None {
				consume = func(body io.Reader) error {
					var err error
					listed[i], err = archive.List(kind, body)
					return err
				}
			}
			s, cerr, err := f.fetch(ctx, url, consume)
			if cerr != nil {
				listed[i] = nil
			}
			sums[i] = s
			return err
		})
	}
	if err := g.Wait(); err != nil {
		return nil, nil, fmt.Errorf("checksum %s %s: %w", r.Name, version, err)
	}
	out := make(map[string]string, len(platforms))
	files := make(map[string][]archive.File, len(platforms))
	for i, p := range platforms {
		out[p] = sums[i]
		files[p] = listed[i]
	}
	return out, files, nil
}
