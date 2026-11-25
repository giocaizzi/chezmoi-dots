package term_test

import (
	"bytes"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/giocaizzi/chezmoi-dots/internal/chez"
	"github.com/giocaizzi/chezmoi-dots/internal/inventory"
	"github.com/giocaizzi/chezmoi-dots/internal/term"
)

func TestStatus(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 1, 2, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name    string
		tree    inventory.Tree
		entries []chez.StatusEntry
		want    string
	}{
		{
			"clean",
			inventory.Tree{},
			nil,
			"dotfiles  not a git checkout\napply     up to date\n",
		},
		{
			"one target",
			inventory.Tree{Known: true, Branch: "main", Upstream: "origin/main", Fetched: now},
			[]chez.StatusEntry{{Last: ' ', Apply: 'A', Path: ".zshrc"}},
			"dotfiles  main · up to date with upstream · fetched just now\napply     1 target differs (chezmoi diff shows them)\n   A .zshrc\n",
		},
		{
			"two targets",
			inventory.Tree{Known: true, Branch: "main", Upstream: "origin/main", Behind: 2, Dirty: 1},
			[]chez.StatusEntry{{Last: 'M', Apply: 'M', Path: ".zshrc"}, {Last: ' ', Apply: 'A', Path: ".config/example/config.toml"}},
			"dotfiles  main · 2 behind upstream · 1 local change · not fetched\napply     2 targets differ (chezmoi diff shows them)\n  MM .zshrc\n   A .config/example/config.toml\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var out bytes.Buffer
			term.NewStyle(&out, func(string) string { return "" }).Status(&out, tt.tree, tt.entries, now)
			if diff := cmp.Diff(tt.want, out.String()); diff != "" {
				t.Errorf("Status() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
