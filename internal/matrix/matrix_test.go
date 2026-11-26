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

func TestParseSpec(t *testing.T) {
	t.Parallel()
	got, fs := ParseSpec(map[string]any{"matrix": map[string]any{
		"seed": map[string]any{"name": "octocat"},
		"role": map[string]any{"work": map[string]any{"featureX": true, "name": "hubot"}},
		"variant": []any{
			map[string]any{"name": "one tool", "roles": []any{"personal", "work"}, "set": map[string]any{"tools": []any{"alpha"}}},
			map[string]any{"name": "all", "set": map[string]any{"flag": false}},
		},
	}})
	if len(fs) > 0 {
		t.Fatalf("ParseSpec() findings = %v, want none", fs)
	}
	want := Spec{
		Seed: map[string]any{"name": "octocat"},
		Role: map[string]map[string]any{"work": {"featureX": true, "name": "hubot"}},
		Variants: []Variant{
			{Name: "one tool", Roles: []string{"personal", "work"}, Set: map[string]any{"tools": []any{"alpha"}}},
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
		{"role not a table", map[string]any{"role": []any{}}, "matrix.role", "must be a table"},
		{"role answers not a table", map[string]any{"role": map[string]any{"work": 1}}, "matrix.role.work", "must be a table"},
		{"unknown role", map[string]any{"role": map[string]any{"boss": map[string]any{}}}, "matrix.role.boss", "unknown role"},
		{"reserved in seed", map[string]any{"seed": map[string]any{"light": true}}, "matrix.seed.light", "reserved"},
		{"reserved in role", map[string]any{"role": map[string]any{"work": map[string]any{"work": true}}}, "matrix.role.work.work", "reserved"},
		{"reserved in set", variant(map[string]any{"name": "v", "set": map[string]any{"work": true}}), "matrix.variant[0].set.work", "reserved"},
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
		{"variant roles not a list", variant(map[string]any{"name": "v", "set": set, "roles": "work"}), "matrix.variant[0].roles", "list of roles"},
		{"variant unknown role", variant(map[string]any{"name": "v", "set": set, "roles": []any{"boss"}}), "matrix.variant[0].roles", "unknown role boss"},
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
		Seed: map[string]any{"name": "octocat", "tier": "base"},
		Role: map[string]map[string]any{"work": {"tier": "work"}},
	}
	tests := []struct {
		role string
		want map[string]any
	}{
		{"personal", map[string]any{"name": "octocat", "tier": "base", "light": false, "work": false}},
		{"work", map[string]any{"name": "octocat", "tier": "work", "light": false, "work": true}},
		{"light", map[string]any{"name": "octocat", "tier": "base", "light": true, "work": false}},
	}
	for _, tt := range tests {
		t.Run(tt.role, func(t *testing.T) {
			t.Parallel()
			got := s.Answers(tt.role)
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("Answers(%s) (-want +got):\n%s", tt.role, diff)
			}
			got["name"] = "changed"
			if s.Seed["name"] != "octocat" {
				t.Error("Answers returns the spec's own map, want a copy")
			}
		})
	}
	want := map[string]any{"light": false, "work": true}
	if diff := cmp.Diff(want, Spec{}.Answers("work")); diff != "" {
		t.Errorf("zero Spec Answers(work) (-want +got):\n%s", diff)
	}
}

func TestExpand(t *testing.T) {
	t.Parallel()
	base := []Entry{
		{Config: Config{"darwin", "personal"}, Overrides: chez.Overrides{"tools": []any{}, "keep": "x"}},
		{Config: Config{"darwin", "work"}, Overrides: chez.Overrides{"tools": []any{}, "keep": "x"}},
		{Config: Config{"darwin", "light"}, Overrides: chez.Overrides{"keep": "x"}},
	}
	s := Spec{Variants: []Variant{
		{Name: "one", Roles: []string{"work"}, Set: map[string]any{"tools": []any{"alpha"}}},
		{Name: "every", Set: map[string]any{"flag": true}},
	}}
	got := s.Expand(base)
	labels := make([]string, 0, len(got))
	for _, e := range got {
		labels = append(labels, e.Label())
	}
	wantLabels := []string{
		"darwin/personal", "darwin/work", "darwin/light",
		"darwin/work [one]",
		"darwin/personal [every]", "darwin/work [every]", "darwin/light [every]",
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
	e := Entry{Config: Config{"linux", "work"}}
	if got := e.Label(); got != "linux/work" {
		t.Errorf("Label() = %q", got)
	}
	e.Variant = "one tool"
	if got := e.Label(); got != "linux/work [one tool]" {
		t.Errorf("Label() = %q", got)
	}
}

func TestUniform(t *testing.T) {
	t.Parallel()
	data := map[string]map[string]any{
		"personal": {"tools": []any{"a"}, "on": true, "n": float64(2), "s": "x", "m": map[string]any{"k": 1}},
		"work":     {"tools": []any{"b"}},
		"light":    {},
	}
	machine := map[string]any{"tools": []any{"z"}, "machineOnly": "x", "machineList": []any{"y"}, "chezmoi": map[string]any{"os": "linux"}}
	uniform(data, machine)
	want := map[string]any{"tools": []any{}, "on": false, "n": float64(0), "s": "", "m": map[string]any{}, "machineOnly": "", "machineList": []any{}}
	if diff := cmp.Diff(want, data["light"]); diff != "" {
		t.Errorf("light (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff([]any{"b"}, data["work"]["tools"]); diff != "" {
		t.Errorf("work tools changed (-want +got):\n%s", diff)
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

// Review findings on the declaration checks. Each subtest is an x-failure: remove
// its t.Skip once ParseSpec reports the finding.
func TestParseSpecReviewXFail(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, skip string
		matrix     any
		where, in  string
	}{
		{
			// Expand layers set over the entry's overrides, so a `chezmoi` answer replaces
			// the per-OS {"os": ...} override and every OS renders alike.
			name: "xfail: chezmoi is reserved in set", skip: "xfail: ParseSpec accepts a set that replaces the chezmoi override",
			matrix: map[string]any{"variant": []any{map[string]any{"name": "v", "set": map[string]any{"chezmoi": "x"}}}},
			where:  "matrix.variant[0].set.chezmoi", in: "reserved",
		},
		{
			// An explicit empty list silently means every role, like an absent one.
			name: "xfail: an empty roles list is a finding", skip: "xfail: ParseSpec treats roles = [] as every role",
			matrix: map[string]any{"variant": []any{map[string]any{"name": "v", "roles": []any{}, "set": map[string]any{"a": "b"}}}},
			where:  "matrix.variant[0].roles", in: "",
		},
		{
			// TOML writes keys bare, so a declared key that needs quoting breaks the
			// seed config at render time while lint matrix passes.
			name: "xfail: a key TOML cannot write bare is a finding or is quoted", skip: "xfail: ParseSpec accepts it and TOML writes it bare",
			matrix: map[string]any{"seed": map[string]any{"my key": "v"}},
			where:  "matrix.seed.my key", in: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			t.Skip(tt.skip)
			spec, fs := ParseSpec(map[string]any{"matrix": tt.matrix})
			for _, f := range fs {
				if f.Where == tt.where && strings.Contains(f.Message, tt.in) {
					return
				}
			}
			if tt.where == "matrix.seed.my key" && strings.Contains(TOML(spec.Answers("work")), `"my key" = "v"`) {
				return // quoting the key is a fix too
			}
			t.Errorf("ParseSpec() findings = %v, want one at %q containing %q", fs, tt.where, tt.in)
		})
	}
}
