package release

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/giocaizzi/chezmoi-dots/internal/archive"
	"github.com/giocaizzi/chezmoi-dots/internal/pkgreg"
)

func tarGzOf(t *testing.T, name string, mode int64, trailing int) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(zw)
	if err := tw.WriteHeader(&tar.Header{Name: name, Mode: mode, Size: 1, Typeflag: tar.TypeReg}); err != nil {
		t.Fatal(err)
	}
	_, _ = tw.Write([]byte("x"))
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	_, _ = zw.Write(make([]byte, trailing)) // bytes after the tar end must still be hashed
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func zipOf(t *testing.T, name string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create(name)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = w.Write([]byte("x"))
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func sumOf(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

func TestInspect(t *testing.T) {
	t.Parallel()
	bodies := map[string][]byte{
		"/linux.tar.gz": tarGzOf(t, "tool_1/tool", 0o755, 4096),
		"/win.zip":      zipOf(t, "tool_1/tool.exe"),
		"/mac.tar.xz":   []byte("an xz archive"),
		"/bad.tar.gz":   []byte("not gzip at all"),
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, ok := bodies[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)
	rel := pkgreg.Release{Name: "tool", Version: "1", URL: srv.URL + "/{target}", Targets: map[string]string{
		"linux/amd64": "linux.tar.gz", "windows/amd64": "win.zip", "darwin/arm64": "mac.tar.xz", "darwin/amd64": "bad.tar.gz",
	}}
	f := Finder{HTTP: srv.Client()}

	sums, files, err := f.Inspect(t.Context(), rel, "1")
	if err != nil {
		t.Fatal(err)
	}
	wantSums := map[string]string{
		"linux/amd64": sumOf(bodies["/linux.tar.gz"]), "windows/amd64": sumOf(bodies["/win.zip"]),
		"darwin/arm64": sumOf(bodies["/mac.tar.xz"]), "darwin/amd64": sumOf(bodies["/bad.tar.gz"]),
	}
	if diff := cmp.Diff(wantSums, sums); diff != "" {
		t.Errorf("sums (-want +got):\n%s", diff)
	}
	wantFiles := map[string][]archive.File{
		"linux/amd64":   {{Path: "tool_1/tool", Exec: true}},
		"windows/amd64": {{Path: "tool_1/tool.exe", Exec: true}},
		"darwin/arm64":  nil, // .tar.xz is not listed
		"darwin/amd64":  nil, // unreadable
	}
	if diff := cmp.Diff(wantFiles, files); diff != "" {
		t.Errorf("files (-want +got):\n%s", diff)
	}

	plain, err := f.Sums(t.Context(), rel, "1")
	if err != nil {
		t.Fatal(err)
	}
	if diff := cmp.Diff(wantSums, plain); diff != "" {
		t.Errorf("Sums (-want +got):\n%s", diff)
	}
	one, err := f.SHA256(t.Context(), srv.URL+"/linux.tar.gz")
	if err != nil || one != wantSums["linux/amd64"] {
		t.Errorf("SHA256 = %q, %v, want %q", one, err, wantSums["linux/amd64"])
	}
}

func TestInspectKeepsLimits(t *testing.T) {
	t.Parallel()
	body := tarGzOf(t, "tool", 0o755, 4096)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = http.NewResponseController(w).Flush() // chunked: no Content-Length
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)
	rel := pkgreg.Release{Name: "tool", Version: "1", URL: srv.URL + "/{target}.tar.gz", Targets: map[string]string{"linux/amd64": "a"}}

	tests := []struct {
		name string
		max  int64
		fail bool
	}{
		{"exactly the cap", int64(len(body)), false},
		{"one byte over the cap", int64(len(body)) - 1, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			f := Finder{HTTP: srv.Client(), MaxBytes: tt.max}
			_, _, err := f.Inspect(t.Context(), rel, "1")
			if tt.fail != (err != nil) || (err != nil && !strings.Contains(err.Error(), "larger than")) {
				t.Errorf("Inspect = %v, want failure %v", err, tt.fail)
			}
		})
	}
}
