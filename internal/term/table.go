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
	case r >= 0x200B && r <= 0x200F, r >= 0x2060 && r <= 0x2064, r == 0xFEFF,
		r >= 0xFE00 && r <= 0xFE0F,
		unicode.Is(unicode.Mn, r), unicode.Is(unicode.Me, r):
		return 0
	}
	// Inclusive rune ranges that take two terminal cells.
	wideRanges := [][2]rune{
		{0x1100, 0x115F},
		{0x231A, 0x231B},
		{0x2329, 0x232A},
		{0x23E9, 0x23EC},
		{0x23F0, 0x23F0},
		{0x23F3, 0x23F3},
		{0x25FD, 0x25FE},
		{0x2614, 0x2615},
		{0x2648, 0x2653},
		{0x267F, 0x267F},
		{0x2693, 0x2693},
		{0x26A1, 0x26A1},
		{0x26AA, 0x26AB},
		{0x26BD, 0x26BE},
		{0x26C4, 0x26C5},
		{0x26CE, 0x26CE},
		{0x26D4, 0x26D4},
		{0x26EA, 0x26EA},
		{0x26F2, 0x26F3},
		{0x26F5, 0x26F5},
		{0x26FA, 0x26FA},
		{0x26FD, 0x26FD},
		{0x2705, 0x2705},
		{0x270A, 0x270B},
		{0x2728, 0x2728},
		{0x274C, 0x274C},
		{0x274E, 0x274E},
		{0x2753, 0x2755},
		{0x2757, 0x2757},
		{0x2795, 0x2797},
		{0x27B0, 0x27B0},
		{0x27BF, 0x27BF},
		{0x2B1B, 0x2B1C},
		{0x2B50, 0x2B50},
		{0x2B55, 0x2B55},
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
		{0x1F004, 0x1F004},
		{0x1F0CF, 0x1F0CF},
		{0x1F18E, 0x1F18E},
		{0x1F191, 0x1F19A},
		{0x1F200, 0x1F202},
		{0x1F210, 0x1F23B},
		{0x1F240, 0x1F248},
		{0x1F250, 0x1F251},
		{0x1F260, 0x1F265},
		{0x1F300, 0x1F64F},
		{0x1F680, 0x1F6FF},
		{0x1F7E0, 0x1F7EB},
		{0x1F90C, 0x1F9FF},
		{0x1FA70, 0x1FAFF},
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
