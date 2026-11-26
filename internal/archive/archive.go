// Package archive lists the regular files of a downloaded archive without
// extracting anything to disk.
//
// .tar.gz, .tgz and .zip are listed; every other format (.tar.xz, ...) is not, and
// KindOf reports it as such.
package archive

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"strings"
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
}

// DefaultLimits returns the limits List applies: names up to 4096 bytes and 16 MiB of
// names in all, and 4 GiB of decompressed tar stream.
func DefaultLimits() Limits {
	return Limits{MaxNameLen: 4096, MaxNameBytes: 16 << 20, MaxDecompressed: 4 << 30}
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
	Path string // slash separated, without a leading "./"
	Exec bool   // an executable bit is set, or the archive does not record modes
}

// List returns the regular files of the archive of the given kind read from r, in
// archive order. Directories, symlinks and other special entries are skipped. A zip
// is spooled to a temporary file, which is removed before List returns; it is never
// extracted. For a tar, r is read up to the end of the tar stream only.
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

func clean(name string) string {
	return strings.TrimPrefix(path.Clean(strings.ReplaceAll(name, "\\", "/")), "./")
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

func listZip(r io.Reader, l Limits) (files []File, err error) {
	files = []File{}
	tmp, err := os.CreateTemp("", "dots-zip-*")
	if err != nil {
		return nil, fmt.Errorf("spool zip: %w", err)
	}
	defer func() {
		err = errors.Join(err, tmp.Close(), os.Remove(tmp.Name()))
	}()
	size, err := io.Copy(tmp, r)
	if err != nil {
		return nil, fmt.Errorf("spool zip: %w", err)
	}
	zr, err := zip.NewReader(tmp, size)
	if err != nil {
		return nil, fmt.Errorf("read zip: %w", err)
	}
	if len(zr.File) > MaxEntries {
		return nil, ErrTooManyEntries
	}
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
