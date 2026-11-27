package term_test

import (
	"bytes"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/giocaizzi/chezmoi-dots/internal/term"
)

func TestWidth(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		in   string
		want int
	}{
		{name: "plain", in: "abc", want: 3},
		{name: "colored", in: "\x1b[1;34mabc\x1b[0m", want: 3},
		{name: "multibyte", in: "$1 · #3", want: 7},
		{name: "empty", in: "", want: 0},
		{name: "cjk", in: "設定ファイル", want: 12},                   //nolint:gosmopolitan // wide runes are the case under test
		{name: "cjk colored", in: "\x1b[35m設定\x1b[0m", want: 4}, //nolint:gosmopolitan // wide runes are the case under test
		{name: "combining mark", in: "e\u0301", want: 1},
		{name: "precomposed", in: "é", want: 1},
		{name: "zero width joiner", in: "a\u200db", want: 2},
		{name: "variation selector", in: "■\ufe0f", want: 1},
		{name: "ambiguous symbols", in: "■ □ · ◇", want: 7},
		{name: "emoji outside the wide ranges", in: "\U0001F680", want: 2},
		{name: "emoji presentation symbol", in: "✅", want: 2},
		{name: "zero width space", in: "a\u200bb", want: 2},
		{name: "test tube", in: "🧪", want: 2},
		{name: "extended pictographs", in: "\U0001FA90", want: 2},
		{name: "watch", in: "⌚", want: 2},
		{name: "word joiner", in: "a\u2060b", want: 2},
		{name: "byte order mark", in: "a\ufeffb", want: 2},
		{name: "ambiguous diamond", in: "◇", want: 1},
		{name: "ambiguous cross", in: "✗", want: 1},
		{name: "trigram", in: "\u2630", want: 2},
		{name: "yijing hexagram", in: "\u4dc0", want: 2},
		{name: "hangul jamo extended-A", in: "\ua960", want: 2},
		{name: "vertical form", in: "\ufe10", want: 2},
		{name: "small form", in: "\ufe50", want: 2},
		{name: "tangut", in: "\U00017000", want: 2},
		{name: "tangut components", in: "\U00018800", want: 2},
		{name: "kana supplement", in: "\U0001B000", want: 2},
		{name: "nushu", in: "\U0001B170", want: 2},
		{name: "tai xuan jing", in: "\U0001D300", want: 2},
		{name: "ambiguous parenthesized ideograph", in: "\u3248", want: 1},
		{name: "circled number on black square", in: "\u3250", want: 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := term.Width(tt.in); got != tt.want {
				t.Errorf("Width(%q) = %d, want %d", tt.in, got, tt.want)
			}
		})
	}
}

func TestPad(t *testing.T) {
	t.Parallel()
	if got, want := term.Pad("\x1b[35mab\x1b[0m", 4), "\x1b[35mab\x1b[0m  "; got != want {
		t.Errorf("Pad = %q, want %q", got, want)
	}
	if got, want := term.Pad("abcdef", 3), "abcdef"; got != want {
		t.Errorf("Pad = %q, want %q", got, want)
	}
}

func TestTable(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	s := term.Style{D: "<d>", O: "</>"}
	s.Table(&buf, []string{"MODEL", "TEXT", "CODE"}, [][]string{
		{"GPT-6 Luna", "#12", ""},
		{"Claude Haiku 5.5", "", "#3"},
	})
	want := "  <d>MODEL             TEXT  CODE</>\n" +
		"  GPT-6 Luna        #12\n" +
		"  Claude Haiku 5.5        #3\n"
	if diff := cmp.Diff(want, buf.String()); diff != "" {
		t.Errorf("Table() mismatch (-want +got):\n%s", diff)
	}
}

func TestTable_WideCell(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	term.Style{}.Table(&buf, []string{"NAME", "VALUE"}, [][]string{{"設定", "x"}, {"abcd", "y"}}) //nolint:gosmopolitan // wide runes are the case under test
	want := "  NAME  VALUE\n  設定  x\n  abcd  y\n"                                               //nolint:gosmopolitan // wide runes are the case under test
	if diff := cmp.Diff(want, buf.String()); diff != "" {
		t.Errorf("Table() mismatch (-want +got):\n%s", diff)
	}
}
