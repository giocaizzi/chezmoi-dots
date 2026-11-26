package lint

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestTemplateFiles(t *testing.T) {
	t.Parallel()
	files := map[string]string{
		// included
		"dot_config/shell/aliases.sh.tmpl":         "alias ll='ls -l'\n",
		"dot_config/zsh/extra.zsh.tmpl":            "{{ if eq .chezmoi.os \"darwin\" }}x=1{{ end }}\n",
		"dot_config/powershell/profile.ps1.tmpl":   "Set-Alias ll Get-ChildItem\n",
		"dot_config/app/settings.json.tmpl":        "{}\n",
		"dot_local/bin/executable_tool.bash.tmpl":  "#!/bin/bash\n",
		".chezmoiscripts/run_once_install.sh.tmpl": "#!/bin/sh\n",
		"dot_config/notes.sh.tmpl":                 "# remember to pass the salt\n",
		"dot_config/data.sh.tmpl":                  "{{ .passphrase }} {{ $pass }} {{ .data.secret }}\n",
		"private_dot_config/private_exec.sh.tmpl":  "echo hi\n",
		"dot_config/zsh/b.zsh.tmpl":                "echo b\n",
		// excluded
		".chezmoitemplates/partial.sh.tmpl":           "echo {{ . }}\n",
		".chezmoidata/data.json.tmpl":                 "{}\n",
		".git/hooks/pre.sh.tmpl":                      "echo\n",
		".chezmoi.toml.tmpl":                          "x = 1\n",
		".chezmoiignore.tmpl":                         "x\n",
		".chezmoiexternal.json.tmpl":                  "{}\n",
		"dot_config/encrypted_secret.sh.tmpl":         "x\n",
		"dot_config/private_encrypted_secret.sh.tmpl": "x\n",
		"dot_config/symlink_link.sh.tmpl":             "x\n",
		"dot_config/modify_edit.sh.tmpl":              "x\n",
		"dot_config/key.sh.age":                       "x\n",
		"dot_config/plain.sh":                         "echo not a template\n",
		"dot_config/readme.md.tmpl":                   "# doc\n",
		"dot_config/vault.sh.tmpl":                    "{{ bitwarden \"item\" \"id\" }}\n",
		"dot_config/pw.zsh.tmpl":                      "{{- (pass \"x\") -}}\n",
		"dot_config/dec.json.tmpl":                    "{{ include \"f.age\" | decrypt }}\n",
		"dot_config/aws.sh.tmpl":                      "{{ awsSecretsManagerRaw \"r\" \"s\" }}\n",
		"dot_config/multi.sh.tmpl":                    "echo\n{{\n  onepasswordRead \"op://v/i\"\n}}\n",
	}
	src := t.TempDir()
	for name, body := range files {
		p := filepath.Join(src, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	got, err := Linter{Src: src}.templateFiles()
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		".chezmoiscripts/run_once_install.sh.tmpl",
		"dot_config/app/settings.json.tmpl",
		"dot_config/data.sh.tmpl",
		"dot_config/notes.sh.tmpl",
		"dot_config/powershell/profile.ps1.tmpl",
		"dot_config/shell/aliases.sh.tmpl",
		"dot_config/zsh/b.zsh.tmpl",
		"dot_config/zsh/extra.zsh.tmpl",
		"dot_local/bin/executable_tool.bash.tmpl",
		"private_dot_config/private_exec.sh.tmpl",
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("templateFiles() mismatch (-want +got):\n%s", diff)
	}
}

func TestLinterArgv(t *testing.T) {
	t.Parallel()
	const p = "/s/r.x"
	tests := []struct {
		name     string
		ext      string
		rendered string
		os       string
		want     []string
		json     bool
	}{
		{name: "sh no shebang", ext: ".sh", rendered: "echo hi\n", os: "linux", want: []string{"shellcheck", "-e", "SC2034", "-s", "bash", p}},
		{name: "sh shebang", ext: ".sh", rendered: "#!/bin/sh\necho hi\n", os: "linux", want: []string{"shellcheck", "-e", "SC2034", p}},
		{name: "bash no shebang", ext: ".bash", rendered: "echo\n", os: "darwin", want: []string{"shellcheck", "-e", "SC2034", "-s", "bash", p}},
		{name: "bash shebang", ext: ".bash", rendered: "#!/usr/bin/env bash\n", os: "darwin", want: []string{"shellcheck", "-e", "SC2034", p}},
		{name: "zsh", ext: ".zsh", rendered: "echo\n", os: "linux", want: []string{"zsh", "-n", p}},
		{name: "json", ext: ".json", rendered: "{}", os: "linux", json: true},
		{name: "ps1 on windows", ext: ".ps1", rendered: "ls\n", os: "windows", want: []string{"pwsh", "-NoProfile", "-Command", "$e = $null; [void][System.Management.Automation.Language.Parser]::ParseFile('" + p + "', [ref]$null, [ref]$e); if ($e) { $e; exit 1 }"}},
		{name: "ps1 on linux", ext: ".ps1", rendered: "ls\n", os: "linux"},
		{name: "ps1 on darwin", ext: ".ps1", rendered: "ls\n", os: "darwin"},
		{name: "unknown", ext: ".txt", rendered: "x", os: "linux"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, jsonCheck := linterArgv(tt.ext, []byte(tt.rendered), p, tt.os)
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("argv mismatch (-want +got):\n%s", diff)
			}
			if jsonCheck != tt.json {
				t.Errorf("jsonCheck = %v, want %v", jsonCheck, tt.json)
			}
		})
	}
}
