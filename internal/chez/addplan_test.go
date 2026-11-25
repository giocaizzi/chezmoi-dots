package chez

import (
	"errors"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestAddPlan(t *testing.T) {
	t.Parallel()
	const chain = "diff --git a/dot_config b/dot_config\nnew file mode 40755\nindex 0000000..0000000\n" +
		"diff --git a/dot_config/example b/dot_config/example\nnew file mode 40755\n" +
		"diff --git a/dot_config/example/config.toml.tmpl b/dot_config/example/config.toml.tmpl\nnew file mode 100600\nindex 0000000..e69de29\n--- /dev/null\n+++ b/dot_config/example/config.toml.tmpl\n@@ -0,0 +1 @@\n+x\n"
	tests := []struct {
		name, out, want string
		fail            bool
	}{
		{"directory chain and template", chain, "dot_config/example/config.toml.tmpl", false},
		{"plain file", "diff --git a/dot_zshrc b/dot_zshrc\nnew file mode 100644\n", "dot_zshrc", false},
		{"executable with spaces", "diff --git a/dot_my tool b/dot_my tool\nnew file mode 100755\n", "dot_my tool", false},
		{"nothing planned", "", "", true},
		{"directories only", "diff --git a/dot_config b/dot_config\nnew file mode 40755\n", "", true},
		{"two files", "diff --git a/a b/a\nnew file mode 100644\ndiff --git a/b b/b\nnew file mode 100644\n", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r := &fakeRunner{stdout: tt.out}
			got, err := New(r, "chezmoi", "--config", "/c").AddPlan(t.Context(), "/h/.x", "--template")
			if (err != nil) != tt.fail {
				t.Fatalf("AddPlan() error = %v, want failure %v", err, tt.fail)
			}
			if got != tt.want {
				t.Errorf("AddPlan() = %q, want %q", got, tt.want)
			}
			want := []string{"chezmoi", "--config", "/c", "--dry-run", "--verbose", "add", "--template", "/h/.x"}
			if diff := cmp.Diff(want, r.calls[0]); diff != "" {
				t.Errorf("argv (-want +got):\n%s", diff)
			}
		})
	}
	t.Run("chezmoi error", func(t *testing.T) {
		t.Parallel()
		r := &fakeRunner{stderr: "boom\n", err: errors.New("exit 1")}
		if _, err := New(r, "chezmoi").AddPlan(t.Context(), "/h/.x"); err == nil {
			t.Error("AddPlan() succeeded on a chezmoi failure")
		}
	})
}
