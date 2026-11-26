package matrix

import (
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/giocaizzi/chezmoi-dots/internal/chez"
)

func TestParse(t *testing.T) {
	t.Parallel()
	tests := []struct {
		in      string
		want    Config
		wantErr bool
	}{
		{"darwin", Config{"darwin", ""}, false},
		{"linux,home", Config{"linux", "home"}, false},
		{"windows/dev-ops_2", Config{"windows", "dev-ops_2"}, false},
		{"plan9", Config{}, true},
		{"linux,Home", Config{}, true},
		{"linux,-x", Config{}, true},
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

func TestShort(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		profiles []string
		want     map[string]string
	}{
		{"distinct", []string{"light", "personal", "work"}, map[string]string{"light": "L", "personal": "P", "work": "W"}},
		{"shared first letter", []string{"web", "work"}, map[string]string{"web": "We", "work": "Wo"}},
		{"prefix of another", []string{"dev", "devops"}, map[string]string{"dev": "Dev", "devops": "Devo"}},
		{"single", []string{"default"}, map[string]string{"default": "D"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if diff := cmp.Diff(tt.want, Short(tt.profiles)); diff != "" {
				t.Errorf("Short(%v) (-want +got):\n%s", tt.profiles, diff)
			}
		})
	}
}

func TestProfilesAndConfigs(t *testing.T) {
	t.Parallel()
	if diff := cmp.Diff([]string{"default"}, Spec{}.Profiles()); diff != "" {
		t.Errorf("zero Spec Profiles() (-want +got):\n%s", diff)
	}
	s := Spec{Profile: map[string]map[string]any{"minimal": {}, "home": {}}}
	if diff := cmp.Diff([]string{"home", "minimal"}, s.Profiles()); diff != "" {
		t.Errorf("Profiles() (-want +got):\n%s", diff)
	}
	labels := make([]string, 0, len(s.Configs()))
	for _, c := range s.Configs() {
		labels = append(labels, c.Label())
	}
	want := []string{
		"darwin/home", "darwin/minimal", "linux/home", "linux/minimal", "windows/home", "windows/minimal",
	}
	if diff := cmp.Diff(want, labels); diff != "" {
		t.Errorf("Configs() (-want +got):\n%s", diff)
	}
}

func TestParseSpec(t *testing.T) {
	t.Parallel()
	got, fs := ParseSpec(map[string]any{"matrix": map[string]any{
		"seed":    map[string]any{"name": "octocat"},
		"profile": map[string]any{"home": map[string]any{"featureX": true, "name": "hubot"}, "minimal": map[string]any{}},
		"variant": []any{
			map[string]any{"name": "one tool", "profiles": []any{"home", "minimal"}, "set": map[string]any{"tools": []any{"alpha"}}},
			map[string]any{"name": "all", "set": map[string]any{"flag": false}},
		},
	}})
	if len(fs) > 0 {
		t.Fatalf("ParseSpec() findings = %v, want none", fs)
	}
	want := Spec{
		Seed:    map[string]any{"name": "octocat"},
		Profile: map[string]map[string]any{"home": {"featureX": true, "name": "hubot"}, "minimal": {}},
		Variants: []Variant{
			{Name: "one tool", Profiles: []string{"home", "minimal"}, Set: map[string]any{"tools": []any{"alpha"}}},
			{Name: "all", Set: map[string]any{"flag": false}},
		},
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("ParseSpec() (-want +got):\n%s", diff)
	}
	if got, fs := ParseSpec(map[string]any{}); len(fs) != 0 || !cmp.Equal(got, Spec{}) {
		t.Errorf("ParseSpec(absent) = %v, %v, want zero and none", got, fs)
	}
}

func TestParseSpecFindings(t *testing.T) {
	t.Parallel()
	variant := func(v map[string]any) map[string]any {
		return map[string]any{"variant": []any{v}}
	}
	set := map[string]any{"a": "b"}
	tests := []struct {
		name      string
		matrix    any
		where, in string
	}{
		{"not a table", "x", "matrix", "must be a table"},
		{"unknown key", map[string]any{"extra": 1}, "matrix.extra", "unknown key"},
		{"seed not a table", map[string]any{"seed": "x"}, "matrix.seed", "must be a table"},
		{"profile not a table", map[string]any{"profile": []any{}}, "matrix.profile", "must be a table"},
		{"profile answers not a table", map[string]any{"profile": map[string]any{"home": 1}}, "matrix.profile.home", "must be a table"},
		{"profile bad name", map[string]any{"profile": map[string]any{"Home": map[string]any{}}}, "matrix.profile.Home", "invalid profile name"},
		{"profile name starts with dash", map[string]any{"profile": map[string]any{"-x": map[string]any{}}}, "matrix.profile.-x", "invalid profile name"},
		{"chezmoi reserved in seed", map[string]any{"seed": map[string]any{"chezmoi": "x"}}, "matrix.seed.chezmoi", "reserved"},
		{"chezmoi reserved in profile", map[string]any{"profile": map[string]any{"home": map[string]any{"chezmoi": "x"}}}, "matrix.profile.home.chezmoi", "reserved"},
		{"chezmoi reserved in set", variant(map[string]any{"name": "v", "set": map[string]any{"chezmoi": "x"}}), "matrix.variant[0].set.chezmoi", "reserved"},
		{"key not bare", map[string]any{"seed": map[string]any{"my key": "v"}}, "matrix.seed.my key", "not a bare TOML key"},
		{"key with dot", map[string]any{"seed": map[string]any{"a.b": "v"}}, "matrix.seed.a.b", "not a bare TOML key"},
		{"variant empty profiles", variant(map[string]any{"name": "v", "set": set, "profiles": []any{}}), "matrix.variant[0].profiles", "profiles is empty"},
		{"variant name not a string", variant(map[string]any{"name": float64(1), "set": set}), "matrix.variant[0].name", "name must be a string"},
		{"number value", map[string]any{"seed": map[string]any{"n": float64(1)}}, "matrix.seed.n", "string, a boolean or a list of strings"},
		{"mixed list", map[string]any{"seed": map[string]any{"l": []any{"a", true}}}, "matrix.seed.l", "list of strings"},
		{"table value", map[string]any{"seed": map[string]any{"t": map[string]any{}}}, "matrix.seed.t", "list of strings"},
		{"variant not an array", map[string]any{"variant": "x"}, "matrix.variant", "array of tables"},
		{"variant not a table", map[string]any{"variant": []any{"x"}}, "matrix.variant[0]", "must be a table"},
		{"variant unknown key", variant(map[string]any{"name": "v", "set": set, "extra": 1}), "matrix.variant[0].extra", "unknown key"},
		{"variant no name", variant(map[string]any{"set": set}), "matrix.variant[0]", "missing name"},
		{"variant empty name", variant(map[string]any{"name": "", "set": set}), "matrix.variant[0]", "missing name"},
		{"variant duplicate", map[string]any{"variant": []any{
			map[string]any{"name": "v", "set": set}, map[string]any{"name": "v", "set": set},
		}}, "matrix.variant[1]", "duplicate name"},
		{"variant profiles not a list", variant(map[string]any{"name": "v", "set": set, "profiles": "home"}), "matrix.variant[0].profiles", "list of profiles"},
		{"variant unknown profile", variant(map[string]any{"name": "v", "set": set, "profiles": []any{"boss"}}), "matrix.variant[0].profiles", "unknown profile boss"},
		{"variant names default when declared", map[string]any{"profile": map[string]any{"home": map[string]any{}}, "variant": []any{map[string]any{"name": "v", "set": set, "profiles": []any{"default"}}}}, "matrix.variant[0].profiles", "unknown profile default"},
		{"variant no set", variant(map[string]any{"name": "v"}), "matrix.variant[0]", "no set"},
		{"variant empty set", variant(map[string]any{"name": "v", "set": map[string]any{}}), "matrix.variant[0]", "no set"},
		{"variant set not a table", variant(map[string]any{"name": "v", "set": "x"}), "matrix.variant[0].set", "must be a table"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, fs := ParseSpec(map[string]any{"matrix": tt.matrix})
			for _, f := range fs {
				if f.Where == tt.where && strings.Contains(f.Message, tt.in) {
					return
				}
			}
			t.Errorf("ParseSpec() findings = %v, want one at %q containing %q", fs, tt.where, tt.in)
		})
	}
}

func TestAnswers(t *testing.T) {
	t.Parallel()
	s := Spec{
		Seed:    map[string]any{"name": "octocat", "tier": "base"},
		Profile: map[string]map[string]any{"home": {"tier": "home"}, "minimal": {}},
	}
	tests := []struct {
		profile string
		want    map[string]any
	}{
		{"home", map[string]any{"name": "octocat", "tier": "home"}},
		{"minimal", map[string]any{"name": "octocat", "tier": "base"}},
	}
	for _, tt := range tests {
		t.Run(tt.profile, func(t *testing.T) {
			t.Parallel()
			got := s.Answers(tt.profile)
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("Answers(%s) (-want +got):\n%s", tt.profile, diff)
			}
			got["name"] = "changed"
			if s.Seed["name"] != "octocat" {
				t.Error("Answers returns the spec's own map, want a copy")
			}
		})
	}
	if diff := cmp.Diff(map[string]any{"name": "octocat"}, Spec{Seed: map[string]any{"name": "octocat"}}.Answers("default")); diff != "" {
		t.Errorf("default profile Answers (-want +got):\n%s", diff)
	}
}

func TestExpand(t *testing.T) {
	t.Parallel()
	base := []Entry{
		{Config: Config{"darwin", "a"}, Overrides: chez.Overrides{"tools": []any{}, "keep": "x"}},
		{Config: Config{"darwin", "b"}, Overrides: chez.Overrides{"tools": []any{}, "keep": "x"}},
		{Config: Config{"darwin", "c"}, Overrides: chez.Overrides{"keep": "x"}},
	}
	s := Spec{Variants: []Variant{
		{Name: "one", Profiles: []string{"b"}, Set: map[string]any{"tools": []any{"alpha"}}},
		{Name: "every", Set: map[string]any{"flag": true}},
	}}
	got := s.Expand(base)
	labels := make([]string, 0, len(got))
	for _, e := range got {
		labels = append(labels, e.Label())
	}
	wantLabels := []string{
		"darwin/a", "darwin/b", "darwin/c",
		"darwin/b [one]",
		"darwin/a [every]", "darwin/b [every]", "darwin/c [every]",
	}
	if diff := cmp.Diff(wantLabels, labels); diff != "" {
		t.Errorf("Expand() labels (-want +got):\n%s", diff)
	}
	wantOne := chez.Overrides{"tools": []any{"alpha"}, "keep": "x"}
	if diff := cmp.Diff(wantOne, got[3].Overrides); diff != "" {
		t.Errorf("variant overrides (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff(chez.Overrides{"tools": []any{}, "keep": "x"}, base[1].Overrides); diff != "" {
		t.Errorf("base overrides changed (-want +got):\n%s", diff)
	}
	if base[1].Variant != "" {
		t.Errorf("base entry changed: %v", base[1])
	}
	if got := (Spec{}).Expand(base); len(got) != 3 {
		t.Errorf("Expand without variants has %d entries, want 3", len(got))
	}
}

func TestEntryLabel(t *testing.T) {
	t.Parallel()
	e := Entry{Config: Config{"linux", "home"}}
	if got := e.Label(); got != "linux/home" {
		t.Errorf("Label() = %q", got)
	}
	e.Variant = "one tool"
	if got := e.Label(); got != "linux/home [one tool]" {
		t.Errorf("Label() = %q", got)
	}
}

func TestUniform(t *testing.T) {
	t.Parallel()
	data := map[string]map[string]any{
		"linux/a": {"tools": []any{"a"}, "on": true, "n": float64(2), "s": "x", "m": map[string]any{"k": 1}},
		"linux/b": {"tools": []any{"b"}},
		"linux/c": {},
	}
	machine := map[string]any{"tools": []any{"z"}, "machineOnly": "x", "machineList": []any{"y"}, "chezmoi": map[string]any{"os": "linux"}}
	uniform(data, machine)
	want := map[string]any{"tools": []any{}, "on": false, "n": float64(0), "s": "", "m": map[string]any{}, "machineOnly": "", "machineList": []any{}}
	if diff := cmp.Diff(want, data["linux/c"]); diff != "" {
		t.Errorf("linux/c (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff([]any{"b"}, data["linux/b"]["tools"]); diff != "" {
		t.Errorf("linux/b tools changed (-want +got):\n%s", diff)
	}
}

func TestTOML(t *testing.T) {
	t.Parallel()
	got := TOML(map[string]any{"b": true, "a": `x"y`, "c": []string{"p", "q"}, "d": []any{"r", "s"}})
	want := "[data]\na = \"x\\\"y\"\nb = true\nc = [\"p\", \"q\"]\nd = [\"r\", \"s\"]\n"
	if got != want {
		t.Errorf("TOML() = %q, want %q", got, want)
	}
}

func TestQuote(t *testing.T) {
	t.Parallel()
	tests := []struct{ name, in, want string }{
		{"plain", "abc", `"abc"`},
		{"quote and backslash", `a"b\c`, `"a\"b\\c"`},
		{"named controls", "\b\t\n\f\r", `"\b\t\n\f\r"`},
		{"other controls", "\x01\a\v\x1f\x7f", `"\u0001\u0007\u000B\u001F\u007F"`},
		{"unicode kept", "caffè ☃", `"caffè ☃"`},
		{"empty", "", `""`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := quote(tt.in); got != tt.want {
				t.Errorf("quote(%q) = %s, want %s", tt.in, got, tt.want)
			}
		})
	}
}
