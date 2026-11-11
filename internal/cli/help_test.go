package cli_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/giocaizzi/chezmoi-dots/internal/chez"
	"github.com/giocaizzi/chezmoi-dots/internal/cli"
)

// helpTrap records every external boundary; help must not cross any of them.
type helpTrap struct{ calls []string }

func (s *helpTrap) Run(_ context.Context, c chez.Cmd) error {
	s.calls = append(s.calls, "process: "+strings.Join(c.Args, " "))
	return errors.New("unexpected process")
}

func (s *helpTrap) Read(_ []byte) (int, error) {
	s.calls = append(s.calls, "input")
	return 0, errors.New("unexpected input")
}

func (s *helpTrap) RoundTrip(_ *http.Request) (*http.Response, error) {
	s.calls = append(s.calls, "http")
	return nil, errors.New("unexpected network")
}

func (s *helpTrap) deps(out, errw *bytes.Buffer) cli.Deps {
	return cli.Deps{
		Stdout: out, Stderr: errw, Stdin: s, Runner: s,
		LookPath: func(name string) (string, error) {
			s.calls = append(s.calls, "lookpath: "+name)
			return "", errors.New("unexpected lookup")
		},
		HTTP: &http.Client{Transport: s}, Home: filepath.Join("testdata", "missing-home"), GOOS: "linux", Version: "test",
		Getenv: func(key string) string {
			switch key {
			case "CHEZMOI_SOURCE_DIR", "XDG_CONFIG_HOME", "TMPDIR", "TMP", "TEMP":
				return filepath.Join("testdata", "missing-help-source")
			case "NO_COLOR":
				return "1"
			}
			return ""
		},
	}
}

func TestLocalHelp(t *testing.T) {
	t.Parallel()
	pages := [][]string{nil, {"edit"}, {"view"}, {"list"}, {"lint"}, {"audit"}, {"audit", "aliases"}, {"outdated"}, {"bump"}, {"upgrade"}, {"version"}, {"help"}, {"live"}}
	for _, page := range pages {
		t.Run(strings.Join(page, "/"), func(t *testing.T) {
			t.Parallel()
			var canonical string
			for _, variant := range [][]string{append(append([]string{}, page...), "-h"), append(append([]string{}, page...), "--help"), append([]string{"help"}, page...)} {
				text := localHelp(t, variant)
				if canonical == "" {
					canonical = text
				} else if canonical != text {
					t.Errorf("Run(%q) differs from local -h", variant)
				}
			}
		})
	}
}

func localHelp(t *testing.T, args []string) string {
	t.Helper()
	var out, errw bytes.Buffer
	trap := &helpTrap{}
	err := cli.Run(t.Context(), trap.deps(&out, &errw), args)
	if err != nil || errw.Len() != 0 {
		t.Errorf("Run(%q) = %v, stderr %q; want success on stdout", args, err, errw.String())
	}
	if len(trap.calls) != 0 {
		t.Errorf("Run(%q) touched external boundaries: %v", args, trap.calls)
	}
	text := out.String()
	for _, heading := range []string{"USAGE", "OPTIONS", "EXAMPLES"} {
		if !strings.Contains(text, heading) {
			t.Errorf("Run(%q) lacks %s", args, heading)
		}
	}
	return text
}

func TestVersionIgnoredArguments(t *testing.T) {
	t.Parallel()
	for _, args := range [][]string{{"version", "--unknown"}, {"version", "--as=linux"}, {"version", "extra"}, {"version", "--unknown", "--help"}} {
		t.Run(strings.Join(args, "/"), func(t *testing.T) {
			t.Parallel()
			var out, errw bytes.Buffer
			trap := &helpTrap{}
			if err := cli.Run(t.Context(), trap.deps(&out, &errw), args); err != nil {
				t.Errorf("Run(%q) = %v, want success", args, err)
			}
			if out.String() != "chezmoi-dots test\n" || errw.Len() != 0 || len(trap.calls) != 0 {
				t.Errorf("stdout %q, stderr %q, calls %v; want version only", out.String(), errw.String(), trap.calls)
			}
		})
	}
}

func TestHelpContent(t *testing.T) {
	t.Parallel()
	tests := []struct {
		page         []string
		want, absent []string
	}{
		{nil, []string{"edit", "view", "list", "lint", "audit", "outdated", "bump", "upgrade", "version", "help", "COMMANDS", "--help", "-h", "default"}, []string{"--as", "--matrix", "--all", "--dry-run", "--offline", "--fetch", "--check", "\n  live "}},
		{[]string{"edit"}, []string{"ARGUMENTS", "ENVIRONMENT", "CHEZMOI_SOURCE_DIR", "EDITOR"}, nil},
		{[]string{"view"}, []string{"ARGUMENTS", "ENVIRONMENT", "--as", "--matrix", "--all", "CHEZMOI_SOURCE_DIR"}, nil},
		{[]string{"lint"}, []string{"ARGUMENTS", "registry", "aliases", "packages", "matrix", "templates"}, []string{"COMMANDS"}},
		{[]string{"audit"}, []string{"COMMANDS", "aliases"}, nil},
		{[]string{"audit", "aliases"}, []string{"ENVIRONMENT", "XDG_CONFIG_HOME"}, []string{"COMMANDS"}},
		{[]string{"outdated"}, []string{"--offline", "--fetch", "--check", "ENVIRONMENT", "CHEZMOI_WORKING_TREE"}, nil},
		{[]string{"bump"}, []string{"ARGUMENTS", "--dry-run", "ENVIRONMENT", "CHEZMOI_SOURCE_DIR"}, []string{"COMMANDS"}},
		{[]string{"upgrade"}, []string{"ARGUMENTS", "--dry-run", "ENVIRONMENT", "CHEZMOI_EXECUTABLE", "show upgrade commands without executing upgrade steps", "Default dry run still checks pinned releases and can use the network."}, []string{"COMMANDS", "run nothing"}},
		{[]string{"live"}, []string{"--global", "--bin", "--src", "Internal", "chezmoi"}, nil},
	}
	for _, tt := range tests {
		t.Run(strings.Join(tt.page, "/"), func(t *testing.T) {
			t.Parallel()
			var out, errw bytes.Buffer
			trap := &helpTrap{}
			if err := cli.Run(t.Context(), trap.deps(&out, &errw), append([]string{"help"}, tt.page...)); err != nil {
				t.Fatal(err)
			}
			for _, want := range tt.want {
				if !strings.Contains(out.String(), want) {
					t.Errorf("help %q lacks %q", tt.page, want)
				}
			}
			for _, absent := range tt.absent {
				if strings.Contains(out.String(), absent) {
					t.Errorf("help %q contains %q", tt.page, absent)
				}
			}
		})
	}
}

func TestHelpAuditPageContext(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		args, page []string
	}{
		{[]string{"audit", "--help", "aliases"}, []string{"audit"}},
		{[]string{"audit", "aliases", "--help"}, []string{"audit", "aliases"}},
	} {
		t.Run(strings.Join(tt.args, "/"), func(t *testing.T) {
			t.Parallel()
			var out, errw, want, unused bytes.Buffer
			trap := &helpTrap{}
			if err := cli.Run(t.Context(), trap.deps(&out, &errw), tt.args); err != nil {
				t.Fatal(err)
			}
			if err := cli.Run(t.Context(), trap.deps(&want, &unused), append([]string{"help"}, tt.page...)); err != nil {
				t.Fatal(err)
			}
			if out.String() != want.String() || errw.Len() != 0 || len(trap.calls) != 0 {
				t.Errorf("stdout %q, stderr %q, calls %v; want page %q", out.String(), errw.String(), trap.calls, tt.page)
			}
		})
	}
}

func TestHelpOptionDefaults(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		page     string
		options  []string
		defValue string
	}{
		{"view", []string{"--as <os[,role]>"}, ""},
		{"view", []string{"--matrix", "--all"}, "false"},
		{"outdated", []string{"--offline", "--fetch", "--check"}, "false"},
		{"bump", []string{"--dry-run"}, "false"},
		{"upgrade", []string{"--dry-run"}, "false"},
		{"live", []string{"--bin <executable>"}, "chezmoi"},
		{"live", []string{"--src <directory>", "--real-src <directory>", "--file <file>", "--edited <file>", "--dir <directory>", "--global <argument>"}, ""},
	} {
		t.Run(tt.page+"/"+tt.options[0], func(t *testing.T) {
			t.Parallel()
			var out, errw bytes.Buffer
			trap := &helpTrap{}
			if err := cli.Run(t.Context(), trap.deps(&out, &errw), []string{"help", tt.page}); err != nil {
				t.Fatal(err)
			}
			for _, option := range tt.options {
				found := slices.ContainsFunc(strings.Split(out.String(), "\n"), func(line string) bool {
					return strings.HasPrefix(line, "  "+option+"  ") && strings.HasSuffix(line, fmt.Sprintf("(default: %q)", tt.defValue))
				})
				if !found {
					t.Errorf("help %s lacks option %q with default %q", tt.page, option, tt.defValue)
				}
			}
		})
	}
}

func TestHelpInvalidPath(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct{ args, nearest []string }{
		{[]string{"help", "no-such-command"}, nil},
		{[]string{"help", "audit", "no-such-child"}, []string{"audit"}},
		{[]string{"help", "audit", "aliases", "extra"}, []string{"audit", "aliases"}},
		{[]string{"help", "lint", "registry"}, []string{"lint"}},
		{[]string{"help", "bump", "release"}, []string{"bump"}},
		{[]string{"help", "upgrade", "step"}, []string{"upgrade"}},
	} {
		t.Run(strings.Join(tt.args, "/"), func(t *testing.T) {
			t.Parallel()
			var out, errw, want, unused bytes.Buffer
			trap := &helpTrap{}
			err := cli.Run(t.Context(), trap.deps(&out, &errw), tt.args)
			if !errors.Is(err, cli.ErrUsage) {
				t.Errorf("error = %v, want ErrUsage", err)
			}
			if out.Len() != 0 {
				t.Errorf("stdout = %q, want empty", out.String())
			}
			if err := cli.Run(t.Context(), trap.deps(&want, &unused), append([]string{"help"}, tt.nearest...)); err != nil {
				t.Fatal(err)
			}
			if want.Len() == 0 || !strings.Contains(errw.String(), want.String()) {
				t.Errorf("stderr lacks nearest page %q: %q", tt.nearest, errw.String())
			}
			if strings.Count(errw.String(), "USAGE") != 1 {
				t.Errorf("stderr must have one local help: %q", errw.String())
			}
			if len(trap.calls) != 0 {
				t.Errorf("external calls: %v", trap.calls)
			}
		})
	}
}

func TestHelpInterspersed(t *testing.T) {
	t.Parallel()
	for _, args := range [][]string{{"edit", "note", "--help"}, {"view", "note", "--matrix", "-h"}, {"lint", "registry", "--help"}, {"bump", "release", "--dry-run", "-h"}, {"upgrade", "step", "--help"}, {"version", "extra", "--help"}, {"version", "--help", "--unknown"}, {"edit", "--", "note", "--help"}} {
		t.Run(strings.Join(args, "/"), func(t *testing.T) {
			t.Parallel()
			var out, errw bytes.Buffer
			trap := &helpTrap{}
			if err := cli.Run(t.Context(), trap.deps(&out, &errw), args); err != nil {
				t.Errorf("error = %v", err)
			}
			if !strings.Contains(out.String(), "USAGE") || errw.Len() != 0 || len(trap.calls) != 0 {
				t.Errorf("stdout %q, stderr %q, calls %v", out.String(), errw.String(), trap.calls)
			}
		})
	}
}

func TestHelpTokensRetainParserMeaning(t *testing.T) {
	t.Parallel()
	for _, args := range [][]string{{"view", "--as", "-h", "note"}, {"view", "--as=--help", "note"}, {"view", "--", "--help-looking-selector"}, {"edit", "--", "-h"}, {"edit", "--", "--help"}, {"--help-looking-selector"}, {"note"}, nil} {
		t.Run(strings.Join(args, "/"), func(t *testing.T) {
			t.Parallel()
			var out, errw bytes.Buffer
			trap := &helpTrap{}
			err := cli.Run(t.Context(), trap.deps(&out, &errw), args)
			if err == nil {
				t.Errorf("Run(%q) unexpectedly treated selector/value as help", args)
			}
			if strings.Contains(out.String(), "USAGE") {
				t.Errorf("help on stdout for selector/value %q", args)
			}
			// A leading dash is still parsed as a flag; a plain unknown token and
			// the missing command must reach the default edit service/picker.
			if (len(args) == 0 || slices.Equal(args, []string{"note"})) && len(trap.calls) == 0 {
				t.Errorf("default edit did not reach service")
			}
		})
	}
}

func TestUsageReportsLocalHelpOnce(t *testing.T) {
	t.Parallel()
	for _, args := range [][]string{{"edit", "a", "b"}, {"list", "pattern"}, {"view", "--unknown"}, {"view", "--unknown", "--help"}, {"lint", "--unknown"}, {"audit"}, {"audit", "other"}, {"outdated", "extra"}, {"bump", "--unknown"}, {"upgrade", "--unknown"}, {"live", "--unknown"}} {
		t.Run(strings.Join(args, "/"), func(t *testing.T) {
			t.Parallel()
			var out, errw bytes.Buffer
			trap := &helpTrap{}
			err := cli.Run(t.Context(), trap.deps(&out, &errw), args)
			if !errors.Is(err, cli.ErrUsage) {
				t.Errorf("error = %v, want ErrUsage", err)
			}
			if out.Len() != 0 || strings.Count(errw.String(), "USAGE") != 1 {
				t.Errorf("stdout %q, stderr %q; want one stderr help", out.String(), errw.String())
			}
			if i := strings.Index(errw.String(), "USAGE"); i < 0 || strings.TrimSpace(errw.String()[:i]) == "" {
				t.Error("missing diagnostic before local help")
			}
		})
	}
}

func TestHelpGoldens(t *testing.T) {
	t.Parallel()
	for _, page := range []string{"root", "audit", "view", "bump", "live"} {
		t.Run(page, func(t *testing.T) {
			t.Parallel()
			args := []string{"help"}
			if page != "root" {
				args = append(args, page)
			}
			var out, errw bytes.Buffer
			trap := &helpTrap{}
			if err := cli.Run(t.Context(), trap.deps(&out, &errw), args); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join("testdata", "help_"+page+".golden")
			want := helpGolden(t, path, out.Bytes())
			if !bytes.Equal(out.Bytes(), want) {
				t.Errorf("help differs from %s; review changes before UPDATE_GOLDEN=1", path)
			}
		})
	}
}

func helpGolden(t *testing.T, path string, got []byte) []byte {
	t.Helper()
	if os.Getenv("UPDATE_GOLDEN") == "1" {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden (generate after implementation with UPDATE_GOLDEN=1): %v", err)
	}
	return want
}

type liveCapture struct{ calls [][]string }

func (r *liveCapture) Run(_ context.Context, c chez.Cmd) error {
	r.calls = append(r.calls, append([]string{}, c.Args...))
	_, err := fmt.Fprint(c.Stdout, "{}")
	return err
}

func TestLiveRepeatableGlobals(t *testing.T) {
	t.Parallel()
	for _, value := range []string{"-h", "--help"} {
		t.Run(value, func(t *testing.T) {
			t.Parallel()
			ctx, cancel := context.WithCancel(t.Context())
			cancel()
			var out, errw bytes.Buffer
			trap := &helpTrap{}
			d := trap.deps(&out, &errw)
			runner := &liveCapture{}
			d.Runner = runner
			args := []string{"live", "--bin", "custom-chezmoi", "--global", "--config", "--global", value, "--global=--verbose", "--src", "shadow"}
			if err := cli.Run(ctx, d, args); err != nil {
				t.Fatal(err)
			}
			want := []string{"custom-chezmoi", "--config", value, "--verbose", "--source", "shadow"}
			if len(runner.calls) != 1 {
				t.Fatalf("calls = %q, want one initial render", runner.calls)
			}
			got := runner.calls[0]
			if len(got) < len(want) || !slices.Equal(got[:len(want)], want) || !slices.Contains(got, "dump") {
				t.Errorf("call = %q, want globals prefix %q followed by dump", got, want)
			}
			if strings.Contains(out.String(), "USAGE") {
				t.Error("global value incorrectly triggered help")
			}
		})
	}
}
