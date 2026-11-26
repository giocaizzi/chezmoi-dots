package ghrelease

import (
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/giocaizzi/chezmoi-dots/internal/archive"
)

func exe(paths ...string) []archive.File {
	out := make([]archive.File, len(paths))
	for i, p := range paths {
		out[i] = archive.File{Path: p, Exec: true}
	}
	return out
}

func TestProposePath(t *testing.T) {
	t.Parallel()
	names := []string{"tool", "example-tool"}
	three := Proposal{Version: "1.2.3", Targets: map[string]string{
		"linux/amd64": "linux_amd64", "darwin/arm64": "darwin_arm64", "windows/amd64": "windows_amd64",
	}}
	tests := []struct {
		name       string
		p          Proposal
		files      map[string][]archive.File
		wantPath   string
		wantOK     bool
		wantCands  map[string][]string
		candidates bool // compare candidates
	}{
		{
			name: "top level directory with version and target",
			p:    three,
			files: map[string][]archive.File{
				"linux/amd64":   exe("tool_1.2.3_linux_amd64/tool", "tool_1.2.3_linux_amd64/README"),
				"darwin/arm64":  exe("tool_1.2.3_darwin_arm64/tool"),
				"windows/amd64": exe("tool_1.2.3_windows_amd64/tool.exe"),
			},
			wantPath: "tool_{version}_{target}/tool", wantOK: true,
		},
		{
			name: "target values carry the archive extension",
			p: Proposal{Version: "1.2.3", Targets: map[string]string{
				"linux/amd64": "linux_amd64.tar.gz", "windows/amd64": "windows_amd64.zip",
			}},
			files: map[string][]archive.File{
				"linux/amd64":   exe("tool_1.2.3_linux_amd64/tool"),
				"windows/amd64": exe("tool_1.2.3_windows_amd64/tool.exe"),
			},
			wantCands: map[string][]string{
				"linux/amd64": {"tool_1.2.3_linux_amd64/tool"}, "windows/amd64": {"tool_1.2.3_windows_amd64/tool.exe"},
			},
			candidates: true,
		},
		{
			name: "target values carry the extension and the directory holds the full value",
			p: Proposal{Version: "1.2.3", Targets: map[string]string{
				"linux/amd64": "linux_amd64.tar.gz", "windows/amd64": "windows_amd64.zip",
			}},
			files: map[string][]archive.File{
				"linux/amd64":   exe("linux_amd64.tar.gz/tool"),
				"windows/amd64": exe("windows_amd64.zip/tool.exe"),
			},
			wantPath: "{target}/tool", wantOK: true,
		},
		{
			name: "flat archives",
			p:    three,
			files: map[string][]archive.File{
				"linux/amd64": exe("tool"), "darwin/arm64": exe("tool"), "windows/amd64": exe("tool.EXE"),
			},
			wantPath: "tool", wantOK: true,
		},
		{
			name:     "name is the repository name, not the entry name",
			p:        three,
			files:    map[string][]archive.File{"linux/amd64": exe("bin/example-tool"), "darwin/arm64": exe("bin/example-tool")},
			wantPath: "bin/example-tool", wantOK: true,
		},
		{
			name: "v-prefixed version directory",
			p:    Proposal{Version: "1.2.3", Targets: map[string]string{"linux/amd64": "linux-x64"}},
			files: map[string][]archive.File{
				"linux/amd64": exe("tool-v1.2.3/linux-x64/tool"),
			},
			wantPath: "tool-v{version}/{target}/tool", wantOK: true,
		},
		{
			name: "version only inside a longer token is kept",
			p:    Proposal{Version: "1.2.3", Targets: map[string]string{"linux/amd64": "linux"}},
			files: map[string][]archive.File{
				"linux/amd64": exe("tool-1.2.30/tool"),
			},
			wantPath: "tool-1.2.30/tool", wantOK: true,
		},
		{
			name:  "darwin universal asset serves both arches",
			p:     Proposal{Version: "1.2.3", Targets: map[string]string{"darwin/amd64": "darwin_all", "darwin/arm64": "darwin_all"}},
			files: map[string][]archive.File{"darwin/amd64": exe("tool_darwin_all/tool"), "darwin/arm64": exe("tool_darwin_all/tool")},

			wantPath: "tool_{target}/tool", wantOK: true,
		},
		{
			name: "mixed listable and unlistable targets",
			p:    three,
			files: map[string][]archive.File{
				"linux/amd64": exe("tool"), "darwin/arm64": nil, "windows/amd64": exe("tool.exe"),
			},
			wantPath: "tool", wantOK: true,
		},
		{
			name:  "no target is listable",
			p:     three,
			files: map[string][]archive.File{"linux/amd64": nil, "darwin/arm64": nil},
		},
		{
			name: "none matches: executable files are reported",
			p:    three,
			files: map[string][]archive.File{
				"linux/amd64":  {{Path: "x/run", Exec: true}, {Path: "x/LICENSE"}},
				"darwin/arm64": exe("a", "b", "c", "d", "e", "f", "g"),
			},
			wantCands:  map[string][]string{"linux/amd64": {"x/run"}, "darwin/arm64": {"a", "b", "c", "d", "e"}},
			candidates: true,
		},
		{
			name:       "several match in one target",
			p:          three,
			files:      map[string][]archive.File{"linux/amd64": exe("a/tool", "b/tool"), "darwin/arm64": exe("tool")},
			wantCands:  map[string][]string{"linux/amd64": {"a/tool", "b/tool"}, "darwin/arm64": {"tool"}},
			candidates: true,
		},
		{
			name:       "a match that is not executable does not count",
			p:          three,
			files:      map[string][]archive.File{"linux/amd64": {{Path: "tool"}, {Path: "other", Exec: true}}},
			wantCands:  map[string][]string{"linux/amd64": {"other"}},
			candidates: true,
		},
		{
			name:       "paths differ between targets",
			p:          three,
			files:      map[string][]archive.File{"linux/amd64": exe("bin/tool"), "darwin/arm64": exe("tool")},
			wantCands:  map[string][]string{"linux/amd64": {"bin/tool"}, "darwin/arm64": {"tool"}},
			candidates: true,
		},
		{
			name:       "windows needs the .exe name",
			p:          three,
			files:      map[string][]archive.File{"windows/amd64": exe("tool")},
			wantCands:  map[string][]string{"windows/amd64": {"tool"}},
			candidates: true,
		},
		{
			name:       "an empty archive is listed with no candidates",
			p:          three,
			files:      map[string][]archive.File{"linux/amd64": {}},
			wantCands:  map[string][]string{"linux/amd64": nil},
			candidates: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, ok, cands := ProposePath(names, tt.p, tt.files)
			if got != tt.wantPath || ok != tt.wantOK {
				t.Errorf("ProposePath = %q, %v, want %q, %v", got, ok, tt.wantPath, tt.wantOK)
			}
			if tt.candidates {
				if diff := cmp.Diff(tt.wantCands, cands); diff != "" {
					t.Errorf("candidates (-want +got):\n%s", diff)
				}
			}
			if !tt.wantOK && tt.wantPath != "" {
				t.Errorf("path %q with ok false", got)
			}
		})
	}
}

// TestProposePathRendersToTheListedFile checks the contract of a found path: with
// {version} and {target} replaced by the release version and the registry's target
// value (as an externals template does), plus ".exe" on windows, it names the file
// that matched in that target's archive.
func TestProposePathRendersToTheListedFile(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		p     Proposal
		files map[string][]archive.File
	}{
		{
			name: "target values without an extension",
			p:    Proposal{Version: "1.2.3", Targets: map[string]string{"linux/amd64": "linux_amd64", "windows/amd64": "windows_amd64"}},
			files: map[string][]archive.File{
				"linux/amd64": exe("tool_1.2.3_linux_amd64/tool"), "windows/amd64": exe("tool_1.2.3_windows_amd64/tool.exe"),
			},
		},
		{
			name: "target values carry the archive extension",
			p:    Proposal{Version: "1.2.3", Targets: map[string]string{"linux/amd64": "linux_amd64.tar.gz", "windows/amd64": "windows_amd64.zip"}},
			files: map[string][]archive.File{
				"linux/amd64": exe("tool_1.2.3_linux_amd64/tool"), "windows/amd64": exe("tool_1.2.3_windows_amd64/tool.exe"),
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, ok, _ := ProposePath([]string{"tool"}, tt.p, tt.files)
			if !ok {
				return // an unsettled path makes no promise
			}
			for k, list := range tt.files {
				rendered := strings.NewReplacer("{version}", tt.p.Version, "{target}", tt.p.Targets[k]).Replace(got)
				if strings.HasPrefix(k, "windows/") {
					rendered += extExe
				}
				if rendered != list[0].Path {
					t.Errorf("%s: path %q renders to %q, want the listed %q", k, got, rendered, list[0].Path)
				}
			}
		})
	}
}

func TestProposePathLeavesNoOtherTargetInThePath(t *testing.T) {
	t.Parallel()
	t.Run("one listed goreleaser target does not settle a path for the others", func(t *testing.T) {
		t.Parallel()
		p := Proposal{Version: "1.2.3", Targets: map[string]string{
			"linux/amd64": "linux_amd64.tar.gz", "darwin/arm64": "darwin_arm64.tar.xz", // .tar.xz is not listed
		}}
		files := map[string][]archive.File{"linux/amd64": exe("tool_1.2.3_linux_amd64/tool"), "darwin/arm64": nil}
		if got, ok, _ := ProposePath([]string{"tool"}, p, files); ok && strings.Contains(got, "linux_amd64") {
			t.Errorf("ProposePath = %q, true; darwin/arm64 renders it to a linux directory", got)
		}
	})
	t.Run("an arch outside amd64 and arm64 settles nothing either", func(t *testing.T) {
		t.Parallel()
		p := Proposal{Version: "1.2.3", Targets: map[string]string{
			"linux/arm": "linux_armv7.tar.gz", "linux/amd64": "linux_amd64.tar.xz", // .tar.xz is not listed
		}}
		files := map[string][]archive.File{"linux/arm": exe("armv7/tool"), "linux/amd64": nil}
		if got, ok, _ := ProposePath([]string{"tool"}, p, files); ok {
			t.Errorf("ProposePath = %q, true; linux/amd64 renders it to an armv7 directory", got)
		}
	})
	t.Run("xfail: a path every target lists is kept when a token only looks like a platform", func(t *testing.T) {
		t.Parallel()
		t.Skip("xfail: platformSpecific rejects any path with an OS or arch token, even when every target " +
			"is listed and agrees on it, so a toolchain such as arm-none-eabi-gcc or a darwin-helpers directory " +
			"gets no proposal")
		p := Proposal{Version: "1.2.3", Targets: map[string]string{"linux/amd64": "linux_amd64", "darwin/arm64": "darwin_arm64"}}
		for name, want := range map[string]string{
			"arm-none-eabi-gcc": "toolchain-{version}/bin/arm-none-eabi-gcc",
			"tool":              "darwin-helpers/tool",
		} {
			raw := strings.ReplaceAll(want, "{version}", "1.2.3")
			files := map[string][]archive.File{"linux/amd64": exe(raw), "darwin/arm64": exe(raw)}
			if got, ok, _ := ProposePath([]string{name}, p, files); !ok || got != want {
				t.Errorf("ProposePath = %q, %v; want %q, true: every target lists that same path", got, ok, want)
			}
		}
	})
}
