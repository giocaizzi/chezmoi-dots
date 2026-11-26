package term

import (
	"fmt"
	"io"
	"strings"
)

// Symbols for matrix preview grids and overview tables.
const (
	SymSquareFilled = "■"
	SymSquareEmpty  = "□"
	SymDot          = "·"
	SymCross        = "✗"
)

// Palette returns the distinct highlight colours for matrix preview diffs:
// Green, Mauve, Cyan, Yellow, Blue.
func (s Style) Palette() []string {
	return []string{s.G, s.M, s.C, s.Y, s.B}
}

// MatrixGrid prints an OS × profile block: a row per OS, a column per profile headed
// by its short label (matrix.Short).
func (s Style) MatrixGrid(w io.Writer, profiles []string, short map[string]string, cell func(os, profile string) (glyph, color string)) {
	oses := []string{"darwin", "linux", "windows"}
	width := 1
	for _, p := range profiles {
		width = max(width, visibleWidth(short[p]))
	}

	var h strings.Builder
	fmt.Fprintf(&h, "%-12s", "")
	for _, p := range profiles {
		h.WriteString("  " + padRight(short[p], width))
	}
	fmt.Fprintln(w, strings.TrimRight(h.String(), " "))
	for _, osName := range oses {
		var b strings.Builder
		fmt.Fprintf(&b, "%-12s", osName)
		for i, p := range profiles {
			glyph, color := cell(osName, p)
			if color != "" && s.O != "" {
				glyph = color + glyph + s.O
			}
			b.WriteString("  ")
			if i < len(profiles)-1 {
				glyph = padRight(glyph, width)
			}
			b.WriteString(glyph)
		}
		fmt.Fprintln(w, b.String())
	}
}

// OverviewRow represents one row in the matrix preview overview table.
type OverviewRow struct {
	Entry   string // entry name (e.g. "ghostty")
	Darwin  string // one symbol per profile, e.g. "■ ■ □" or "· · ·"
	Linux   string // one symbol per profile, e.g. "■ ■ □"
	Windows string // one symbol per profile, e.g. "□ □ □"
	Targets string // description e.g. "2 targets (macOS, Linux)" or "1 target (all)"
}

// OverviewTable prints a formatted table of all entries across the matrix, whose OS
// blocks hold one symbol for each of the given number of profiles.
func (s Style) OverviewTable(w io.Writer, profiles int, rows []OverviewRow) {
	width := max(10, 2*profiles+1)
	header := "  " + padRight("ENTRY", 22) + padRight("DARWIN", width) + padRight("LINUX", width) + padRight("WINDOWS", width) + "TARGETS"
	if s.D != "" {
		fmt.Fprintf(w, "%s%s%s\n", s.D, header, s.O)
	} else {
		fmt.Fprintln(w, header)
	}
	for _, r := range rows {
		entry := r.Entry
		if s.M != "" && !strings.Contains(entry, "\x1b[") {
			entry = s.M + entry + s.O
		}
		targets := r.Targets
		if s.D != "" && !strings.Contains(targets, "\x1b[") {
			targets = s.D + targets + s.O
		}
		fmt.Fprintf(w, "  %s%s%s%s%s\n",
			padRight(entry, 22),
			padRight(r.Darwin, width),
			padRight(r.Linux, width),
			padRight(r.Windows, width),
			targets,
		)
	}
}

func visibleWidth(s string) int {
	inEscape := false
	w := 0
	for _, r := range s {
		if r == '\x1b' {
			inEscape = true
			continue
		}
		if inEscape {
			if (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') {
				inEscape = false
			}
			continue
		}
		w++
	}
	return w
}

func padRight(s string, width int) string {
	vw := visibleWidth(s)
	if vw >= width {
		return s
	}
	return s + strings.Repeat(" ", width-vw)
}
