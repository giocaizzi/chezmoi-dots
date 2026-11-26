package chez

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"slices"
	"strings"
)

// Overrides is data layered over a configuration (chezmoi --override-data).
type Overrides map[string]any

// Entry is one target of the target state (chezmoi dump).
type Entry struct {
	Type     string `json:"type"`
	Contents string `json:"contents"`
	Linkname string `json:"linkname"`
	Raw      string `json:"-"` // the entry's JSON, which decides whether two states differ
}

// State is the target state of a configuration: target path to entry.
type State map[string]Entry

// Client calls chezmoi. The zero value is not usable; build one with New.
type Client struct {
	run    Runner
	bin    string
	global []string
}

// New returns a client that runs bin through r with global flags (--config,
// --source, ...) before every subcommand.
func New(r Runner, bin string, global ...string) *Client {
	return &Client{run: r, bin: bin, global: global}
}

// With returns a client with more global flags; later flags win.
func (c *Client) With(global ...string) *Client {
	n := *c
	n.global = append(append([]string(nil), c.global...), global...)
	return &n
}

// in returns the client targeting configuration file cfg, or c itself when cfg is
// empty. The file replaces the client's own config (the last --config wins), so its
// data reaches templates as chezmoi init would store it.
func (c *Client) in(cfg string) *Client {
	if cfg == "" {
		return c
	}
	return c.With("--config", cfg)
}

// render marks a read-only render of the source: .chezmoiexternal.toml.tmpl then
// declares no externals, so nothing is downloaded for a preview.
func render(o Overrides) Overrides { return o.Clone(Overrides{"dotsRender": true}) }

func (c *Client) args(o Overrides, sub ...string) ([]string, error) {
	a := append([]string{c.bin}, c.global...)
	if len(o) > 0 {
		j, err := json.Marshal(o)
		if err != nil {
			return nil, fmt.Errorf("encode overrides: %w", err)
		}
		a = append(a, "--override-data", string(j))
	}
	return append(a, sub...), nil
}

func (c *Client) output(ctx context.Context, o Overrides, stdin io.Reader, sub ...string) ([]byte, error) {
	a, err := c.args(o, sub...)
	if err != nil {
		return nil, err
	}
	out, err := Output(ctx, c.run, Cmd{Args: a, Stdin: stdin})
	if err != nil {
		return nil, fmt.Errorf("chezmoi %s: %w", sub[0], err)
	}
	return out, nil
}

func (c *Client) stream(ctx context.Context, stdout, stderr io.Writer, stdin io.Reader, sub ...string) error {
	a, err := c.args(nil, sub...)
	if err != nil {
		return err
	}
	return c.run.Run(ctx, Cmd{Args: a, Stdin: stdin, Stdout: stdout, Stderr: stderr})
}

// Data returns the template data (chezmoi data).
func (c *Client) Data(ctx context.Context, o Overrides) (map[string]any, error) {
	out, err := c.output(ctx, render(o), nil, "data", "--format", "json")
	if err != nil {
		return nil, err
	}
	var m map[string]any
	if err := json.Unmarshal(out, &m); err != nil {
		return nil, fmt.Errorf("decode chezmoi data: %w", err)
	}
	return m, nil
}

// Dump returns the target state without directories and externals, rendered with
// configuration file cfg (empty: the client's own) and the overrides o.
func (c *Client) Dump(ctx context.Context, cfg string, o Overrides) (State, error) {
	out, err := c.in(cfg).output(ctx, render(o), nil, "dump", "--exclude", "dirs,externals", "--format", "json")
	if err != nil {
		return nil, err
	}
	return DecodeState(out)
}

// DecodeState decodes chezmoi dump --format json.
func DecodeState(b []byte) (State, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil {
		return nil, fmt.Errorf("decode chezmoi dump: %w", err)
	}
	s := make(State, len(raw))
	for k, v := range raw {
		var e Entry
		if err := json.Unmarshal(v, &e); err != nil {
			return nil, fmt.Errorf("decode chezmoi dump entry %s: %w", k, err)
		}
		e.Raw = string(v)
		s[k] = e
	}
	return s, nil
}

// Changed lists, sorted, the targets whose entry differs between two states.
func Changed(before, after State) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range []State{before, after} {
		for k := range s {
			if seen[k] {
				continue
			}
			seen[k] = true
			if before[k].Raw != after[k].Raw {
				out = append(out, k)
			}
		}
	}
	slices.Sort(out)
	return out
}

// Managed maps each managed target to its source path relative to the source dir.
func (c *Client) Managed(ctx context.Context, cfg string, o Overrides) (map[string]string, error) {
	out, err := c.in(cfg).output(ctx, render(o), nil, "managed", "-i", "files,symlinks,scripts", "-x", "externals", "-p", "all", "-f", "json")
	if err != nil {
		return nil, err
	}
	var raw map[string]struct {
		SourceRelative string `json:"sourceRelative"`
	}
	if err := json.Unmarshal(out, &raw); err != nil {
		return nil, fmt.Errorf("decode chezmoi managed: %w", err)
	}
	m := make(map[string]string, len(raw))
	for k, v := range raw {
		m[k] = v.SourceRelative
	}
	return m, nil
}

// Cat returns what chezmoi would write for target.
func (c *Client) Cat(ctx context.Context, target, cfg string, o Overrides) ([]byte, error) {
	return c.in(cfg).output(ctx, render(o), nil, "cat", target)
}

// SourcePath returns the source dir (target empty) or a target's source file.
func (c *Client) SourcePath(ctx context.Context, target, cfg string, o Overrides) (string, error) {
	sub := []string{"source-path"}
	if target != "" {
		sub = append(sub, target)
	}
	out, err := c.in(cfg).output(ctx, render(o), nil, sub...)
	return strings.TrimSpace(string(out)), err
}

// TargetPath returns the destination dir, normally the home directory.
func (c *Client) TargetPath(ctx context.Context) (string, error) {
	out, err := c.output(ctx, nil, nil, "target-path")
	return strings.TrimSpace(string(out)), err
}

// EditConfig contains the effective chezmoi settings used when editing source files.
type EditConfig struct {
	WorkingTree string `json:"workingTree"`
	Edit        struct {
		Apply bool `json:"apply"`
	} `json:"edit"`
	Git struct {
		AutoAdd    bool `json:"autoadd"`
		AutoCommit bool `json:"autocommit"`
		AutoPush   bool `json:"autopush"`
	} `json:"git"`
}

// EditConfig returns the settings from chezmoi dump-config.
func (c *Client) EditConfig(ctx context.Context) (EditConfig, error) {
	out, err := c.output(ctx, nil, nil, "dump-config", "--format", "json")
	if err != nil {
		return EditConfig{}, err
	}
	var cfg EditConfig
	if err := json.Unmarshal(out, &cfg); err != nil {
		return EditConfig{}, fmt.Errorf("decode chezmoi dump-config: %w", err)
	}
	return cfg, nil
}

// WorkingTree returns the root of the git repository that holds the source dir.
func (c *Client) WorkingTree(ctx context.Context) (string, error) {
	cfg, err := c.EditConfig(ctx)
	return cfg.WorkingTree, err
}

// Status returns chezmoi status for targets (empty when they are up to date).
func (c *Client) Status(ctx context.Context, targets ...string) (string, error) {
	out, err := c.output(ctx, nil, nil, append([]string{"status"}, targets...)...)
	// Trim only the end: a line's first column may be a space.
	return strings.TrimRight(string(out), "\r\n"), err
}

// Diff writes chezmoi diff for targets to w.
func (c *Client) Diff(ctx context.Context, w, errw io.Writer, targets ...string) error {
	return c.stream(ctx, w, errw, nil, append([]string{"diff"}, targets...)...)
}

// Apply applies targets.
func (c *Client) Apply(ctx context.Context, stdout, stderr io.Writer, targets ...string) error {
	return c.stream(ctx, stdout, stderr, nil, append([]string{"apply"}, targets...)...)
}

// Add adds target to the source state; flags (--template, --encrypt) go before it.
func (c *Client) Add(ctx context.Context, stdout, stderr io.Writer, target string, flags ...string) error {
	sub := append(append([]string{"add"}, flags...), target)
	return c.stream(ctx, stdout, stderr, nil, sub...)
}

// AddPlan returns the source path, relative to the source dir, that chezmoi add
// would create for target, without writing or committing anything.
func (c *Client) AddPlan(ctx context.Context, target string, flags ...string) (string, error) {
	sub := append(append([]string{"--dry-run", "--verbose", "add"}, flags...), target)
	out, err := c.output(ctx, nil, nil, sub...)
	if err != nil {
		return "", err
	}
	return parseAddPlan(string(out), target)
}

// parseAddPlan reads the git-style diff of chezmoi --dry-run --verbose add: each
// "diff --git a/<p> b/<p>" header is followed by "new file mode <mode>", where a
// directory has mode 40755 and a file 100xxx. The one file is the source path.
func parseAddPlan(out, target string) (string, error) {
	const sep = " b/"
	var files []string
	var path string
	for line := range strings.Lines(out) {
		line = strings.TrimRight(line, "\r\n")
		if rest, ok := strings.CutPrefix(line, "diff --git a/"); ok {
			path = ""
			if n := (len(rest) - len(sep)) / 2; n > 0 && len(rest) == 2*n+len(sep) && rest[:n] == rest[n+len(sep):] {
				path = rest[:n]
			}
			continue
		}
		if mode, ok := strings.CutPrefix(line, "new file mode "); ok && path != "" && strings.HasPrefix(mode, "100") {
			files = append(files, path)
			path = ""
		}
	}
	if len(files) != 1 {
		return "", fmt.Errorf("cannot tell the source path chezmoi add would create for %s", target)
	}
	return files[0], nil
}

// Encrypt encrypts plain with the configured encryption, armored as chezmoi writes it.
func (c *Client) Encrypt(ctx context.Context, plain []byte) ([]byte, error) {
	return c.output(ctx, nil, bytes.NewReader(plain), "encrypt")
}

// Decrypt decrypts the encrypted file at path.
func (c *Client) Decrypt(ctx context.Context, path string) ([]byte, error) {
	return c.output(ctx, nil, nil, "decrypt", path)
}

// Execute renders a template read from tmpl.
func (c *Client) Execute(ctx context.Context, o Overrides, tmpl []byte, init bool) ([]byte, error) {
	sub := []string{"execute-template"}
	if init {
		sub = append(sub, "--init", "--no-tty")
	}
	return c.output(ctx, render(o), bytes.NewReader(tmpl), sub...)
}

// Clone returns a copy of o with the entries of more added.
func (o Overrides) Clone(more Overrides) Overrides {
	n := Overrides{}
	maps.Copy(n, o)
	maps.Copy(n, more)
	return n
}
