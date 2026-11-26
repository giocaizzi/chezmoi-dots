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
	switch kind {
	case TarGz:
		return listTarGz(r)
	case Zip:
		return listZip(r)
	case None:
	}
	return nil, errors.New("archive format is not listable")
}

func clean(name string) string {
	return strings.TrimPrefix(path.Clean(strings.ReplaceAll(name, "\\", "/")), "./")
}

func listTarGz(r io.Reader) ([]File, error) {
	zr, err := gzip.NewReader(r)
	if err != nil {
		return nil, fmt.Errorf("read gzip: %w", err)
	}
	tr := tar.NewReader(zr)
	files := []File{}
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
		files = append(files, File{Path: clean(hdr.Name), Exec: hdr.FileInfo().Mode()&0o111 != 0})
	}
}

const creatorUnix = 3

func listZip(r io.Reader) (files []File, err error) {
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
	for _, f := range zr.File {
		mode := f.Mode()
		if !mode.IsRegular() {
			continue
		}
		exec := true // not a unix zip: modes are unknown
		if f.CreatorVersion>>8 == creatorUnix {
			exec = mode&0o111 != 0
		}
		files = append(files, File{Path: clean(f.Name), Exec: exec})
	}
	return files, nil
}
