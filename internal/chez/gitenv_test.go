package chez

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/google/go-cmp/cmp"
)

// configRunner records the commands it runs and answers the config probe.
type configRunner struct {
	config string
	err    error
	calls  [][]string
}

func (r *configRunner) Run(_ context.Context, c Cmd) error {
	r.calls = append(r.calls, c.Args)
	if r.err != nil {
		return r.err
	}
	_, err := io.WriteString(c.Stdout, r.config)
	return err
}

func TestGitNoPromptEnv(t *testing.T) {
	t.Parallel()
	prompt := "GIT_TERMINAL_PROMPT=0"
	batch := "GIT_SSH_COMMAND=ssh -o BatchMode=yes"
	probe := []string{"git", "config", "--get", "core.sshCommand"}
	tests := []struct {
		name      string
		getenv    map[string]string
		nilGetenv bool
		config    string
		err       error
		dir       string
		want      []string
		wantCalls [][]string
	}{
		{name: "default", want: []string{prompt, batch}, wantCalls: [][]string{probe}},
		{name: "nil getenv", nilGetenv: true, want: []string{prompt, batch}, wantCalls: [][]string{probe}},
		{
			name: "dir", dir: "/wt", want: []string{prompt, batch},
			wantCalls: [][]string{{"git", "-C", "/wt", "config", "--get", "core.sshCommand"}},
		},
		{name: "GIT_SSH_COMMAND", getenv: map[string]string{"GIT_SSH_COMMAND": "ssh -i k"}, want: []string{prompt}},
		{name: "GIT_SSH", getenv: map[string]string{"GIT_SSH": "/bin/myssh"}, want: []string{prompt}},
		{name: "core.sshCommand", config: "ssh -F cfg\n", want: []string{prompt}, wantCalls: [][]string{probe}},
		{name: "blank config", config: " \n", want: []string{prompt, batch}, wantCalls: [][]string{probe}},
		{name: "probe fails", err: errors.New("exit 1"), want: []string{prompt, batch}, wantCalls: [][]string{probe}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r := &configRunner{config: tt.config, err: tt.err}
			getenv := func(k string) string { return tt.getenv[k] }
			if tt.nilGetenv {
				getenv = nil
			}
			got := GitNoPromptEnv(t.Context(), r, getenv, tt.dir)
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("env (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(tt.wantCalls, r.calls); diff != "" {
				t.Errorf("calls (-want +got):\n%s", diff)
			}
		})
	}
}
