package vaultcheck

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/giocaizzi/chezmoi-dots/internal/chez"
	"github.com/giocaizzi/chezmoi-dots/internal/finding"
)

const defaultTimeout = 15 * time.Second

// State is the outcome of probing one entry.
type State int

// The states, from best to worst.
const (
	OK State = iota
	Warn
	Fail
)

func (s State) String() string {
	switch s {
	case OK:
		return "ok"
	case Warn:
		return "warn"
	case Fail:
		return "fail"
	}
	return fmt.Sprintf("State(%d)", int(s))
}

// Result is the probe of one entry.
type Result struct {
	Name, Kind string
	State      State
	Detail     string // one line, never a secret value, token or account email
	Hint       string // what to do next; empty for ok
}

// Keyring reads one secret from the OS keyring. The value is only held in memory.
type Keyring interface {
	Get(ctx context.Context, service, user string) (string, error)
}

// ChezmoiKeyring reads it natively with `<chezmoi...> secret keyring get --service S --user U`.
type ChezmoiKeyring struct {
	Runner  chez.Runner
	Chezmoi []string // the chezmoi binary and its global args (--config, --source)
}

// Get implements Keyring. Its error carries chezmoi's stderr, never its stdout.
func (k ChezmoiKeyring) Get(ctx context.Context, service, user string) (string, error) {
	var stdout, stderr bytes.Buffer
	args := append(append([]string{}, k.Chezmoi...), "secret", "keyring", "get", "--service", service, "--user", user)
	if err := k.Runner.Run(ctx, chez.Cmd{Args: args, Stdout: &stdout, Stderr: &stderr}); err != nil {
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return "", fmt.Errorf("chezmoi secret keyring get: %w: %s", err, msg)
		}
		return "", fmt.Errorf("chezmoi secret keyring get: %w", err)
	}
	v := strings.TrimRight(stdout.String(), "\r\n")
	if v == "" {
		return "", errors.New("empty")
	}
	return v, nil
}

// Prober checks the declared secret managers without changing anything.
type Prober struct {
	Runner   chez.Runner
	LookPath func(string) (string, error)
	Keyring  Keyring
	Home     string        // expands a leading ~/ in command
	Timeout  time.Duration // per CLI call; 0 means 15s
}

// CheckAll parses data and checks every entry in name order; findings non-empty means no results.
func (p Prober) CheckAll(ctx context.Context, data map[string]any) ([]Result, []finding.Finding) {
	entries, fs := Parse(data)
	if len(fs) > 0 {
		return nil, fs
	}
	out := make([]Result, 0, len(entries))
	for _, e := range entries {
		out = append(out, p.Check(ctx, e, data))
	}
	return out, nil
}

// Check probes one entry; data is this machine's chezmoi data (answers for projectFrom).
func (p Prober) Check(ctx context.Context, e Entry, data map[string]any) Result {
	r := Result{Name: e.Name, Kind: e.Kind}
	project := e.Project
	if e.Kind == KindBws && e.ProjectFrom != "" {
		v, present := data[e.ProjectFrom]
		s, isStr := v.(string)
		if present && !isStr {
			return r.with(Fail, "answer "+e.ProjectFrom+" is not a string", "")
		}
		if s == "" {
			return r.with(OK, "not configured (answer "+e.ProjectFrom+" is empty)", "")
		}
		project = s
	}
	bin, name, err := resolveCLI(e, p.Home, p.LookPath)
	if err != nil {
		return r.with(Fail, name+" not found", "install "+e.Kind+" or set command in .chezmoidata/secrets.toml")
	}
	switch e.Kind {
	case KindBws:
		return p.checkBws(ctx, r, e, bin, project)
	case KindBw:
		return p.checkBw(ctx, r, bin)
	default:
		return p.checkRbw(ctx, r, bin)
	}
}

// resolveCLI finds the entry's CLI: its declared command (a leading ~/ is the home
// directory) or else the kind's name, looked up on PATH. name is what was looked up.
func resolveCLI(e Entry, home string, lookPath func(string) (string, error)) (bin, name string, err error) {
	name = e.Command
	if name == "" {
		name = e.Kind
	} else if rest, ok := strings.CutPrefix(name, "~/"); ok {
		name = filepath.Join(home, rest)
	}
	bin, err = lookPath(name)
	return bin, name, err
}

func (r Result) with(s State, detail, hint string) Result {
	r.State, r.Detail, r.Hint = s, detail, hint
	return r
}

func (p Prober) timeout() time.Duration {
	if p.Timeout > 0 {
		return p.Timeout
	}
	return defaultTimeout
}

// call is the outcome of one CLI run. Only stderr is kept for errors.
type call struct {
	stdout   []byte
	stderr   string
	err      error
	timedOut bool
}

func (p Prober) run(ctx context.Context, argv, env []string, keepStdout bool) call {
	cctx, cancel := context.WithTimeout(ctx, p.timeout())
	defer cancel()
	var stdout, stderr bytes.Buffer
	c := chez.Cmd{Args: argv, Env: env, Stderr: &stderr}
	if keepStdout {
		c.Stdout = &stdout
	}
	err := p.Runner.Run(cctx, c)
	out := call{err: err, stderr: stderr.String()}
	if keepStdout {
		out.stdout = stdout.Bytes()
	}
	out.timedOut = err != nil && errors.Is(cctx.Err(), context.DeadlineExceeded)
	return out
}

// firstLine is the first non-empty line of s with every secret replaced.
func firstLine(s string, secrets ...string) string {
	for line := range strings.SplitSeq(s, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		for _, sec := range secrets {
			if sec != "" {
				line = strings.ReplaceAll(line, sec, "[redacted]")
			}
		}
		return line
	}
	return ""
}

func networkMarkers() []string {
	return []string{"error sending request", "dns", "connect", "network", "timed out", "temporary failure", "unreachable"}
}

func looksLikeNetwork(stderr string) bool {
	low := strings.ToLower(stderr)
	for _, m := range networkMarkers() {
		if strings.Contains(low, m) {
			return true
		}
	}
	return false
}

func (p Prober) timedOut(r Result) Result {
	return r.with(Warn, "unreachable: timed out after "+p.timeout().String(), "check the network and rerun")
}

func (p Prober) checkBws(ctx context.Context, r Result, e Entry, bin, project string) Result {
	token, err := p.Keyring.Get(ctx, e.Service, e.User)
	if err != nil || token == "" {
		return r.with(Fail, fmt.Sprintf("no access token in the keyring (service %s, user %s)", e.Service, e.User),
			fmt.Sprintf("chezmoi secret keyring set --service=%s --user=%s", e.Service, e.User))
	}
	c := p.run(ctx, []string{bin, keyProject, "get", project, "--output", "none", "--color", "no"},
		[]string{"BWS_ACCESS_TOKEN=" + token}, false)
	switch {
	case c.err == nil:
		return r.with(OK, "project "+project+" reachable", "")
	case c.timedOut:
		return p.timedOut(r)
	case looksLikeNetwork(c.stderr):
		return r.with(Warn, "unreachable: "+firstLine(c.stderr, token), "check the network and rerun")
	}
	return r.with(Fail, "bws project get failed: "+firstLine(c.stderr, token),
		"check the token and that its machine account can read project "+project)
}

func (p Prober) checkBw(ctx context.Context, r Result, bin string) Result {
	c := p.run(ctx, []string{bin, "status"}, nil, true)
	if c.timedOut {
		return p.timedOut(r)
	}
	fail := func() Result {
		msg := firstLine(c.stderr)
		if msg == "" {
			msg = "unexpected output"
		}
		return r.with(Fail, "bw status failed: "+msg, "")
	}
	if c.err != nil {
		return fail()
	}
	var st struct {
		Status string `json:"status"`
	}
	if json.Unmarshal(c.stdout, &st) != nil {
		c.stderr = ""
		return fail()
	}
	switch st.Status {
	case "unlocked":
		return r.with(OK, "vault unlocked", "")
	case "locked":
		return r.with(Warn, "vault locked", "bw unlock, then export BW_SESSION")
	case "unauthenticated":
		return r.with(Fail, "not logged in", "bw login")
	}
	c.stderr = ""
	return fail()
}

func (p Prober) checkRbw(ctx context.Context, r Result, bin string) Result {
	c := p.run(ctx, []string{bin, "unlocked"}, nil, false)
	switch {
	case c.err == nil:
		return r.with(OK, "agent unlocked", "")
	case c.timedOut:
		return p.timedOut(r)
	}
	return r.with(Warn, "locked or agent not running", "rbw unlock")
}
