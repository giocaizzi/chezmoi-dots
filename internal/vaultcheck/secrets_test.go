package vaultcheck_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/giocaizzi/chezmoi-dots/internal/vaultcheck"
)

type m = map[string]any

func ring(service, user any) m { return m{"service": service, "user": user} }

func TestValidate(t *testing.T) {
	t.Parallel()
	valid := func(over m) m {
		e := m{"kind": "bws", "projectFrom": "secretsProjectId", "keyring": ring("bws", "access-token")}
		for k, v := range over {
			if v == nil {
				delete(e, k)
			} else {
				e[k] = v
			}
		}
		return e
	}
	tests := []struct {
		name string
		data m
		want []string // "where: message"
	}{
		{"absent", m{}, nil},
		{"not a table is ignored", m{"secret": "x"}, nil},
		{"entry not a table is ignored", m{"secret": m{"a": "x"}}, nil},
		{"valid bws projectFrom", m{"secret": m{"a": valid(nil)}}, nil},
		{"valid bws project and command", m{"secret": m{"a": valid(m{"projectFrom": nil, "project": "p", "command": "~/bin/bws"})}}, nil},
		{"valid bw and rbw", m{"secret": m{"a": m{"kind": "bw"}, "b": m{"kind": "rbw", "command": "rbw"}}}, nil},
		{"unknown keys sorted", m{"secret": m{"a": valid(m{"zeta": 1, "alpha": 1})}}, []string{"secret a: unknown key alpha", "secret a: unknown key zeta"}},
		{"unknown keyring key", m{"secret": m{"a": valid(m{"keyring": m{"service": "s", "user": "u", "x": 1}})}}, []string{"secret a: unknown key keyring.x"}},
		{"table without kind is ignored", m{"secret": m{"a": m{"foo": "bar"}}}, nil},
		{"unknown kind", m{"secret": m{"a": m{"kind": "pass"}}}, []string{`secret a: unknown kind "pass" (one of bws, bw, rbw)`}},
		{"non-string kind", m{"secret": m{"a": m{"kind": 3}}}, []string{`secret a: unknown kind "3" (one of bws, bw, rbw)`}},
		{"empty command", m{"secret": m{"a": m{"kind": "bw", "command": ""}}}, []string{"secret a: command must be a non-empty string"}},
		{"non-string command", m{"secret": m{"a": m{"kind": "bw", "command": 1}}}, []string{"secret a: command must be a non-empty string"}},
		{"bws neither project", m{"secret": m{"a": valid(m{"projectFrom": nil})}}, []string{"secret a: kind bws needs project or projectFrom"}},
		{"bws both project", m{"secret": m{"a": valid(m{"project": "p"})}}, []string{"secret a: set project or projectFrom, not both"}},
		{"bws empty project", m{"secret": m{"a": valid(m{"projectFrom": nil, "project": ""})}}, []string{"secret a: project must be a non-empty string"}},
		{"bws empty projectFrom", m{"secret": m{"a": valid(m{"projectFrom": ""})}}, []string{"secret a: projectFrom must be a non-empty string"}},
		{"bws no keyring", m{"secret": m{"a": valid(m{"keyring": nil})}}, []string{"secret a: kind bws needs keyring = { service, user }"}},
		{"bws keyring not table", m{"secret": m{"a": valid(m{"keyring": "x"})}}, []string{"secret a: keyring must be a table"}},
		{"bws keyring empty", m{"secret": m{"a": valid(m{"keyring": ring("", 4)})}}, []string{"secret a: keyring needs service", "secret a: keyring needs user"}},
		{"bw with bws keys", m{"secret": m{"a": m{"kind": "bw", "project": "p", "projectFrom": "x", "keyring": ring("s", "u")}}}, []string{
			"secret a: project is only for kind bws", "secret a: projectFrom is only for kind bws", "secret a: keyring is only for kind bws",
		}},
		{"rbw with keyring", m{"secret": m{"a": m{"kind": "rbw", "keyring": ring("s", "u")}}}, []string{"secret a: keyring is only for kind bws"}},
		{"entries in name order", m{"secret": m{"b": m{"kind": "x"}, "a": m{"kind": "y"}}}, []string{`secret a: unknown kind "y" (one of bws, bw, rbw)`, `secret b: unknown kind "x" (one of bws, bw, rbw)`}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var got []string
			for _, f := range vaultcheck.Validate(tt.data) {
				got = append(got, f.String())
			}
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("Validate() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// A relative command with a path separator resolves against the working directory of
// whoever runs chezmoi, so a planted bin/bws there would receive the token.
func TestValidateCommand(t *testing.T) {
	t.Parallel()
	const msg = "secret a: command must be an absolute path, ~/..., or a bare name looked up on PATH"
	tests := []struct {
		command string
		bad     bool
	}{
		{"bin/bws", true},
		{"./bws", true},
		{"../tools/bws", true},
		{`bin\bws`, true},
		{`.\bws`, true},
		{"bws", false},
		{"~/bin/bws", false},
		{"/usr/bin/bws", false},
		{"C:/tools/bws.exe", false},
		{`c:\tools\bws.exe`, false},
	}
	for _, tt := range tests {
		t.Run(tt.command, func(t *testing.T) {
			t.Parallel()
			var got []string
			for _, f := range vaultcheck.Validate(m{"secret": m{"a": m{"kind": "bw", "command": tt.command}}}) {
				got = append(got, f.String())
			}
			var want []string
			if tt.bad {
				want = []string{msg}
			}
			if diff := cmp.Diff(want, got); diff != "" {
				t.Errorf("Validate() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestParse(t *testing.T) {
	t.Parallel()
	data := m{"secret": m{
		"z": m{"kind": "rbw"},
		"a": m{"kind": "bws", "projectFrom": "id", "keyring": ring("svc", "usr"), "command": "~/bws"},
		"m": m{"kind": "bws", "project": "p", "keyring": ring("s", "u")},
	}}
	got, fs := vaultcheck.Parse(data)
	if len(fs) != 0 {
		t.Fatalf("Parse() findings = %v", fs)
	}
	want := []vaultcheck.Entry{
		{Name: "a", Kind: "bws", ProjectFrom: "id", Service: "svc", User: "usr", Command: "~/bws"},
		{Name: "m", Kind: "bws", Project: "p", Service: "s", User: "u"},
		{Name: "z", Kind: "rbw"},
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("Parse() mismatch (-want +got):\n%s", diff)
	}
}

func TestIgnored(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		data m
		want []string
	}{
		{"absent", m{}, nil},
		{"secret not a table", m{"secret": "x"}, []string{"secret"}},
		{"only entries", m{"secret": m{"a": m{"kind": "bw"}}}, nil},
		{"sorted, mixed", m{"secret": m{"z": "x", "a": m{"foo": 1}, "k": m{"kind": "bw"}, "m": m{}}}, []string{"a", "m", "z"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if diff := cmp.Diff(tt.want, vaultcheck.Ignored(tt.data)); diff != "" {
				t.Errorf("Ignored() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestParse_ignores(t *testing.T) {
	t.Parallel()
	got, fs := vaultcheck.Parse(m{"secret": m{"a": m{"kind": "rbw"}, "other": m{"foo": "bar"}, "s": "x"}})
	if len(fs) != 0 || len(got) != 1 || got[0].Name != "a" {
		t.Errorf("Parse() = %v, %v; want only entry a", got, fs)
	}
}

func TestParse_findings(t *testing.T) {
	t.Parallel()
	got, fs := vaultcheck.Parse(m{"secret": m{"a": m{"kind": "pass"}}})
	if got != nil || len(fs) != 1 {
		t.Errorf("Parse() = %v, %v; want no entries and one finding", got, fs)
	}
	if got, fs := vaultcheck.Parse(m{}); len(got) != 0 || fs != nil {
		t.Errorf("Parse(empty) = %v, %v", got, fs)
	}
}
