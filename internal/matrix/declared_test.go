package matrix_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/giocaizzi/chezmoi-dots/internal/matrix"
)

func TestDeclaredProfiles(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		data map[string]any
		want []string
	}{
		{"no matrix", map[string]any{}, nil},
		{"matrix without profiles", map[string]any{"matrix": map[string]any{"seed": map[string]any{"a": "b"}}}, nil},
		{"matrix not a table", map[string]any{"matrix": "x"}, nil},
		{"profiles sorted", map[string]any{"matrix": map[string]any{"profile": map[string]any{
			"minimal": map[string]any{}, "home": map[string]any{},
		}}}, []string{"home", "minimal"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if diff := cmp.Diff(tt.want, matrix.DeclaredProfiles(tt.data)); diff != "" {
				t.Errorf("DeclaredProfiles mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
