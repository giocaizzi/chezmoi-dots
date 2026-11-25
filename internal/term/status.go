package term

import (
	"fmt"
	"io"
	"time"

	"github.com/giocaizzi/chezmoi-dots/internal/chez"
	"github.com/giocaizzi/chezmoi-dots/internal/inventory"
)

// Status prints the dotfiles checkout against its upstream and what chezmoi apply
// would change.
func (s Style) Status(w io.Writer, t inventory.Tree, entries []chez.StatusEntry, now time.Time) {
	fmt.Fprintf(w, "%sdotfiles%s  %s\n", s.B, s.O, s.treeText(t, now))
	if len(entries) == 0 {
		fmt.Fprintf(w, "%sapply%s     %sup to date%s\n", s.B, s.O, s.G, s.O)
		return
	}
	verb := "differ"
	if len(entries) == 1 {
		verb = "differs"
	}
	fmt.Fprintf(w, "%sapply%s     %s%s %s (chezmoi diff shows them)%s\n",
		s.B, s.O, s.Y, inventory.Count(len(entries), "target"), verb, s.O)
	for _, e := range entries {
		fmt.Fprintf(w, "  %c%c %s\n", e.Last, e.Apply, e.Path)
	}
}
