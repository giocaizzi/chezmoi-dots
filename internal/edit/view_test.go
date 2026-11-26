package edit

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/giocaizzi/chezmoi-dots/internal/matrix"
	"github.com/giocaizzi/chezmoi-dots/internal/registry"
)

func entriesFor(profiles ...string) []matrix.Entry {
	out := make([]matrix.Entry, 0, len(profiles)*len(matrix.OSes()))
	for _, o := range matrix.OSes() {
		for _, p := range profiles {
			out = append(out, matrix.Entry{OS: o, Profile: p})
		}
	}
	return out
}

func TestResolveProfile(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		as       matrix.Config
		profiles []string
		want     matrix.Config
		wantErr  string
	}{
		{"single profile is the default", matrix.Config{OS: "linux"}, []string{"default"}, matrix.Config{OS: "linux", Profile: "default"}, ""},
		{"named profile", matrix.Config{OS: "linux", Profile: "home"}, []string{"home", "minimal"}, matrix.Config{OS: "linux", Profile: "home"}, ""},
		{"profile needed", matrix.Config{OS: "linux"}, []string{"home", "minimal"}, matrix.Config{}, "profiles: home, minimal"},
		{"unknown profile", matrix.Config{OS: "linux", Profile: "boss"}, []string{"home", "minimal"}, matrix.Config{}, `unknown profile "boss" (profiles: home, minimal)`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := resolveProfile(tt.as, tt.profiles)
			if tt.wantErr != "" {
				if !errors.Is(err, ErrUsage) || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("resolveProfile() error = %v, want a usage error containing %q", err, tt.wantErr)
				}
				return
			}
			if err != nil || got != tt.want {
				t.Errorf("resolveProfile() = %v, %v, want %v", got, err, tt.want)
			}
		})
	}
}

func TestViewAsNeedsProfile(t *testing.T) {
	t.Parallel()
	r := newRig(t, registry.Entry{Name: "a", Desc: "d", Source: "a.md"}, st("1"))
	r.svc.Matrix = func(context.Context, string) ([]matrix.Entry, error) { return entriesFor("home", "minimal"), nil }
	err := r.svc.View(t.Context(), "a", ViewAs, matrix.Config{OS: "linux"})
	if !errors.Is(err, ErrUsage) || !strings.Contains(err.Error(), "home, minimal") {
		t.Errorf("View() = %v, want a usage error listing the profiles", err)
	}
}

func TestViewOverviewListsProfiles(t *testing.T) {
	t.Parallel()
	r := newRig(t, registry.Entry{Name: "a", Desc: "d", Source: "a.md"}, st("1"))
	r.svc.Matrix = func(context.Context, string) ([]matrix.Entry, error) { return entriesFor("work", "web"), nil }
	var out bytes.Buffer
	r.svc.Out = &out
	if err := r.svc.ViewOverview(t.Context()); err != nil {
		t.Fatalf("ViewOverview() = %v", err)
	}
	want := "▸ matrix overview (6 configurations, profiles: Wo=work · We=web)"
	if first, _, _ := strings.Cut(out.String(), "\n"); first != want {
		t.Errorf("overview title = %q, want %q", first, want)
	}
}

func TestProfilesOf(t *testing.T) {
	t.Parallel()
	if diff := cmp.Diff([]string{"a", "b"}, profilesOf(entriesFor("a", "b"))); diff != "" {
		t.Errorf("profilesOf() (-want +got):\n%s", diff)
	}
}
