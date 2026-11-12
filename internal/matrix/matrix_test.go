package matrix

import (
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestParse(t *testing.T) {
	t.Parallel()
	tests := []struct {
		in      string
		want    Config
		wantErr bool
	}{
		{"darwin", Config{"darwin", "personal"}, false},
		{"linux,work", Config{"linux", "work"}, false},
		{"windows/light", Config{"windows", "light"}, false},
		{"plan9", Config{}, true},
		{"linux,boss", Config{}, true},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			t.Parallel()
			got, err := Parse(tt.in)
			if (err != nil) != tt.wantErr {
				t.Fatalf("Parse(%q) error = %v, wantErr %v", tt.in, err, tt.wantErr)
			}
			if got != tt.want {
				t.Errorf("Parse(%q) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

func TestAll(t *testing.T) {
	t.Parallel()
	all := All()
	if len(all) != 9 || all[0].Label() != "darwin/personal" || all[8].Label() != "windows/light" {
		t.Errorf("All() = %v, want 9 configurations from darwin/personal to windows/light", all)
	}
}

func TestHarnesses(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		tmpl string
		want []string
	}{
		{name: "list", tmpl: `{{- $all := list "claude" "copilot" "vscode" -}}`, want: []string{"claude", "copilot", "vscode"}},
		{name: "no list", tmpl: "nothing", want: nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if diff := cmp.Diff(tt.want, Harnesses([]byte(tt.tmpl))); diff != "" {
				t.Errorf("Harnesses() (-want +got):\n%s", diff)
			}
		})
	}
}

func TestDataKeys(t *testing.T) {
	t.Parallel()
	tmpl := "x = 1\n[data]\n    gitUser = {{ $u }}\n    light = {{ $l }}\n{{- if x }}\n    harnesses = []\n{{- end }}\n\n[git]\n    autoAdd = true\n"
	if diff := cmp.Diff([]string{"gitUser", "harnesses", "light"}, DataKeys([]byte(tmpl))); diff != "" {
		t.Errorf("DataKeys() (-want +got):\n%s", diff)
	}
}

func TestSeed(t *testing.T) {
	t.Parallel()
	local := map[string]any{"gitUser": "me", "gitEmail": "", "githubUser": "gh"}
	got := Seed("work", local, []string{"claude"})
	want := map[string]any{
		"light": false, "work": true, "terraform": true, "harnesses": []string{"claude"}, "bwsProjectId": "",
		"gitUser": "me", "gitEmail": "<gitEmail>", "workEmail": "<workEmail>",
		"githubUser": "gh", "personalGitRemote": "", "otelEndpoint": "",
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("Seed() (-want +got):\n%s", diff)
	}
	if Seed("light", local, nil)["terraform"] != false {
		t.Error("Seed(light) turns terraform on, want off")
	}
}

func TestTOML(t *testing.T) {
	t.Parallel()
	got := TOML(map[string]any{"b": true, "a": `x"y`, "c": []string{"p", "q"}})
	want := "[data]\na = \"x\\\"y\"\nb = true\nc = [\"p\", \"q\"]\n"
	if got != want {
		t.Errorf("TOML() = %q, want %q", got, want)
	}
}

func TestOrder(t *testing.T) {
	t.Parallel()
	order := Order()
	wantLabels := []string{
		"darwin/personal", "darwin/work", "darwin/light",
		"linux/personal", "linux/work", "linux/light",
		"windows/personal", "windows/work", "windows/light",
	}
	gotLabels := make([]string, 0, len(order))
	for _, c := range order {
		gotLabels = append(gotLabels, c.Label())
	}
	if diff := cmp.Diff(wantLabels, gotLabels); diff != "" {
		t.Errorf("Order() (-want +got):\n%s", diff)
	}
}

func TestRoleAbbr(t *testing.T) {
	t.Parallel()
	tests := []struct {
		config Config
		want   string
	}{
		{Config{OS: "darwin", Role: "personal"}, "P"},
		{Config{OS: "linux", Role: "work"}, "W"},
		{Config{OS: "windows", Role: "light"}, "L"},
		{Config{OS: "darwin", Role: "unknown"}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.config.Label(), func(t *testing.T) {
			t.Parallel()
			if got := tt.config.RoleAbbr(); got != tt.want {
				t.Errorf("%v.RoleAbbr() = %q, want %q", tt.config, got, tt.want)
			}
		})
	}
}
