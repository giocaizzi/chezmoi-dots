package cli

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

// writeFixtures makes files named names in a temp dir, each holding "old <name>".
func writeFixtures(t *testing.T, names ...string) (files []string, texts map[string]string) {
	t.Helper()
	dir := t.TempDir()
	texts = map[string]string{}
	for _, n := range names {
		f := filepath.Join(dir, n)
		if err := os.WriteFile(f, []byte("old "+n), 0o640); err != nil {
			t.Fatal(err)
		}
		files = append(files, f)
		texts[f] = "new " + n
	}
	return files, texts
}

func readAll(t *testing.T, files []string) []string {
	t.Helper()
	var got []string
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		got = append(got, string(b))
	}
	return got
}

// leftovers lists the entries of dir that are not in keep.
func leftovers(t *testing.T, dir string, keep []string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var extra []string
	for _, e := range entries {
		if !slices.ContainsFunc(keep, func(k string) bool { return filepath.Base(k) == e.Name() }) {
			extra = append(extra, e.Name())
		}
	}
	return extra
}

func TestWriteAll(t *testing.T) {
	t.Parallel()
	files, texts := writeFixtures(t, "a.toml", "b.toml")
	if err := writeAll(files, texts, os.Rename); err != nil {
		t.Fatal(err)
	}
	if diff := cmp.Diff([]string{"new a.toml", "new b.toml"}, readAll(t, files)); diff != "" {
		t.Errorf("contents (-want +got):\n%s", diff)
	}
	if extra := leftovers(t, filepath.Dir(files[0]), files); len(extra) != 0 {
		t.Errorf("temp files left behind: %v", extra)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(files[0])
		if err != nil {
			t.Fatal(err)
		}
		if got, want := info.Mode().Perm(), os.FileMode(0o640); got != want {
			t.Errorf("mode = %v, want %v (the original's)", got, want)
		}
	}
}

func TestWriteAllPrepareFailureChangesNothing(t *testing.T) {
	t.Parallel()
	files, texts := writeFixtures(t, "a.toml")
	missing := filepath.Join(filepath.Dir(files[0]), "gone.toml") // cannot be stat'ed, so cannot be prepared
	texts[missing] = "new"
	renamed := false
	err := writeAll(append(files, missing), texts, func(string, string) error { renamed = true; return nil })
	if err == nil || !strings.Contains(err.Error(), missing) {
		t.Fatalf("writeAll() = %v, want an error naming %s", err, missing)
	}
	if renamed {
		t.Error("renamed a file although a later one could not be prepared")
	}
	if diff := cmp.Diff([]string{"old a.toml"}, readAll(t, files)); diff != "" {
		t.Errorf("contents (-want +got):\n%s", diff)
	}
	if extra := leftovers(t, filepath.Dir(files[0]), files); len(extra) != 0 {
		t.Errorf("temp files left behind: %v", extra)
	}
}

func TestWriteAllRenameFailureNamesWrittenFiles(t *testing.T) {
	t.Parallel()
	files, texts := writeFixtures(t, "a.toml", "b.toml", "c.toml")
	boom := errors.New("boom")
	err := writeAll(files, texts, func(from, to string) error {
		if to == files[1] {
			return boom
		}
		return os.Rename(from, to)
	})
	if !errors.Is(err, boom) {
		t.Fatalf("writeAll() = %v, want boom", err)
	}
	for _, want := range []string{files[1], "already written: " + files[0]} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q lacks %q", err, want)
		}
	}
	if diff := cmp.Diff([]string{"new a.toml", "old b.toml", "old c.toml"}, readAll(t, files)); diff != "" {
		t.Errorf("contents (-want +got):\n%s", diff)
	}
	if extra := leftovers(t, filepath.Dir(files[0]), files); len(extra) != 0 {
		t.Errorf("temp files left behind: %v", extra)
	}
}
