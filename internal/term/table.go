package term

import (
	"fmt"
	"io"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Width is the number of terminal cells s occupies, ignoring ANSI escapes. East
// Asian wide runes take two cells; combining marks and zero-width joiners and
// variation selectors take none.
func Width(s string) int {
	w := 0
	for i := 0; i < len(s); {
		if s[i] == '\x1b' {
			j := i + 1
			for j < len(s) && (s[j] < '@' || s[j] > '~' || s[j] == '[') {
				j++
			}
			i = j + 1
			continue
		}
		r, size := utf8.DecodeRuneInString(s[i:])
		i += size
		w += runeWidth(r)
	}
	return w
}

func runeWidth(r rune) int {
	switch {
	case r == 0x200D, r >= 0xFE00 && r <= 0xFE0F,
		unicode.Is(unicode.Mn, r), unicode.Is(unicode.Me, r):
		return 0
	}
	// Inclusive rune ranges that take two terminal cells.
	wideRanges := [][2]rune{
		{0x1100, 0x115F},
		{0x2E80, 0x303E},
		{0x3041, 0x33FF},
		{0x3400, 0x4DBF},
		{0x4E00, 0x9FFF},
		{0xA000, 0xA4CF},
		{0xAC00, 0xD7A3},
		{0xF900, 0xFAFF},
		{0xFE30, 0xFE4F},
		{0xFF00, 0xFF60},
		{0xFFE0, 0xFFE6},
		{0x1F300, 0x1F64F},
		{0x1F900, 0x1F9FF},
		{0x20000, 0x2FFFD},
		{0x30000, 0x3FFFD},
	}
	for _, rg := range wideRanges {
		if r >= rg[0] && r <= rg[1] {
			return 2
		}
	}
	return 1
}

// Pad right-pads s with spaces to width visible columns.
func Pad(s string, width int) string {
	if n := width - Width(s); n > 0 {
		return s + strings.Repeat(" ", n)
	}
	return s
}

// Table writes an aligned table indented by two spaces: a dim header, then the
// rows, columns two spaces apart. Cells may carry their own colour escapes.
func (s Style) Table(w io.Writer, header []string, rows [][]string) {
	widths := make([]int, len(header))
	for i, h := range header {
		widths[i] = Width(h)
	}
	for _, r := range rows {
		for i, c := range r {
			if i < len(widths) {
				widths[i] = max(widths[i], Width(c))
			}
		}
	}
	line := func(cells []string) string {
		var b strings.Builder
		for i, c := range cells {
			if i >= len(widths) {
				break
			}
			if i == len(cells)-1 || i == len(widths)-1 {
				b.WriteString(c)
				break
			}
			b.WriteString(Pad(c, widths[i]+2))
		}
		return strings.TrimRight(b.String(), " ")
	}
	fmt.Fprintf(w, "  %s%s%s\n", s.D, line(header), s.O)
	for _, r := range rows {
		fmt.Fprintf(w, "  %s\n", line(r))
	}
}
