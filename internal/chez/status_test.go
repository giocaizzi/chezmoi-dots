package chez_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/giocaizzi/chezmoi-dots/internal/chez"
)

func TestParseStatus(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		in   string
		want []chez.StatusEntry
	}{
		{"empty", "", nil},
		{"blank lines", "\n\n", nil},
		{"one", "MM .zshrc\n", []chez.StatusEntry{{Last: 'M', Apply: 'M', Path: ".zshrc"}}},
		{"leading space column", " A .config/example/config.toml", []chez.StatusEntry{{Last: ' ', Apply: 'A', Path: ".config/example/config.toml"}}},
		{"script and delete", "DD old\n R run_once.sh\r\n", []chez.StatusEntry{{Last: 'D', Apply: 'D', Path: "old"}, {Last: ' ', Apply: 'R', Path: "run_once.sh"}}},
		{"path with spaces", " M my file.txt", []chez.StatusEntry{{Last: ' ', Apply: 'M', Path: "my file.txt"}}},
		{"unreadable skipped", "garbage\nXX nope\nM\n MM\n M ok", []chez.StatusEntry{{Last: ' ', Apply: 'M', Path: "ok"}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if diff := cmp.Diff(tt.want, chez.ParseStatus(tt.in)); diff != "" {
				t.Errorf("ParseStatus() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
