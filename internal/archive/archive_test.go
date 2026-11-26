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
	zw, err := gzip.NewWriterLevel(&buf, gzip.BestSpeed) // the entry-cap fixture is slow at the default level
	if err != nil {
		t.Fatal(err)
	}
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
		many[i] = entry{name: fmt.Sprintf("d%d/", i), mode: 0o755, typ: tar.TypeDir} // every entry counts, not only regular files
	}
	tests := []struct {
		name    string
		kind    archive.Kind
		data    []byte
		overCap bool  // data is MaxEntries+1 entries of kind, written in the parallel subtest (slow)
		want    error // nil: any error
	}{
		{name: "tar.gz over the entry cap", kind: archive.TarGz, overCap: true, want: archive.ErrTooManyEntries},
		{name: "zip over the entry cap", kind: archive.Zip, overCap: true, want: archive.ErrTooManyEntries},
		{name: "not gzip", kind: archive.TarGz, data: []byte("not an archive")},
		{name: "not zip", kind: archive.Zip, data: []byte("not an archive")},
		{name: "unlistable kind", kind: archive.None},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			data := tt.data
			switch {
			case tt.overCap && tt.kind == archive.TarGz:
				data = tarGz(t, many)
			case tt.overCap:
				data = zipBytes(t, many, true)
			}
			_, err := archive.List(tt.kind, bytes.NewReader(data))
			if err == nil || (tt.want != nil && !errors.Is(err, tt.want)) {
				t.Errorf("List = %v, want %v", err, tt.want)
			}
		})
	}
}

func TestListBoundsNameMemory(t *testing.T) {
	t.Parallel()
	t.Run("long entry names are rejected, so a small download cannot fill memory", func(t *testing.T) {
		t.Parallel()
		var buf bytes.Buffer
		zw := gzip.NewWriter(&buf)
		tw := tar.NewWriter(zw)
		long := strings.Repeat("a", 1<<20-16) // a GNU long name just under archive/tar's 1 MiB limit
		for i := range 128 {
			hdr := &tar.Header{Name: fmt.Sprintf("%s%d", long, i), Mode: 0o755, Typeflag: tar.TypeReg, Format: tar.FormatGNU}
			if err := tw.WriteHeader(hdr); err != nil {
				t.Fatal(err)
			}
		}
		if err := tw.Close(); err != nil {
			t.Fatal(err)
		}
		if err := zw.Close(); err != nil {
			t.Fatal(err)
		}
		if files, err := archive.List(archive.TarGz, &buf); !errors.Is(err, archive.ErrNameTooLong) {
			t.Errorf("List = %d files, %v; want %v", len(files), err, archive.ErrNameTooLong)
		}
	})
	t.Run("total name bytes are capped", func(t *testing.T) {
		t.Parallel()
		entries := make([]entry, 10)
		for i := range entries {
			entries[i] = entry{name: fmt.Sprintf("dir/file-%d", i), mode: 0o644}
		}
		l := archive.DefaultLimits()
		l.MaxNameBytes = 50 // each name is 11 bytes
		for kind, data := range map[archive.Kind][]byte{
			archive.TarGz: tarGz(t, entries),
			archive.Zip:   zipBytes(t, entries, true),
		} {
			if _, err := archive.ListLimits(kind, bytes.NewReader(data), l); !errors.Is(err, archive.ErrNamesTooLarge) {
				t.Errorf("kind %v: ListLimits = %v, want %v", kind, err, archive.ErrNamesTooLarge)
			}
		}
	})
	t.Run("zip names over the length cap are rejected", func(t *testing.T) {
		t.Parallel()
		data := zipBytes(t, []entry{{name: strings.Repeat("a", 5000), mode: 0o644}}, true)
		if _, err := archive.List(archive.Zip, bytes.NewReader(data)); !errors.Is(err, archive.ErrNameTooLong) {
			t.Errorf("List = %v, want %v", err, archive.ErrNameTooLong)
		}
	})
}

func TestListZipNamesMatchChezmoi(t *testing.T) {
	t.Parallel()
	t.Run("xfail: backslashes in zip names are rewritten, chezmoi matches the raw name", func(t *testing.T) {
		t.Parallel()
		t.Skip("xfail: clean turns `d\\tool.exe` into `d/tool.exe`, but chezmoi's archive-file walk compares " +
			"path.Clean of the raw zip name, so the proposed path is not found at apply time")
		got, err := archive.List(archive.Zip, bytes.NewReader(zipBytes(t, []entry{{name: `d\tool.exe`, mode: 0o755}}, false)))
		if err != nil {
			t.Fatal(err)
		}
		if want := []archive.File{{Path: `d\tool.exe`, Exec: true}}; !cmp.Equal(want, got) {
			t.Errorf("List = %v, want %v (the name chezmoi matches)", got, want)
		}
	})
}

func TestListCapsDecompressedStream(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(zw)
	const size = 1 << 20
	if err := tw.WriteHeader(&tar.Header{Name: "big", Mode: 0o644, Typeflag: tar.TypeReg, Size: size}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(make([]byte, size)); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	l := archive.DefaultLimits()
	l.MaxDecompressed = size / 2
	if _, err := archive.ListLimits(archive.TarGz, bytes.NewReader(buf.Bytes()), l); !errors.Is(err, archive.ErrTooLarge) {
		t.Errorf("ListLimits = %v, want %v", err, archive.ErrTooLarge)
	}
	l.MaxDecompressed = 2 * size
	got, err := archive.ListLimits(archive.TarGz, bytes.NewReader(buf.Bytes()), l)
	if err != nil || len(got) != 1 {
		t.Errorf("ListLimits under the cap = %v, %v; want one file", got, err)
	}
}
