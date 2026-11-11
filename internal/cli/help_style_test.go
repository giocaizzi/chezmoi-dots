package cli

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/giocaizzi/chezmoi-dots/internal/term"
)

func TestRenderHelpStyles(t *testing.T) {
	t.Parallel()
	style := term.Style{B: "\x1b[1;34m", M: "\x1b[35m", O: "\x1b[0m"}
	tests := []struct {
		name        string
		path        string
		commandName string
	}{
		{name: "root", commandName: "edit"},
		{name: "group", path: "audit", commandName: "aliases"},
		{name: "leaf", path: "audit aliases"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			node := helpPage(tt.path)
			var plain, colored bytes.Buffer
			renderHelp(&plain, node, term.Style{})
			renderHelp(&colored, node, style)
			assertHelpStylePage(t, node, tt.path, tt.commandName, plain.String(), colored.String(), style)
		})
	}
}

func assertHelpStylePage(t *testing.T, node helpNode, path, commandName, plain, colored string, style term.Style) {
	t.Helper()
	if got, want := stripHelpANSI(colored), plain; got != want {
		t.Errorf("colored help without ANSI = %q, want %q", got, want)
	}
	title := strings.TrimSpace("chezmoi dots " + path)
	assertHelpStyle(t, colored, style.B, title, style.O+" — ")
	assertBlueHeadings(t, colored, style, node)
	if commandName != "" {
		assertHelpStyle(t, colored, style.M, commandName, "")
	}
}

func assertBlueHeadings(t *testing.T, output string, style term.Style, node helpNode) {
	t.Helper()
	for _, heading := range []string{"USAGE", "ARGUMENTS", "OPTIONS", "ENVIRONMENT", "EXAMPLES"} {
		assertHelpStyle(t, output, style.B, heading, style.O)
	}
	if len(node.children) > 0 {
		assertHelpStyle(t, output, style.B, "COMMANDS", style.O)
	}
}

func assertHelpStyle(t *testing.T, output, color, text, suffix string) {
	t.Helper()
	if !strings.Contains(output, color+text+suffix) {
		t.Errorf("output lacks styled text %q: %q", text, output)
	}
}

func TestHelpStreamsRespectColorPolicy(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		env  map[string]string
	}{
		{name: "redirected"},
		{name: "no color", env: map[string]string{"NO_COLOR": "1"}},
		{name: "dumb terminal", env: map[string]string{"TERM": "dumb"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var stdout, stderr bytes.Buffer
			deps := Deps{
				Stdout: &stdout,
				Stderr: &stderr,
				Getenv: func(key string) string { return tt.env[key] },
			}
			if err := Run(t.Context(), deps, []string{"help"}); err != nil {
				t.Fatal(err)
			}
			if err := Run(t.Context(), deps, []string{"help", "missing"}); !errors.Is(err, ErrUsage) {
				t.Fatalf("Run(help missing) = %v, want usage error", err)
			}
			if strings.Contains(stdout.String(), "\x1b[") || strings.Contains(stderr.String(), "\x1b[") {
				t.Errorf("redirected help contains ANSI escapes: stdout=%q stderr=%q", stdout.String(), stderr.String())
			}
		})
	}
}

func stripHelpANSI(text string) string {
	return strings.NewReplacer(
		"\x1b[1;34m", "",
		"\x1b[35m", "",
		"\x1b[0m", "",
	).Replace(text)
}
