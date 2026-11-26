package archive_test

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"io/fs"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/giocaizzi/chezmoi-dots/internal/archive"
)

type entry struct {
	name string
	mode int64
	typ  byte // tar type flag; 0 means a regular file
	link string
}

func tarGz(t *testing.T, entries []entry) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(zw)
	for _, e := range entries {
		typ := e.typ
		if typ == 0 {
			typ = tar.TypeReg
		}
		hdr := &tar.Header{Name: e.name, Mode: e.mode, Typeflag: typ, Linkname: e.link}
		if typ == tar.TypeReg {
			hdr.Size = 1
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatal(err)
		}
		if typ == tar.TypeReg {
			if _, err := tw.Write([]byte("x")); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// zipBytes writes a zip whose entries are made by a unix creator when unix is set.
func zipBytes(t *testing.T, entries []entry, unix bool) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, e := range entries {
		hdr := &zip.FileHeader{Name: e.name, Method: zip.Store}
		if unix {
			hdr.CreatorVersion = 3 << 8
			mode := fs.FileMode(e.mode)
			if strings.HasSuffix(e.name, "/") {
				mode |= fs.ModeDir
			}
			hdr.SetMode(mode)
		}
		w, err := zw.CreateHeader(hdr)
		if err != nil {
			t.Fatal(err)
		}
		if strings.HasSuffix(e.name, "/") {
			continue
		}
		if _, err := w.Write([]byte("x")); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestKindOf(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		want archive.Kind
	}{
		{"tool_1.2.3_linux_amd64.tar.gz", archive.TarGz},
		{"tool.TGZ", archive.TarGz},
		{"tool.zip", archive.Zip},
		{"tool.tar.xz", archive.None},
		{"tool.exe", archive.None},
		{"tool", archive.None},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := archive.KindOf(tt.name); got != tt.want {
				t.Errorf("KindOf(%q) = %v, want %v", tt.name, got, tt.want)
			}
		})
	}
}

func TestListTarGz(t *testing.T) {
	t.Parallel()
	data := tarGz(t, []entry{
		{name: "tool_1/", mode: 0o755, typ: tar.TypeDir},
		{name: "./tool_1/tool", mode: 0o755},
		{name: "tool_1/README.md", mode: 0o644},
		{name: "tool_1/link", mode: 0o777, typ: tar.TypeSymlink, link: "tool"},
		{name: "tool_1/hard", mode: 0o755, typ: tar.TypeLink, link: "tool_1/tool"},
	})
	got, err := archive.List(archive.TarGz, bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	want := []archive.File{{Path: "tool_1/tool", Exec: true}, {Path: "tool_1/README.md", Exec: false}}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("List (-want +got):\n%s", diff)
	}
}

func TestListZip(t *testing.T) {
	t.Parallel()
	entries := []entry{{name: "d/", mode: 0o755}, {name: "d/tool", mode: 0o755}, {name: "d/notes.txt", mode: 0o644}}
	tests := []struct {
		name string
		unix bool
		want []archive.File
	}{
		{"unix modes", true, []archive.File{{Path: "d/tool", Exec: true}, {Path: "d/notes.txt", Exec: false}}},
		{"modes unknown count as executable", false, []archive.File{{Path: "d/tool", Exec: true}, {Path: "d/notes.txt", Exec: true}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := archive.List(archive.Zip, bytes.NewReader(zipBytes(t, entries, tt.unix)))
			if err != nil {
				t.Fatal(err)
			}
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("List (-want +got):\n%s", diff)
			}
		})
	}
}

func TestListZipSkipsUnixSymlink(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	hdr := &zip.FileHeader{Name: "link", Method: zip.Store, CreatorVersion: 3 << 8}
	hdr.SetMode(fs.ModeSymlink | 0o777)
	w, err := zw.CreateHeader(hdr)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = w.Write([]byte("tool"))
	if err = zw.Close(); err != nil {
		t.Fatal(err)
	}
	got, err := archive.List(archive.Zip, &buf)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("List = %v, want the symlink skipped", got)
	}
}

func TestListErrors(t *testing.T) {
	t.Parallel()
	many := make([]entry, archive.MaxEntries+1)
	for i := range many {
		many[i] = entry{name: fmt.Sprintf("f%d", i), mode: 0o644}
	}
	tests := []struct {
		name string
		kind archive.Kind
		data []byte
		want error // nil: any error
	}{
		{"tar.gz over the entry cap", archive.TarGz, tarGz(t, many), archive.ErrTooManyEntries},
		{"zip over the entry cap", archive.Zip, zipBytes(t, many, true), archive.ErrTooManyEntries},
		{"not gzip", archive.TarGz, []byte("not an archive"), nil},
		{"not zip", archive.Zip, []byte("not an archive"), nil},
		{"unlistable kind", archive.None, nil, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := archive.List(tt.kind, bytes.NewReader(tt.data))
			if err == nil || (tt.want != nil && !errors.Is(err, tt.want)) {
				t.Errorf("List = %v, want %v", err, tt.want)
			}
		})
	}
}
