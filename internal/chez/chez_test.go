package chez

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

// fakeRunner answers each call with the next canned reply and records the argv.
type fakeRunner struct {
	stdout, stderr string
	err            error
	calls          [][]string
}

func (f *fakeRunner) Run(_ context.Context, c Cmd) error {
	f.calls = append(f.calls, c.Args)
	if c.Stdout != nil {
		_, _ = c.Stdout.Write([]byte(f.stdout))
	}
	if c.Stderr != nil {
		_, _ = c.Stderr.Write([]byte(f.stderr))
	}
	return f.err
}

func TestOutputErrorCarriesStderr(t *testing.T) {
	t.Parallel()
	r := &fakeRunner{stderr: "boom\n", err: errors.New("exit 1")}
	_, err := Output(t.Context(), r, Cmd{Args: []string{"x"}})
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Errorf("Output() error = %v, want it to carry stderr", err)
	}
	r = &fakeRunner{stdout: "lint says no\n", err: errors.New("exit 1")}
	_, err = Output(t.Context(), r, Cmd{Args: []string{"x"}})
	if err == nil || !strings.Contains(err.Error(), "lint says no") {
		t.Errorf("Output() error = %v, want it to fall back to stdout", err)
	}
}

func TestDataBuildsCommand(t *testing.T) {
	t.Parallel()
	r := &fakeRunner{stdout: `{"a":1}`}
	c := New(r, "chezmoi", "--source", "/s").With("--config", "/c")
	got, err := c.Data(t.Context(), Overrides{"work": true})
	if err != nil {
		t.Fatal(err)
	}
	if got["a"] != float64(1) {
		t.Errorf("Data() = %v", got)
	}
	a := r.calls[0]
	if diff := cmp.Diff([]string{"chezmoi", "--source", "/s", "--config", "/c", "--override-data"}, a[:6]); diff != "" {
		t.Errorf("argv prefix (-want +got):\n%s", diff)
	}
	var o map[string]any
	if err := json.Unmarshal([]byte(a[6]), &o); err != nil {
		t.Fatal(err)
	}
	if diff := cmp.Diff(map[string]any{"work": true, "dotsRender": true}, o); diff != "" {
		t.Errorf("overrides (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff([]string{"data", "--format", "json"}, a[7:]); diff != "" {
		t.Errorf("subcommand (-want +got):\n%s", diff)
	}
}

func TestWithDoesNotAliasGlobals(t *testing.T) {
	t.Parallel()
	r := &fakeRunner{stdout: `{}`}
	base := New(r, "chezmoi", "--a")
	_ = base.With("--b")
	if _, err := base.Data(t.Context(), nil); err != nil {
		t.Fatal(err)
	}
	for _, arg := range r.calls[0] {
		if arg == "--b" {
			t.Errorf("base client leaked a flag from With: %v", r.calls[0])
		}
	}
}

func TestManaged(t *testing.T) {
	t.Parallel()
	r := &fakeRunner{stdout: `{"/h/.zshrc":{"sourceRelative":"dot_zshrc"}}`}
	got, err := New(r, "chezmoi").Managed(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if diff := cmp.Diff(map[string]string{"/h/.zshrc": "dot_zshrc"}, got); diff != "" {
		t.Errorf("Managed() (-want +got):\n%s", diff)
	}
}

func TestEditConfig(t *testing.T) {
	t.Parallel()
	r := &fakeRunner{stdout: `{"workingTree":"/source","edit":{"apply":true},"git":{"autoadd":true,"autocommit":true,"autopush":true}}`}
	got, err := New(r, "chezmoi", "--config", "/config").EditConfig(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if got.WorkingTree != "/source" || !got.Edit.Apply || !got.Git.AutoAdd || !got.Git.AutoCommit || !got.Git.AutoPush {
		t.Errorf("EditConfig() = %+v, want working tree and all four settings enabled", got)
	}
	want := []string{"chezmoi", "--config", "/config", "dump-config", "--format", "json"}
	if diff := cmp.Diff(want, r.calls[0]); diff != "" {
		t.Errorf("argv (-want +got):\n%s", diff)
	}
}

func TestStateAndChanged(t *testing.T) {
	t.Parallel()
	before, err := DecodeState([]byte(`{"/a":{"type":"file","contents":"1"},"/b":{"type":"file","contents":"x"},"/c":{"type":"file","contents":"y"}}`))
	if err != nil {
		t.Fatal(err)
	}
	after, err := DecodeState([]byte(`{"/a":{"type":"file","contents":"2"},"/b":{"type":"file","contents":"x"},"/d":{"type":"file","contents":"z"}}`))
	if err != nil {
		t.Fatal(err)
	}
	if diff := cmp.Diff([]string{"/a", "/c", "/d"}, Changed(before, after)); diff != "" {
		t.Errorf("Changed() (-want +got):\n%s", diff)
	}
	if _, err := DecodeState([]byte(`nope`)); err == nil {
		t.Error("DecodeState(garbage): want an error")
	}
}
