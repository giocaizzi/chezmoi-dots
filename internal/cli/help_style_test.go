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

			if got, want := stripHelpANSI(colored.String()), plain.String(); got != want {
				t.Errorf("colored help without ANSI = %q, want %q", got, want)
			}
			title := strings.TrimSpace("chezmoi dots " + tt.path)
			if !strings.Contains(colored.String(), style.B+title+style.O+" — ") {
				t.Errorf("help title is not blue: %q", colored.String())
			}
			for _, heading := range []string{"USAGE", "ARGUMENTS", "OPTIONS", "ENVIRONMENT", "EXAMPLES"} {
				if !strings.Contains(colored.String(), style.B+heading+style.O) {
					t.Errorf("%s heading is not blue: %q", heading, colored.String())
				}
			}
			if len(node.children) > 0 && !strings.Contains(colored.String(), style.B+"COMMANDS"+style.O) {
				t.Errorf("COMMANDS heading is not blue: %q", colored.String())
			}
			if tt.commandName != "" && !strings.Contains(colored.String(), style.M+tt.commandName) {
				t.Errorf("command %q is not mauve: %q", tt.commandName, colored.String())
			}
		})
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
