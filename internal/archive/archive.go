// Package archive lists the regular files of a downloaded archive without
// extracting anything to disk.
//
// .tar.gz, .tgz and .zip are listed; every other format (.tar.xz, ...) is not, and
// KindOf reports it as such.
package archive

import (
	"archive/tar"
	"archive/zip"
	"bufio"
	"compress/gzip"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
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
// is an error: the caller reports it as unchecked. A zero field means that field's
// default from DefaultLimits, so Limits{} is DefaultLimits(); a limit cannot be
// switched off, but math.MaxInt64 is a cap no archive reaches.
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

// withDefaults replaces every zero field by its default.
func (l Limits) withDefaults() Limits {
	d := DefaultLimits()
	if l.MaxNameLen == 0 {
		l.MaxNameLen = d.MaxNameLen
	}
	if l.MaxNameBytes == 0 {
		l.MaxNameBytes = d.MaxNameBytes
	}
	if l.MaxDecompressed == 0 {
		l.MaxDecompressed = d.MaxDecompressed
	}
	if l.MaxZipSpool == 0 {
		l.MaxZipSpool = d.MaxZipSpool
	}
	if l.MaxCentralDir == 0 {
		l.MaxCentralDir = d.MaxCentralDir
	}
	return l
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
// extracted. Each call spools its own file, so a caller that lists several zips bounds
// their concurrency itself (disk use is up to MaxZipSpool per call). For a tar, r is
// read up to the end of the tar stream only; for a zip too large to list, up to the
// spool limit. After an error the rest of r is unread, so a caller that needs the
// whole body (to checksum it) must drain r itself. Entry names are path.Clean of the
// raw name, the way chezmoi matches them (a zip name with backslashes keeps them).
// DefaultLimits bounds the memory, disk and CPU a hostile archive can use; exceeding
// a limit is an error.
func List(kind Kind, r io.Reader) ([]File, error) {
	return ListLimits(kind, r, DefaultLimits())
}

// ListLimits is List with explicit limits. A zip is spooled to a temporary file of up
// to MaxZipSpool bytes for the length of the call; callers bound how many run at once.
func ListLimits(kind Kind, r io.Reader, l Limits) ([]File, error) {
	l = l.withDefaults()
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
	if int64(len(p))-1 > c.left { // read at most one byte past the cap; c.left+1 could overflow in the other form
		p = p[:c.left+1]
	}
	n, err := c.r.Read(p)
	c.left -= int64(n)
	if c.left < 0 {
		return 0, ErrTooLarge
	}
	return n, err //nolint:wrapcheck // io.EOF must reach the tar reader unwrapped
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
	if err = checkCentralDir(tmp, size, l); err != nil {
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

// Zip record layout (zip 4.3.12 to 4.3.16); archive/zip reads the same fields.
const (
	eocdSig         = "PK\x05\x06"
	eocdLen         = 22
	zip64LocSig     = "PK\x06\x07"
	zip64LocLen     = 20
	zip64EOCDSig    = "PK\x06\x06"
	zip64EOCDLen    = 56
	dirHeaderSig    = "PK\x01\x02"
	dirHeaderLen    = 46
	zipTailShort    = 1024
	zipTailLong     = 65 * 1024
	zip64Marker16   = 0xffff
	zip64Marker32   = 0xffffffff
	dirHeaderNameAt = 28 // name, extra and comment lengths: three uint16 from here
)

// directory is where archive/zip will start reading central directory headers.
type directory struct {
	start   int64  // first header
	records uint64 // entries the end record declares
	size    uint64 // bytes the end record declares
}

// checkCentralDir bounds the central directory before zip.NewReader builds it in
// memory. archive/zip trusts neither the declared entry count (it compares it mod
// 65536) nor the declared size: it reads headers from the directory offset until one is
// malformed. So this finds the offset exactly as archive/zip does, then walks the
// headers itself and counts them. Guaranteed: zip.NewReader runs only when the headers
// it will read, from that offset, number at most MaxEntries and take at most
// MaxCentralDir bytes (header, name, extra and comment), and the declared entry count
// and size are within the same caps. Not guaranteed: the walk stops at the first
// malformed header, as archive/zip does, so entries after it are neither counted nor
// read.
func checkCentralDir(ra io.ReaderAt, size int64, l Limits) error {
	d, err := locateDirectory(ra, size, l)
	if err != nil {
		if errors.Is(err, ErrTooManyEntries) || errors.Is(err, ErrTooLarge) {
			return err
		}
		return fmt.Errorf("read zip: %w", err)
	}
	br := bufio.NewReader(io.NewSectionReader(ra, d.start, size-d.start))
	var entries, total int64
	for {
		var hdr [dirHeaderLen]byte
		if _, err := io.ReadFull(br, hdr[:]); err != nil || string(hdr[:4]) != dirHeaderSig {
			return nil // archive/zip stops here too and reports what is wrong
		}
		n := varLen(hdr[:])
		entries++
		total += dirHeaderLen + n
		if entries > MaxEntries {
			return ErrTooManyEntries
		}
		if total > l.MaxCentralDir {
			return ErrTooLarge
		}
		if skipped, err := br.Discard(int(n)); err != nil || int64(skipped) != n {
			return nil
		}
	}
}

// locateDirectory replicates archive/zip's readDirectoryEnd: the end record in the last
// 1 KiB or else the last 65 KiB, the zip64 record when a field holds a marker, and the
// base offset fallback.
func locateDirectory(ra io.ReaderAt, size int64, l Limits) (directory, error) {
	end, buf, err := findEOCD(ra, size)
	if err != nil {
		return directory{}, err
	}
	d := directory{
		records: uint64(binary.LittleEndian.Uint16(buf[10:])),
		size:    uint64(binary.LittleEndian.Uint32(buf[12:])),
	}
	offset := uint64(binary.LittleEndian.Uint32(buf[16:]))
	if d.records == zip64Marker16 || d.size == zip64Marker32 || offset == zip64Marker32 {
		p, err := zip64End(ra, end)
		if err != nil {
			return directory{}, err
		}
		if p >= 0 {
			end = p
			rec := make([]byte, zip64EOCDLen)
			if _, err := ra.ReadAt(rec, p); err != nil || string(rec[:4]) != zip64EOCDSig {
				return directory{}, zip.ErrFormat
			}
			d.records = binary.LittleEndian.Uint64(rec[32:])
			d.size = binary.LittleEndian.Uint64(rec[40:])
			offset = binary.LittleEndian.Uint64(rec[48:])
		}
	}
	if d.records > MaxEntries {
		return directory{}, ErrTooManyEntries
	}
	if d.size > math.MaxInt64 || int64(d.size) > l.MaxCentralDir {
		return directory{}, ErrTooLarge
	}
	if offset > math.MaxInt64 {
		return directory{}, zip.ErrFormat
	}
	base := end - int64(d.size) - int64(offset)
	if o := base + int64(offset); o < 0 || o >= size {
		return directory{}, zip.ErrFormat
	}
	if base > 0 && headerAt(ra, int64(offset), size) {
		base = 0
	}
	d.start = base + int64(offset)
	return d, nil
}

// findEOCD returns the offset and bytes (from the signature on) of the end record.
func findEOCD(ra io.ReaderAt, size int64) (int64, []byte, error) {
	for i, n := range []int64{zipTailShort, zipTailLong} {
		n = min(n, size)
		buf := make([]byte, n)
		if _, err := ra.ReadAt(buf, size-n); err != nil && !errors.Is(err, io.EOF) {
			return 0, nil, err //nolint:wrapcheck // wrapped by the caller
		}
		if p := lastEOCD(buf); p >= 0 {
			return size - n + int64(p), buf[p:], nil
		}
		if i == 1 || n == size {
			return 0, nil, zip.ErrFormat
		}
	}
	return 0, nil, zip.ErrFormat
}

// lastEOCD is archive/zip's findSignatureInBlock: the last end record whose comment
// fits in b, or -1.
func lastEOCD(b []byte) int {
	for i := len(b) - eocdLen; i >= 0; i-- {
		if string(b[i:i+4]) == eocdSig {
			if int(binary.LittleEndian.Uint16(b[i+eocdLen-2:]))+eocdLen+i > len(b) {
				return -1
			}
			return i
		}
	}
	return -1
}

// zip64End returns the offset of the zip64 end record, or -1 when the end record at
// end has no valid zip64 locator before it.
func zip64End(ra io.ReaderAt, end int64) (int64, error) {
	loc := end - zip64LocLen
	if loc < 0 {
		return -1, nil
	}
	buf := make([]byte, zip64LocLen)
	if _, err := ra.ReadAt(buf, loc); err != nil {
		return -1, err //nolint:wrapcheck // wrapped by the caller
	}
	if string(buf[:4]) != zip64LocSig || binary.LittleEndian.Uint32(buf[4:]) != 0 ||
		binary.LittleEndian.Uint32(buf[16:]) != 1 {
		return -1, nil
	}
	p := binary.LittleEndian.Uint64(buf[8:])
	if p > math.MaxInt64 {
		return -1, zip.ErrFormat
	}
	return int64(p), nil
}

// headerAt reports whether a whole central directory header, name, extra and comment
// included, can be read at off.
func headerAt(ra io.ReaderAt, off, size int64) bool {
	if off >= size {
		return false
	}
	sr := io.NewSectionReader(ra, off, size-off)
	var hdr [dirHeaderLen]byte
	if _, err := io.ReadFull(sr, hdr[:]); err != nil || string(hdr[:4]) != dirHeaderSig {
		return false
	}
	n := varLen(hdr[:])
	_, err := io.CopyN(io.Discard, sr, n)
	return err == nil
}

// varLen is the size of the name, extra field and comment that follow a central
// directory header.
func varLen(hdr []byte) int64 {
	return int64(binary.LittleEndian.Uint16(hdr[dirHeaderNameAt:])) +
		int64(binary.LittleEndian.Uint16(hdr[dirHeaderNameAt+2:])) +
		int64(binary.LittleEndian.Uint16(hdr[dirHeaderNameAt+4:]))
}
