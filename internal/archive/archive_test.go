package archive_test

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math"
	"slices"
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

// tarGzFormat writes a .tar.gz of one regular file with the name in the given format.
func tarGzFormat(t *testing.T, format tar.Format, name string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(zw)
	if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o755, Typeflag: tar.TypeReg, Format: format}); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// archive/tar accepts GNU and PAX names up to 1 MiB, which compress about 1000:1, so a
// small download could fill memory if List kept them: one byte over the cap is refused.
func TestListRejectsLongNames(t *testing.T) {
	t.Parallel()
	maxLen := archive.DefaultLimits().MaxNameLen
	kinds := []struct {
		name string
		kind archive.Kind
		make func(t *testing.T, name string) []byte
	}{
		{"gnu tar", archive.TarGz, func(t *testing.T, n string) []byte { t.Helper(); return tarGzFormat(t, tar.FormatGNU, n) }},
		{"pax tar", archive.TarGz, func(t *testing.T, n string) []byte { t.Helper(); return tarGzFormat(t, tar.FormatPAX, n) }},
		{"zip", archive.Zip, func(t *testing.T, n string) []byte {
			t.Helper()
			return zipBytes(t, []entry{{name: n, mode: 0o755}}, true)
		}},
	}
	for _, k := range kinds {
		for _, n := range []int{maxLen, maxLen + 1} {
			t.Run(fmt.Sprintf("%s name of %d bytes", k.name, n), func(t *testing.T) {
				t.Parallel()
				files, err := archive.List(k.kind, bytes.NewReader(k.make(t, strings.Repeat("a", n))))
				want := error(nil)
				if n > maxLen {
					want = archive.ErrNameTooLong
				}
				if !errors.Is(err, want) || (want == nil && len(files) != 1) {
					t.Errorf("List = %d files, %v; want %v", len(files), err, want)
				}
			})
		}
	}
}

func TestListBoundsNameMemory(t *testing.T) {
	t.Parallel()
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
}

func TestListZipNamesMatchChezmoi(t *testing.T) {
	t.Parallel()
	t.Run("backslashes in zip names are kept, chezmoi matches the raw name", func(t *testing.T) {
		t.Parallel()
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

func TestListZipLimits(t *testing.T) {
	t.Parallel()
	data := zipBytes(t, []entry{{name: "d/tool", mode: 0o755}, {name: "d/other", mode: 0o644}}, true)
	t.Run("a zip over the spool cap is not listed", func(t *testing.T) {
		t.Parallel()
		l := archive.DefaultLimits()
		l.MaxZipSpool = int64(len(data)) - 1
		files, err := archive.ListLimits(archive.Zip, bytes.NewReader(data), l)
		if !errors.Is(err, archive.ErrTooLarge) || files != nil {
			t.Errorf("ListLimits = %v, %v; want nil, %v", files, err, archive.ErrTooLarge)
		}
		// A body far over the cap is read only up to it. The body is finite, so a
		// regression fails here instead of filling the disk.
		l.MaxZipSpool = 1 << 10
		var read int64
		long := readerFunc(func(p []byte) (int, error) {
			n := min(int64(len(p)), 1<<20-read)
			if n == 0 {
				return 0, io.EOF
			}
			read += n
			return int(n), nil
		})
		if _, err := archive.ListLimits(archive.Zip, long, l); !errors.Is(err, archive.ErrTooLarge) || read > l.MaxZipSpool+1 {
			t.Errorf("ListLimits of a long body = %v after reading %d bytes, want %v after at most %d", err, read, archive.ErrTooLarge, l.MaxZipSpool+1)
		}
		l.MaxZipSpool = int64(len(data))
		if files, err := archive.ListLimits(archive.Zip, bytes.NewReader(data), l); err != nil || len(files) != 2 {
			t.Errorf("ListLimits at the cap = %v, %v; want 2 files", files, err)
		}
	})
	t.Run("not a zip", func(t *testing.T) {
		t.Parallel()
		if _, err := archive.List(archive.Zip, bytes.NewReader([]byte("x"))); !errors.Is(err, zip.ErrFormat) {
			t.Errorf("List = %v, want %v", err, zip.ErrFormat)
		}
	})
}

func TestListZipCentralDirectory(t *testing.T) {
	t.Parallel()
	data := zipBytes(t, []entry{{name: "d/tool", mode: 0o755}, {name: "d/other", mode: 0o644}}, true)
	t.Run("a central directory over the cap is refused", func(t *testing.T) {
		t.Parallel()
		l := archive.DefaultLimits()
		l.MaxCentralDir = 10
		if _, err := archive.ListLimits(archive.Zip, bytes.NewReader(data), l); !errors.Is(err, archive.ErrTooLarge) {
			t.Errorf("ListLimits = %v, want %v", err, archive.ErrTooLarge)
		}
	})
	t.Run("a declared central directory size is checked before it is read", func(t *testing.T) {
		t.Parallel()
		forged := bytes.Clone(data)
		eocd := bytes.LastIndex(forged, []byte("PK\x05\x06"))
		binary.LittleEndian.PutUint32(forged[eocd+12:], 1<<30) // the real directory is a few hundred bytes
		// zip.NewReader would reject this as malformed; ErrTooLarge shows the check ran first.
		if _, err := archive.List(archive.Zip, bytes.NewReader(forged)); !errors.Is(err, archive.ErrTooLarge) {
			t.Errorf("List = %v, want %v", err, archive.ErrTooLarge)
		}
	})
	t.Run("a zip64 directory size and entry count are read from the zip64 record", func(t *testing.T) {
		t.Parallel()
		// From 65535 entries archive/zip writes the zip64 records. Other writers also put
		// the 0xFFFFFFFF marker in the 32-bit directory size, which alone is over the cap.
		entries := make([]entry, 1<<16-1)
		entries[0] = entry{name: "tool", mode: 0o755}
		for i := 1; i < len(entries); i++ {
			entries[i] = entry{name: fmt.Sprintf("d%d/", i), mode: 0o755}
		}
		data := zipBytes(t, entries, true)
		eocd := bytes.LastIndex(data, []byte("PK\x05\x06"))
		binary.LittleEndian.PutUint32(data[eocd+12:], math.MaxUint32)
		files, err := archive.List(archive.Zip, bytes.NewReader(data))
		if err != nil || len(files) != 1 {
			t.Errorf("List = %d files, %v; want one file", len(files), err)
		}
		// The same directory declaring 65535 + 2*65536 entries: archive/zip compares the
		// count mod 65536 only and would read it, but a declared count over the cap is refused.
		rec := bytes.LastIndex(data, []byte("PK\x06\x06"))
		binary.LittleEndian.PutUint64(data[rec+32:], 1<<16-1+2<<16)
		if _, err := archive.List(archive.Zip, bytes.NewReader(data)); !errors.Is(err, archive.ErrTooManyEntries) {
			t.Errorf("List with a declared zip64 count over the cap = %v, want %v", err, archive.ErrTooManyEntries)
		}
	})
	t.Run("an understated directory size does not get past the cap", func(t *testing.T) {
		t.Parallel()
		entries := make([]entry, 200)
		for i := range entries {
			entries[i] = entry{name: fmt.Sprintf("d/file-%d", i), mode: 0o755}
		}
		forged := zipBytes(t, entries, true) // a directory of about 10 KiB
		eocd := bytes.LastIndex(forged, []byte("PK\x05\x06"))
		binary.LittleEndian.PutUint32(forged[eocd+12:], 46) // declares one 46-byte header
		l := archive.DefaultLimits()
		l.MaxCentralDir = 1 << 10
		if files, err := archive.ListLimits(archive.Zip, bytes.NewReader(forged), l); !errors.Is(err, archive.ErrTooLarge) {
			t.Errorf("ListLimits = %d files, %v; want %v", len(files), err, archive.ErrTooLarge)
		}
	})
}

func TestListZipFindsTheEndRecordBehindALongComment(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	if _, err := zw.Create("tool"); err != nil {
		t.Fatal(err)
	}
	if err := zw.SetComment(strings.Repeat("c", 4<<10)); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if files, err := archive.List(archive.Zip, bytes.NewReader(buf.Bytes())); err != nil || len(files) != 1 {
		t.Errorf("List = %d files, %v; want one file", len(files), err)
	}
}

// rawZip is prefix, then n empty-name central directory headers, then an end record
// that declares count entries, size bytes and a directory offset of 0.
func rawZip(prefix []byte, n int, count uint16, size uint32) []byte {
	hdr := make([]byte, 46)
	copy(hdr, "PK\x01\x02")
	eocd := make([]byte, 22)
	copy(eocd, "PK\x05\x06")
	binary.LittleEndian.PutUint16(eocd[10:], count)
	binary.LittleEndian.PutUint32(eocd[12:], size)
	return slices.Concat(prefix, bytes.Repeat(hdr, n), eocd)
}

func TestListZipCountsTheHeadersTheReaderReads(t *testing.T) {
	t.Parallel()
	const over = archive.MaxEntries + 1
	// A header at offset 0 whose zip64 extra field is too short for its 0xFFFFFFFF
	// compressed size: this package's walk accepts it, archive/zip's readDirectoryHeader
	// does not.
	short64 := make([]byte, 46, 50)
	copy(short64, "PK\x01\x02")
	binary.LittleEndian.PutUint32(short64[20:], math.MaxUint32)
	binary.LittleEndian.PutUint16(short64[30:], 4)
	short64 = append(short64, 0x01, 0x00, 0x00, 0x00) // zip64 extra field, no data
	tests := []struct {
		name string
		data []byte
	}{
		{name: "an understated entry count", data: rawZip(nil, over, 1, 46)},
		// The directory starts after the prefix, where the declared size puts it
		// (archive/zip's base offset); the count of 0 makes zip.NewReader fail when it
		// reads them, so only the walk can report ErrTooManyEntries.
		{name: "a directory after a self-extractor prefix", data: rawZip([]byte("SFXSTUB!"), over, 0, over*46)},
		{
			name: "a header only this package accepts at the declared offset",
			data: rawZip(slices.Concat(short64, []byte("junk")), over, 0, over*46),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if _, err := archive.List(archive.Zip, bytes.NewReader(tt.data)); !errors.Is(err, archive.ErrTooManyEntries) {
				t.Errorf("List = %v, want %v", err, archive.ErrTooManyEntries)
			}
		})
	}
}

func TestListLimitsAtTheTop(t *testing.T) {
	t.Parallel()
	t.Run("a MaxDecompressed of math.MaxInt64 does not panic", func(t *testing.T) {
		t.Parallel()
		l := archive.DefaultLimits()
		l.MaxDecompressed = math.MaxInt64 // no practical cap
		files, err := archive.ListLimits(archive.TarGz, bytes.NewReader(tarGz(t, []entry{{name: "tool", mode: 0o755}})), l)
		if err != nil || len(files) != 1 {
			t.Errorf("ListLimits = %v, %v; want one file", files, err)
		}
	})
	t.Run("every limit at math.MaxInt64 lists a zip", func(t *testing.T) {
		t.Parallel()
		l := archive.Limits{
			MaxNameLen: math.MaxInt, MaxNameBytes: math.MaxInt64, MaxDecompressed: math.MaxInt64,
			MaxZipSpool: math.MaxInt64, MaxCentralDir: math.MaxInt64,
		}
		files, err := archive.ListLimits(archive.Zip, bytes.NewReader(zipBytes(t, []entry{{name: "tool", mode: 0o755}}, true)), l)
		if err != nil || len(files) != 1 {
			t.Errorf("ListLimits = %v, %v; want one file", files, err)
		}
	})
}

func TestListZeroLimitsMeanDefaults(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		kind archive.Kind
		data []byte
	}{
		"tar.gz": {archive.TarGz, tarGz(t, []entry{{name: "d/tool", mode: 0o755}})},
		"zip":    {archive.Zip, zipBytes(t, []entry{{name: "d/tool", mode: 0o755}}, true)},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			files, err := archive.ListLimits(tc.kind, bytes.NewReader(tc.data), archive.Limits{})
			if err != nil || len(files) != 1 {
				t.Errorf("ListLimits(Limits{}) = %v, %v; want one file", files, err)
			}
		})
	}
}

type readerFunc func([]byte) (int, error)

func (f readerFunc) Read(p []byte) (int, error) { return f(p) }
