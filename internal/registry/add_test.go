package registry_test

import (
	"path/filepath"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/giocaizzi/chezmoi-dots/internal/registry"
)

func TestInferName(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		path   string
		isDir  bool
		taken  map[string]bool
		want   string
		wantOK bool
	}{
		{"file", "/h/.config/app/settings.toml", false, nil, "settings", true},
		{"dotfile", "/h/.zshrc", false, nil, "zshrc", true},
		{"folder keeps dots inside", "/h/.config/my.app", true, nil, "my-app", true},
		{"sanitized upper", "/h/Some File.toml", false, nil, "some-file", true},
		{"collision uses parent", "/h/.config/app/settings.toml", false, map[string]bool{"settings": true}, "app-settings", true},
		{"hidden parent", "/h/.config/settings.toml", false, map[string]bool{"settings": true}, "config-settings", true},
		{"both taken", "/h/app/settings.toml", false, map[string]bool{"settings": true, "app-settings": true}, "", false},
		{"windows style", filepath.Join("C:", "Users", "x", "AppData", "conf.toml"), false, nil, "conf", true},
		{"empty stem", "/h/...", false, nil, "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, ok := registry.InferName(tt.path, tt.isDir, func(n string) bool { return tt.taken[n] })
			if got != tt.want || ok != tt.wantOK {
				t.Errorf("InferName = %q, %v; want %q, %v", got, ok, tt.want, tt.wantOK)
			}
		})
	}
}

func TestAppend(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, old, desc, where, value, want string
	}{
		{"empty", "", "d", "source", "a.toml", "[edit.x]\ndesc = \"d\"\nsource = \"a.toml\"\n"},
		{"no trailing newline", "[edit.y]\ndesc = \"y\"", "d", "local", ".config/app", "[edit.y]\ndesc = \"y\"\n\n[edit.x]\ndesc = \"d\"\nlocal = \".config/app\"\n"},
		{"trailing newline", "a = 1\n", "d", "source", "s", "a = 1\n\n[edit.x]\ndesc = \"d\"\nsource = \"s\"\n"},
		{"escapes", "", "say \"hi\" \\ x\tz\n", "source", "p", "[edit.x]\ndesc = \"say \\\"hi\\\" \\\\ x\\u0009z\\u000A\"\nsource = \"p\"\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := registry.Append(tt.old, "x", tt.desc, tt.where, tt.value)
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("Append mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
