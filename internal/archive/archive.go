// Package archive lists the regular files of a downloaded archive without
// extracting anything to disk.
//
// .tar.gz, .tgz and .zip are listed; every other format (.tar.xz, ...) is not, and
// KindOf reports it as such.
package archive

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path"
	"strings"
	"sync"
)

// MaxEntries is the most entries List reads before it gives up.
const MaxEntries = 100000

// ErrTooManyEntries is returned by List when the archive holds more than MaxEntries entries.
var ErrTooManyEntries = errors.New("archive has too many entries")

// Errors List returns when an archive exceeds a limit.
var (
	ErrNameTooLong   = errors.New("archive entry name is too long")
	ErrNamesTooLarge = errors.New("archive entry names are too large")
	ErrTooLarge      = errors.New("archive is too large to list")
)

// Limits bounds what List reads and keeps from one archive. An archive over a limit
// is an error: the caller reports it as unchecked.
type Limits struct {
	MaxNameLen   int   // longest entry name, in bytes
	MaxNameBytes int64 // total bytes of the names kept

	MaxDecompressed int64 // bytes of the decompressed tar stream List reads

	MaxZipSpool   int64 // bytes of a zip List spools to disk; a larger zip is not listed
	MaxCentralDir int64 // bytes of a zip's central directory; a larger one is not read
}

// DefaultLimits returns the limits List applies: names up to 4096 bytes and 16 MiB of
// names in all, 4 GiB of decompressed tar stream, and for a zip 256 MiB spooled and a
// 64 MiB central directory.
func DefaultLimits() Limits {
	return Limits{
		MaxNameLen: 4096, MaxNameBytes: 16 << 20,
		MaxDecompressed: 4 << 30,
		MaxZipSpool:     256 << 20, MaxCentralDir: 64 << 20,
	}
}

// names checks the names of the entries List keeps against the limits.
type names struct {
	l     Limits
	bytes int64
}

func (n *names) add(name string) error {
	if len(name) > n.l.MaxNameLen {
		return ErrNameTooLong
	}
	n.bytes += int64(len(name))
	if n.bytes > n.l.MaxNameBytes {
		return ErrNamesTooLarge
	}
	return nil
}

// Kind is a listable archive format.
type Kind int

// The archive formats List reads.
const (
	None Kind = iota // not listable
	TarGz
	Zip
)

// KindOf reports the listable format of an asset name or URL, by its extension.
func KindOf(name string) Kind {
	l := strings.ToLower(name)
	switch {
	case strings.HasSuffix(l, ".tar.gz"), strings.HasSuffix(l, ".tgz"):
		return TarGz
	case strings.HasSuffix(l, ".zip"):
		return Zip
	}
	return None
}

// File is a regular file of an archive.
type File struct {
	Path string // path.Clean of the raw entry name, as chezmoi matches it
	Exec bool   // an executable bit is set, or the archive does not record modes
}

// List returns the regular files of the archive of the given kind read from r, in
// archive order. Directories, symlinks and other special entries are skipped. A zip
// is spooled to a temporary file, which is removed before List returns; it is never
// extracted, and only one zip is spooled at a time across callers. For a tar, r is
// read up to the end of the tar stream only; for a zip too large to list, up to the
// spool limit. After an error the rest of r is unread, so a caller that needs the
// whole body (to checksum it) must drain r itself. Entry names are path.Clean of the
// raw name, the way chezmoi matches them (a zip name with backslashes keeps them).
// DefaultLimits bounds the memory, disk and CPU a hostile archive can use; exceeding
// a limit is an error.
func List(kind Kind, r io.Reader) ([]File, error) {
	return ListLimits(kind, r, DefaultLimits())
}

// ListLimits is List with explicit limits.
func ListLimits(kind Kind, r io.Reader, l Limits) ([]File, error) {
	switch kind {
	case TarGz:
		return listTarGz(r, l)
	case Zip:
		return listZip(r, l)
	case None:
	}
	return nil, errors.New("archive format is not listable")
}

// clean normalizes a name the way chezmoi's archive-file walk does: path.Clean of the
// raw name. A backslash in a zip name stays a backslash.
func clean(name string) string {
	return path.Clean(name)
}

// capReader fails with ErrTooLarge once more than left bytes have been read.
type capReader struct {
	r    io.Reader
	left int64
}

func (c *capReader) Read(p []byte) (int, error) {
	if c.left < 0 {
		return 0, ErrTooLarge
	}
	if int64(len(p)) > c.left+1 {
		p = p[:c.left+1]
	}
	n, err := c.r.Read(p)
	c.left -= int64(n)
	if c.left < 0 {
		return 0, ErrTooLarge
	}
	return n, err
}

func listTarGz(r io.Reader, l Limits) ([]File, error) {
	zr, err := gzip.NewReader(r)
	if err != nil {
		return nil, fmt.Errorf("read gzip: %w", err)
	}
	tr := tar.NewReader(&capReader{r: zr, left: l.MaxDecompressed})
	files := []File{}
	kept := names{l: l}
	for entries := 0; ; entries++ {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return files, nil
		}
		if err != nil {
			return nil, fmt.Errorf("read tar: %w", err)
		}
		if entries >= MaxEntries {
			return nil, ErrTooManyEntries
		}
		if !hdr.FileInfo().Mode().IsRegular() || hdr.Typeflag == tar.TypeLink {
			continue
		}
		if err := kept.add(hdr.Name); err != nil {
			return nil, err
		}
		files = append(files, File{Path: clean(hdr.Name), Exec: hdr.FileInfo().Mode()&0o111 != 0})
	}
}

const creatorUnix = 3

// zipSpool admits one zip spool at a time, so the temp disk List uses is bounded by
// one spool however many downloads run at once.
var zipSpool sync.Mutex //nolint:gochecknoglobals // a process-wide lock is the point

func listZip(r io.Reader, l Limits) (files []File, err error) {
	zipSpool.Lock()
	defer zipSpool.Unlock()
	tmp, err := os.CreateTemp("", "dots-zip-*")
	if err != nil {
		return nil, fmt.Errorf("spool zip: %w", err)
	}
	defer func() {
		err = errors.Join(err, tmp.Close(), os.Remove(tmp.Name()))
	}()
	size, err := io.Copy(tmp, io.LimitReader(r, l.MaxZipSpool+1))
	if err != nil {
		return nil, fmt.Errorf("spool zip: %w", err)
	}
	if size > l.MaxZipSpool {
		return nil, ErrTooLarge
	}
	if err := checkCentralDir(tmp, size, l); err != nil {
		return nil, err
	}
	zr, err := zip.NewReader(tmp, size)
	if err != nil {
		return nil, fmt.Errorf("read zip: %w", err)
	}
	if len(zr.File) > MaxEntries {
		return nil, ErrTooManyEntries
	}
	files = []File{}
	kept := names{l: l}
	for _, f := range zr.File {
		mode := f.Mode()
		if !mode.IsRegular() {
			continue
		}
		exec := true // not a unix zip: modes are unknown
		if f.CreatorVersion>>8 == creatorUnix {
			exec = mode&0o111 != 0
		}
		if err := kept.add(f.Name); err != nil {
			return nil, err
		}
		files = append(files, File{Path: clean(f.Name), Exec: exec})
	}
	return files, nil
}

// End-of-central-directory record layout (zip 4.3.14 to 4.3.16).
const (
	eocdSig        = "PK\x05\x06"
	eocdLen        = 22
	eocdMaxComment = 1<<16 - 1
	zip64LocSig    = "PK\x06\x07"
	zip64LocLen    = 20
	zip64EOCDSig   = "PK\x06\x06"
	zip64EOCDLen   = 56
)

// checkCentralDir reads the end-of-central-directory record, and the zip64 one when
// the first holds the zip64 markers, and refuses a zip whose central directory has
// too many entries or is too large, before zip.NewReader builds it in memory.
func checkCentralDir(ra io.ReaderAt, size int64, l Limits) error {
	tail := make([]byte, min(size, eocdLen+eocdMaxComment))
	if _, err := ra.ReadAt(tail, size-int64(len(tail))); err != nil {
		return fmt.Errorf("read zip: %w", err)
	}
	i := bytes.LastIndex(tail, []byte(eocdSig))
	if i < 0 || len(tail)-i < eocdLen {
		return fmt.Errorf("read zip: %w", zip.ErrFormat)
	}
	entries := uint64(binary.LittleEndian.Uint16(tail[i+10:]))
	dirSize := uint64(binary.LittleEndian.Uint32(tail[i+12:]))
	if entries == math.MaxUint16 || dirSize == math.MaxUint32 {
		loc := i - zip64LocLen
		if loc < 0 || string(tail[loc:loc+4]) != zip64LocSig {
			return fmt.Errorf("read zip: %w", zip.ErrFormat)
		}
		off := binary.LittleEndian.Uint64(tail[loc+8:])
		if off > uint64(size-zip64EOCDLen) {
			return fmt.Errorf("read zip: %w", zip.ErrFormat)
		}
		rec := make([]byte, zip64EOCDLen)
		if _, err := ra.ReadAt(rec, int64(off)); err != nil || string(rec[:4]) != zip64EOCDSig {
			return fmt.Errorf("read zip: %w", zip.ErrFormat)
		}
		entries = binary.LittleEndian.Uint64(rec[32:])
		dirSize = binary.LittleEndian.Uint64(rec[40:])
	}
	if entries > MaxEntries {
		return ErrTooManyEntries
	}
	if dirSize > uint64(l.MaxCentralDir) {
		return ErrTooLarge
	}
	return nil
}
