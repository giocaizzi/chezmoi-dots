package chez

import (
	"context"
	"strings"
)

// GitNoPromptEnv returns the environment that keeps a git command from asking
// anything: no credential prompt and, unless the user chose their own ssh command
// (GIT_SSH_COMMAND or GIT_SSH in getenv, or core.sshCommand in the git config of
// dir, or of the current directory when dir is empty), no ssh passphrase or host
// key prompt. A nil getenv reads nothing. The config probe runs under ctx, so the
// caller sets its timeout.
func GitNoPromptEnv(ctx context.Context, r Runner, getenv func(string) string, dir string) []string {
	if userChoseSSH(ctx, r, getenv, dir) {
		return []string{"GIT_TERMINAL_PROMPT=0"}
	}
	return []string{"GIT_TERMINAL_PROMPT=0", "GIT_SSH_COMMAND=ssh -o BatchMode=yes"}
}

func userChoseSSH(ctx context.Context, r Runner, getenv func(string) string, dir string) bool {
	if getenv != nil && (getenv("GIT_SSH_COMMAND") != "" || getenv("GIT_SSH") != "") {
		return true
	}
	args := []string{"git"}
	if dir != "" {
		args = append(args, "-C", dir)
	}
	out, err := Output(ctx, r, Cmd{Args: append(args, "config", "--get", "core.sshCommand")})
	return err == nil && strings.TrimSpace(string(out)) != ""
}
