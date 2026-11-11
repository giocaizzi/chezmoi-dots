package edit

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"testing/fstest"

	"github.com/giocaizzi/chezmoi-dots/internal/chez"
	"github.com/giocaizzi/chezmoi-dots/internal/matrix"
	"github.com/giocaizzi/chezmoi-dots/internal/registry"
	"github.com/giocaizzi/chezmoi-dots/internal/term"
	"github.com/giocaizzi/chezmoi-dots/internal/tree"
)

// fakeCZ renders a state per call: states is consumed in order, then the last repeats.
type fakeCZ struct {
	home, wt  string
	mu        sync.Mutex
	dumpErr   map[string]map[int]error // "here"/"conf" to call number (0-based) to failure
	here      []chez.State             // answers of the plain dump, in order, then the last repeats
	conf      []chez.State             // the same for dumps with overrides
	n         map[string]int
	status    string
	applied   []string
	diffed    []string
	encrypted []byte
	plain     []byte
	config    chez.EditConfig
	applyErr  error
}

func (f *fakeCZ) next(kind string, states []chez.State) (chez.State, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.n == nil {
		f.n = map[string]int{}
	}
	i := f.n[kind]
	f.n[kind]++
	if err := f.dumpErr[kind][i]; err != nil {
		return nil, err
	}
	return states[min(i, len(states)-1)], nil
}

func (f *fakeCZ) SourcePath(context.Context, string, chez.Overrides) (string, error) {
	return "", errors.New("not managed")
}
func (f *fakeCZ) TargetPath(context.Context) (string, error) { return f.home, nil }
func (f *fakeCZ) EditConfig(context.Context) (chez.EditConfig, error) {
	cfg := f.config
	cfg.WorkingTree = f.wt
	return cfg, nil
}

func (f *fakeCZ) Dump(_ context.Context, o chez.Overrides) (chez.State, error) {
	if o == nil {
		return f.next("here", f.here)
	}
	return f.next("conf", f.conf)
}

func (f *fakeCZ) Managed(context.Context, chez.Overrides) (map[string]string, error) {
	return map[string]string{}, nil
}
func (f *fakeCZ) Cat(context.Context, string, chez.Overrides) ([]byte, error) { return nil, nil }
func (f *fakeCZ) Status(context.Context, ...string) (string, error)           { return f.status, nil }

func (f *fakeCZ) Diff(_ context.Context, _, _ io.Writer, t ...string) error {
	f.diffed = t
	return nil
}

func (f *fakeCZ) Apply(_ context.Context, _, _ io.Writer, t ...string) error {
	f.applied = t
	return f.applyErr
}

func (f *fakeCZ) Encrypt(_ context.Context, plain []byte) ([]byte, error) {
	f.encrypted = plain
	return append([]byte("ENC:"), plain...), nil
}
func (f *fakeCZ) Decrypt(context.Context, string) ([]byte, error) { return f.plain, nil }

type fakeEditor struct {
	write string // what the "user" saves; empty leaves the file alone
	err   error
	got   string
}

func (e *fakeEditor) Edit(_ context.Context, path string) error {
	e.got = path
	if e.err != nil {
		return e.err
	}
	if e.write != "" {
		return os.WriteFile(path, []byte(e.write), 0o600)
	}
	return nil
}

type fakeChecker struct {
	err  error
	argv []string
}

func (c *fakeChecker) Check(_ context.Context, _ string, argv []string) error {
	c.argv = argv
	return c.err
}

type fakePrompt struct {
	yes bool
	ask int
}

func (p *fakePrompt) Confirm(string, bool) (bool, error) { p.ask++; return p.yes, nil }
func (p *fakePrompt) Pick(context.Context, string, []string) (string, error) {
	return "", nil
}

type fakeGit struct {
	actions    []string
	fileStatus []string
	pushErr    error
}

func (*fakeGit) Status(context.Context, string) (string, error) { return "", nil }
func (g *fakeGit) FileStatus(context.Context, string, string) (string, error) {
	status := g.fileStatus[0]
	g.fileStatus = g.fileStatus[1:]
	return status, nil
}

func (g *fakeGit) Stage(_ context.Context, _, path string) error {
	g.actions = append(g.actions, "stage "+path)
	return nil
}

func (g *fakeGit) Commit(_ context.Context, _, path string) error {
	g.actions = append(g.actions, "commit "+path)
	return nil
}

func (g *fakeGit) Push(context.Context, string) error {
	g.actions = append(g.actions, "push")
	return g.pushErr
}

type rig struct {
	svc  *Service
	cz   *fakeCZ
	ed   *fakeEditor
	chk  *fakeChecker
	pr   *fakePrompt
	git  *fakeGit
	src  string
	errw *bytes.Buffer
}

func newRig(t *testing.T, e registry.Entry, states ...chez.State) *rig {
	t.Helper()
	src := t.TempDir()
	if e.Source != "" {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(src, e.Source)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(src, e.Source), []byte("old"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	fields := map[string]any{"desc": e.Desc}
	if e.Source != "" {
		fields["source"] = e.Source
	}
	if e.Local != "" {
		fields["local"] = e.Local
	}
	if len(e.Check) > 0 {
		fields["check"] = anys(e.Check)
	}
	if e.Note != "" {
		fields["note"] = e.Note
	}
	reg, fs := registry.Parse(map[string]any{"edit": map[string]any{e.Name: fields}})
	if len(fs) != 0 {
		t.Fatal(fs)
	}
	r := &rig{
		cz:   &fakeCZ{home: t.TempDir(), wt: src, here: states, conf: states, status: "M x"},
		ed:   &fakeEditor{write: "new"},
		chk:  &fakeChecker{},
		pr:   &fakePrompt{yes: true},
		git:  &fakeGit{fileStatus: []string{" M source", "M  source"}},
		src:  src,
		errw: &bytes.Buffer{},
	}
	r.svc = &Service{
		CZ: r.cz, Registry: reg,
		Matrix: func(context.Context, string) ([]matrix.Entry, error) {
			return []matrix.Entry{{OS: "linux", Role: "work", Overrides: chez.Overrides{"work": true}}}, nil
		},
		Tree: tree.New(fstest.MapFS{}), Editor: r.ed, Check: r.chk, Prompt: r.pr, Git: r.git, Src: src,
		Out: io.Discard, Err: r.errw, Style: term.Style{},
	}
	return r
}

func st(contents string) chez.State {
	return chez.State{"x": {Type: "file", Contents: contents, Raw: contents}}
}

func TestEditAppliesOnConfirm(t *testing.T) {
	t.Parallel()
	r := newRig(t, registry.Entry{Name: "a", Desc: "d", Source: "a.md", Check: []string{"lint", "{file}"}, Note: "reload"}, st("1"), st("2"))
	if err := r.svc.Run(t.Context(), "a"); err != nil {
		t.Fatalf("Run() = %v", err)
	}
	if b, _ := os.ReadFile(filepath.Join(r.src, "a.md")); string(b) != "new" {
		t.Errorf("source = %q, want the edit", b)
	}
	if want := filepath.Join(r.src, "a.md"); len(r.chk.argv) != 2 || r.chk.argv[1] != want {
		t.Errorf("check argv = %v, want the edited file substituted", r.chk.argv)
	}
	if len(r.cz.applied) != 1 || !strings.HasSuffix(r.cz.applied[0], "x") {
		t.Errorf("applied = %v, want the changed target", r.cz.applied)
	}
	if !strings.Contains(r.errw.String(), "reload") {
		t.Errorf("stderr = %q, want the entry's note", r.errw.String())
	}
}

func TestEditDeclinedAppliesNothing(t *testing.T) {
	t.Parallel()
	r := newRig(t, registry.Entry{Name: "a", Desc: "d", Source: "a.md"}, st("1"), st("2"))
	r.pr.yes = false
	if err := r.svc.Run(t.Context(), "a"); err != nil {
		t.Fatalf("Run() = %v", err)
	}
	if r.cz.applied != nil {
		t.Errorf("applied = %v, want nothing", r.cz.applied)
	}
}

func TestEditGitSettings(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		add      bool
		commit   bool
		push     bool
		noEdit   bool
		reverted bool
		want     []string
	}{
		{name: "manual"},
		{name: "stage", add: true, want: []string{"stage home/a.md"}},
		{name: "commit", commit: true, want: []string{"stage home/a.md", "commit home/a.md"}},
		{name: "push implies commit", push: true, want: []string{"stage home/a.md", "commit home/a.md", "push"}},
		{name: "editor saved nothing", push: true, noEdit: true},
		{name: "matches HEAD after staging", push: true, reverted: true, want: []string{"stage home/a.md"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r := newRig(t, registry.Entry{Name: "a", Desc: "d", Source: "home/a.md"}, st("1"), st("2"))
			r.svc.Src = filepath.Join(r.src, "home")
			r.cz.config.Edit.Apply = true
			r.cz.config.Git.AutoAdd = tt.add
			r.cz.config.Git.AutoCommit = tt.commit
			r.cz.config.Git.AutoPush = tt.push
			if tt.noEdit {
				r.ed.write = ""
			}
			if tt.reverted {
				r.git.fileStatus[1] = ""
			}
			if err := r.svc.Run(t.Context(), "a.md"); err != nil {
				t.Fatalf("Run() = %v", err)
			}
			wantTargets := []string{filepath.Join(r.cz.home, "x")}
			if r.pr.ask != 0 || !slices.Equal(r.cz.diffed, wantTargets) || !slices.Equal(r.cz.applied, wantTargets) {
				t.Errorf("prompt, diff and apply = %d, %v, %v, want 0 and %v for diff and apply", r.pr.ask, r.cz.diffed, r.cz.applied, wantTargets)
			}
			if !slices.Equal(r.git.actions, tt.want) {
				t.Errorf("Git actions = %v, want %v", r.git.actions, tt.want)
			}
		})
	}
}

func TestEditApplyFailureStopsBeforeGit(t *testing.T) {
	t.Parallel()
	r := newRig(t, registry.Entry{Name: "a", Desc: "d", Source: "a.md"}, st("1"), st("2"))
	r.cz.config.Edit.Apply = true
	r.cz.config.Git.AutoPush = true
	r.cz.applyErr = errors.New("apply refused")
	if err := r.svc.Run(t.Context(), "a"); !errors.Is(err, r.cz.applyErr) {
		t.Fatalf("Run() = %v, want %v", err, r.cz.applyErr)
	}
	if len(r.git.actions) != 0 {
		t.Errorf("Git actions = %v, want none after an apply failure", r.git.actions)
	}
}

func TestEditReportsRejectedPush(t *testing.T) {
	t.Parallel()
	r := newRig(t, registry.Entry{Name: "a", Desc: "d", Source: "a.md"}, st("1"), st("2"))
	r.cz.config.Git.AutoPush = true
	r.git.pushErr = errors.New("protected branch")
	err := r.svc.Run(t.Context(), "a")
	if !errors.Is(err, r.git.pushErr) || !strings.Contains(err.Error(), "commit kept locally") {
		t.Errorf("Run() = %v, want Git rejection and local commit notice", err)
	}
}

func TestEditCheckFailureStopsBeforeRender(t *testing.T) {
	t.Parallel()
	r := newRig(t, registry.Entry{Name: "a", Desc: "d", Source: "a.md", Check: []string{"lint"}}, st("1"))
	r.chk.err = errors.New("bad")
	r.cz.config.Git.AutoPush = true
	err := r.svc.Run(t.Context(), "a")
	if !errors.Is(err, ErrReported) {
		t.Fatalf("Run() = %v, want ErrReported", err)
	}
	if r.cz.applied != nil || r.pr.ask != 0 {
		t.Error("a failed check must not reach apply or confirm")
	}
	if len(r.git.actions) != 0 {
		t.Errorf("Git actions = %v, want none after a failed check", r.git.actions)
	}
	if b, _ := os.ReadFile(filepath.Join(r.src, "a.md")); string(b) != "new" {
		t.Errorf("source = %q, want the edit kept", b)
	}
}

func TestEditEditorFailureLeavesSource(t *testing.T) {
	t.Parallel()
	r := newRig(t, registry.Entry{Name: "a", Desc: "d", Source: "a.md"}, st("1"))
	r.ed.err = errors.New("crash")
	if err := r.svc.Run(t.Context(), "a"); !errors.Is(err, ErrReported) {
		t.Fatalf("Run() = %v, want ErrReported", err)
	}
	if b, _ := os.ReadFile(filepath.Join(r.src, "a.md")); string(b) != "old" {
		t.Errorf("source = %q, want it unchanged", b)
	}
}

func TestEditBrokenRenderBlocksApply(t *testing.T) {
	t.Parallel()
	r := newRig(t, registry.Entry{Name: "a", Desc: "d", Source: "a.md"}, st("1"))
	r.cz.dumpErr = map[string]map[int]error{"conf": {1: errors.New("template: boom")}}
	err := r.svc.Run(t.Context(), "a")
	if !errors.Is(err, ErrReported) {
		t.Fatalf("Run() = %v, want ErrReported", err)
	}
	if r.cz.applied != nil {
		t.Error("applied despite a configuration that no longer renders")
	}
	if !strings.Contains(r.errw.String(), "render FAILED") {
		t.Errorf("stderr = %q, want the failing configuration named", r.errw.String())
	}
}

func TestEditEncryptedReencryptsOnChange(t *testing.T) {
	t.Parallel()
	r := newRig(t, registry.Entry{Name: "s", Desc: "d", Source: "s.age"}, st("1"))
	r.cz.plain = []byte("secret")
	if err := r.svc.Run(t.Context(), "s"); err != nil {
		t.Fatalf("Run() = %v", err)
	}
	if string(r.cz.encrypted) != "new" {
		t.Errorf("encrypted %q, want the edited plaintext", r.cz.encrypted)
	}
	if b, _ := os.ReadFile(filepath.Join(r.src, "s.age")); string(b) != "ENC:new" {
		t.Errorf("source = %q, want the re-encrypted file", b)
	}
	if strings.HasPrefix(r.ed.got, r.src) {
		t.Errorf("editor opened %q, want a private copy outside the source", r.ed.got)
	}
}

func TestEditEncryptedUnchangedKeepsFile(t *testing.T) {
	t.Parallel()
	r := newRig(t, registry.Entry{Name: "s", Desc: "d", Source: "s.age"}, st("1"))
	r.cz.plain = []byte("secret")
	r.ed.write = ""
	if err := r.svc.Run(t.Context(), "s"); err != nil {
		t.Fatalf("Run() = %v", err)
	}
	if r.cz.encrypted != nil {
		t.Error("re-encrypted a file nobody changed")
	}
}

func TestEditUnknownThingIsUsageError(t *testing.T) {
	t.Parallel()
	r := newRig(t, registry.Entry{Name: "a", Desc: "d", Source: "a.md"}, st("1"))
	if err := r.svc.Run(t.Context(), "nope"); !errors.Is(err, ErrUsage) {
		t.Errorf("Run(nope) = %v, want ErrUsage", err)
	}
}

func TestEditLocalReplacesLiveFileAfterCheck(t *testing.T) {
	t.Parallel()
	r := newRig(t, registry.Entry{Name: "ssh", Desc: "d", Local: "cfg.d", Check: []string{"chk", "{file}"}}, st("1"))
	dir := filepath.Join(r.cz.home, "cfg.d")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	live := filepath.Join(dir, "work")
	if err := os.WriteFile(live, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := r.svc.Run(t.Context(), live); err != nil {
		t.Fatalf("Run() = %v", err)
	}
	if b, _ := os.ReadFile(live); string(b) != "new" {
		t.Errorf("live file = %q, want the edit", b)
	}
	if r.cz.applied != nil {
		t.Error("a local edit must not apply anything")
	}
}

func TestEditLocalRejectsBadName(t *testing.T) {
	t.Parallel()
	r := newRig(t, registry.Entry{Name: "ssh", Desc: "d", Local: "cfg.d"}, st("1"))
	err := r.svc.Run(t.Context(), filepath.Join(r.cz.home, "cfg.d", ".hidden"))
	if !errors.Is(err, ErrUsage) {
		t.Errorf("Run(.hidden) = %v, want ErrUsage", err)
	}
}

func TestWithFile(t *testing.T) {
	t.Parallel()
	in := []string{"a", "{file}", "x={file}"}
	got := withFile(in, "/p")
	if got[1] != "/p" || got[2] != "x=/p" || in[1] != "{file}" {
		t.Errorf("withFile() = %v (input %v), want substitution without mutating the input", got, in)
	}
}

func anys(in []string) []any {
	out := make([]any, len(in))
	for i, v := range in {
		out[i] = v
	}
	return out
}
