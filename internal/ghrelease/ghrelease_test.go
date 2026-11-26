package ghrelease

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
)

const releaseJSON = `{"tag_name":"v1.2.3","assets":[{"name":"tool_linux_amd64.tar.gz","browser_download_url":"https://example.com/octocat/tool/releases/download/v1.2.3/tool_linux_amd64.tar.gz"}]}`

func TestLatest(t *testing.T) { //nolint:gocognit // table test with an inline server
	t.Parallel()
	env := func(m map[string]string) func(string) string { return func(k string) string { return m[k] } }
	tests := []struct {
		name    string
		repo    string
		getenv  func(string) string
		handler http.HandlerFunc
		client  Client
		want    Release
		wantErr string
		wantTok string
	}{
		{
			name:   "happy path without token",
			repo:   "octocat/tool",
			getenv: env(nil),
			handler: func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/repos/octocat/tool/releases/latest" || r.Header.Get("Accept") != "application/vnd.github+json" {
					http.Error(w, "bad request", http.StatusBadRequest)
					return
				}
				_, _ = w.Write([]byte(releaseJSON))
			},
			want: Release{Tag: "v1.2.3", Assets: []Asset{{
				Name: "tool_linux_amd64.tar.gz",
				URL:  "https://example.com/octocat/tool/releases/download/v1.2.3/tool_linux_amd64.tar.gz",
			}}},
		},
		{
			name:    "GH_TOKEN wins",
			repo:    "octocat/tool",
			getenv:  env(map[string]string{"GH_TOKEN": "a", "GITHUB_TOKEN": "b"}),
			handler: func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`{"tag_name":"v1"}`)) },
			want:    Release{Tag: "v1"},
			wantTok: "Bearer a",
		},
		{
			name:    "GITHUB_TOKEN fallback",
			repo:    "octocat/tool",
			getenv:  env(map[string]string{"GITHUB_TOKEN": "b"}),
			handler: func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`{"tag_name":"v1"}`)) },
			want:    Release{Tag: "v1"},
			wantTok: "Bearer b",
		},
		{
			name:    "not found",
			repo:    "octocat/tool",
			handler: func(w http.ResponseWriter, _ *http.Request) { http.NotFound(w, nil) },
			wantErr: "404",
		},
		{
			name:    "oversized body",
			repo:    "octocat/tool",
			client:  Client{MaxBytes: 16},
			handler: func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(releaseJSON)) },
			wantErr: "over 16 bytes",
		},
		{
			name:   "timeout",
			repo:   "octocat/tool",
			client: Client{Timeout: 20 * time.Millisecond},
			handler: func(_ http.ResponseWriter, r *http.Request) {
				<-r.Context().Done()
			},
			wantErr: "deadline exceeded",
		},
		{
			name:    "bad json",
			repo:    "octocat/tool",
			handler: func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`{`)) },
			wantErr: "decode",
		},
		{name: "invalid repo", repo: "octocat", wantErr: "invalid repository"},
		{name: "traversal repo", repo: "../..", wantErr: "invalid repository"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var gotTok string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotTok = r.Header.Get("Authorization")
				tt.handler(w, r)
			}))
			t.Cleanup(srv.Close)
			c := tt.client
			c.BaseURL, c.HTTP, c.Getenv = srv.URL, srv.Client(), tt.getenv
			got, err := c.Latest(t.Context(), tt.repo)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("Latest() error = %v, want containing %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("Latest() error = %v", err)
			}
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("Latest() mismatch (-want +got):\n%s", diff)
			}
			if gotTok != tt.wantTok {
				t.Errorf("Authorization = %q, want %q", gotTok, tt.wantTok)
			}
		})
	}
}

// release builds a release whose assets live under the usual download path.
func release(tag string, names ...string) Release {
	rel := Release{Tag: tag}
	for _, n := range names {
		rel.Assets = append(rel.Assets, Asset{Name: n, URL: "https://example.com/octocat/tool/releases/download/" + tag + "/" + n})
	}
	return rel
}

const dl = "https://example.com/octocat/tool/releases/download/"

func TestMap(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		rel     Release
		want    Proposal
		wantErr string
	}{
		{
			name: "goreleaser archives",
			rel: release("v1.2.3",
				"tool_1.2.3_darwin_amd64.tar.gz", "tool_1.2.3_darwin_arm64.tar.gz",
				"tool_1.2.3_linux_amd64.tar.gz", "tool_1.2.3_linux_arm64.tar.gz",
				"tool_1.2.3_windows_amd64.zip", "tool_1.2.3_checksums.txt", "tool_1.2.3_linux_386.tar.gz",
				"tool_1.2.3_linux_amd64.deb"),
			want: Proposal{
				Version: "1.2.3",
				URL:     dl + "v{version}/tool_{version}_{target}",
				Type:    "archive-file",
				Path:    "tool",
				Exe:     true,
				Targets: map[string]string{
					"darwin/amd64": "darwin_amd64.tar.gz", "darwin/arm64": "darwin_arm64.tar.gz",
					"linux/amd64": "linux_amd64.tar.gz", "linux/arm64": "linux_arm64.tar.gz",
					"windows/amd64": "windows_amd64.zip",
				},
				Unmapped: []string{"tool_1.2.3_checksums.txt", "tool_1.2.3_linux_386.tar.gz", "tool_1.2.3_linux_amd64.deb"},
			},
		},
		{
			name: "bare binaries",
			rel: release("v1.2.3", "tool_1.2.3_linux_amd64", "tool_1.2.3_linux_arm64",
				"tool_1.2.3_darwin_arm64", "tool_1.2.3_windows_amd64.exe"),
			want: Proposal{
				Version: "1.2.3",
				URL:     dl + "v{version}/tool_{version}_{target}",
				Type:    "file",
				Exe:     true,
				Targets: map[string]string{
					"linux/amd64": "linux_amd64", "linux/arm64": "linux_arm64",
					"darwin/arm64": "darwin_arm64", "windows/amd64": "windows_amd64.exe",
				},
			},
		},
		{
			name: "x86_64 and aarch64",
			rel: release("1.0.0", "tool-x86_64-unknown-linux-gnu.tar.gz", "tool-aarch64-unknown-linux-gnu.tar.gz",
				"tool-x86_64-apple-darwin.tar.gz"),
			want: Proposal{
				Version: "1.0.0",
				URL:     dl + "{version}/tool-{target}.tar.gz",
				Type:    "archive-file",
				Path:    "tool",
				Targets: map[string]string{
					"linux/amd64": "x86_64-unknown-linux-gnu", "linux/arm64": "aarch64-unknown-linux-gnu",
					"darwin/amd64": "x86_64-apple-darwin",
				},
			},
		},
		{
			name: "universal darwin",
			rel:  release("v2.0.0", "tool_2.0.0_darwin_universal.tar.gz", "tool_2.0.0_linux_amd64.tar.gz"),
			want: Proposal{
				Version: "2.0.0",
				URL:     dl + "v{version}/tool_{version}_{target}.tar.gz",
				Type:    "archive-file",
				Path:    "tool",
				Targets: map[string]string{
					"darwin/amd64": "darwin_universal", "darwin/arm64": "darwin_universal",
					"linux/amd64": "linux_amd64",
				},
			},
		},
		{
			name: "universal ignored when darwin is specific",
			rel:  release("v2.0.0", "tool_2.0.0_darwin_universal.tar.gz", "tool_2.0.0_darwin_arm64.tar.gz", "tool_2.0.0_linux_amd64.tar.gz"),
			want: Proposal{
				Version: "2.0.0",
				URL:     dl + "v{version}/tool_{version}_{target}.tar.gz",
				Type:    "archive-file",
				Path:    "tool",
				Targets: map[string]string{
					"darwin/arm64": "darwin_arm64", "linux/amd64": "linux_amd64",
				},
				Unmapped: []string{"tool_2.0.0_darwin_universal.tar.gz"},
			},
		},
		{
			name: "tar.gz beats zip on linux, zip on windows",
			rel: release("v1.0.0", "tool_1.0.0_linux_amd64.tar.gz", "tool_1.0.0_linux_amd64.zip",
				"tool_1.0.0_windows_amd64.tar.gz", "tool_1.0.0_windows_amd64.zip"),
			want: Proposal{
				Version: "1.0.0",
				URL:     dl + "v{version}/tool_{version}_{target}",
				Type:    "archive-file",
				Path:    "tool",
				Exe:     true,
				Targets: map[string]string{"linux/amd64": "linux_amd64.tar.gz", "windows/amd64": "windows_amd64.zip"},
			},
		},
		{
			name: "ambiguous duplicates",
			rel: release("v1.0.0", "tool_1.0.0_linux_amd64.tar.gz", "tool_1.0.0_linux_x64.tar.gz",
				"tool_1.0.0_darwin_arm64.tar.gz"),
			want: Proposal{
				Version:   "1.0.0",
				URL:       dl + "v{version}/tool_{version}_{target}.tar.gz",
				Type:      "archive-file",
				Path:      "tool",
				Targets:   map[string]string{"darwin/arm64": "darwin_arm64"},
				Unmapped:  []string{"tool_1.0.0_linux_amd64.tar.gz", "tool_1.0.0_linux_x64.tar.gz"},
				Ambiguous: []string{"linux/amd64: tool_1.0.0_linux_amd64.tar.gz or tool_1.0.0_linux_x64.tar.gz"},
			},
		},
		{
			name: "single target",
			rel:  release("v0.9.1", "tool-v0.9.1-linux-amd64.tar.gz"),
			want: Proposal{
				Version: "0.9.1",
				URL:     dl + "v{version}/tool-v{version}-{target}.tar.gz",
				Type:    "archive-file",
				Path:    "tool",
				Targets: map[string]string{"linux/amd64": "linux-amd64"},
			},
		},
		{
			name:    "mixed kinds",
			rel:     release("v1.0.0", "tool_1.0.0_linux_amd64.tar.gz", "tool_1.0.0_darwin_arm64"),
			wantErr: "mixed archive and binary assets",
		},
		{
			name:    "nothing maps",
			rel:     release("v1.0.0", "checksums.txt", "tool_1.0.0_freebsd_amd64.tar.gz", "source.tar.gz"),
			wantErr: "no asset maps to an os/arch target",
		},
		{
			name:    "no shared pattern",
			rel:     release("v1.0.0", "tool_1.0.0_linux_amd64.tar.gz", "other-1.0.0-darwin-arm64.zip"),
			wantErr: "assets do not share one name pattern",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := Map("octocat/tool", tt.rel)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("Map() error = %v, want containing %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("Map() error = %v", err)
			}
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("Map() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// TestMapVersionOnlyInWholeTokens: the version is templated only where it is a whole
// name token, so a version that also occurs inside the tool name ("tool2", tag v2)
// still gives a URL that holds for the next version.
func TestMapVersionOnlyInWholeTokens(t *testing.T) {
	t.Parallel()
	p, err := Map("octocat/tool2", release("v2", "tool2-linux-amd64.tar.gz", "tool2-darwin-arm64.tar.gz"))
	if err != nil {
		return // refusing the release is safe
	}
	got := strings.NewReplacer("{version}", "3", "{target}", p.Targets["linux/amd64"]).Replace(p.URL)
	if want := dl + "v3/tool2-linux-amd64.tar.gz"; got != want {
		t.Errorf("URL %q at version 3 = %q, want %q", p.URL, got, want)
	}
}

func TestTemplVersion(t *testing.T) {
	t.Parallel()
	tests := []struct{ name, asset, version, want string }{
		{"inside a name", "tool2-linux-amd64.tar.gz", "2", "tool2-linux-amd64.tar.gz"},
		{"underscore token", "tool_2_linux_amd64.tar.gz", "2", "tool_{version}_linux_amd64.tar.gz"},
		{"dotted version", "tool_1.2.3_linux.tar.gz", "1.2.3", "tool_{version}_linux.tar.gz"},
		{"v prefix", "tool-v1.2.3.tar.gz", "1.2.3", "tool-v{version}.tar.gz"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := templVersion(tt.asset, tt.version, 0, len(tt.asset)); got != tt.want {
				t.Errorf("templVersion(%q, %q) = %q, want %q", tt.asset, tt.version, got, tt.want)
			}
		})
	}
}
