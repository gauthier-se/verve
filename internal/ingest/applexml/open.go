package applexml

import (
	"archive/zip"
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"iter"
	"os"
	"strings"

	"github.com/gauthier-se/verve/internal/ingest"
)

// ErrNotAnExport is the error of a file that is neither a zip holding an export.xml nor a bare
// export.xml, which fails its import at once (FR-1 of XML import).
var ErrNotAnExport = errors.New("the file is not an Apple Health export")

// Export is an Apple Health export on disk: an export.zip, read in place through its central
// directory, or a bare export.xml.
type Export struct {
	file *diskFile
	// zip is nil for a bare export.xml.
	zip *zip.Reader
	// xml is the export.xml entry of the zip.
	xml *zip.File
	// dir is the folder of export.xml in the zip, with its trailing slash, empty at the root.
	dir  string
	size int64
}

// zipHeader starts a zip local file header.
var zipHeader = []byte("PK\x03\x04")

// Open opens the export of f, which stays open while the export is read: a zip when it starts
// with a local file header, a bare export.xml when its first byte other than XML white space is <
// (FR-1 of XML import). In a zip, the export is the first entry of the central directory named
// export.xml at the root or one folder deep.
//
// It fails with ErrNotAnExport for any other file, with an error wrapping ErrMalformed for a zip
// whose central directory is corrupted, and with the error of f when f fails.
func Open(ctx context.Context, f *os.File) (*Export, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	info, err := f.Stat()
	if err != nil {
		return nil, fmt.Errorf("open export: %w", err)
	}

	file := &diskFile{f: f}
	size := info.Size()

	head := make([]byte, len(zipHeader))
	if _, err := file.ReadAt(head, 0); err != nil && !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("open export: %w", err)
	}

	if bytes.Equal(head, zipHeader) {
		return openZip(file, size)
	}

	isXML, err := startsWithTag(io.NewSectionReader(file, 0, size))
	if err != nil {
		return nil, fmt.Errorf("open export: %w", err)
	}

	if !isXML {
		return nil, ErrNotAnExport
	}

	return &Export{file: file, size: size}, nil
}

// openZip opens the zip of file, whose size is size, and finds its export.xml.
func openZip(file *diskFile, size int64) (*Export, error) {
	zr, err := zip.NewReader(file, size)
	if err != nil && !errors.Is(err, zip.ErrInsecurePath) {
		return nil, fmt.Errorf("open export: %w", zipError(err))
	}

	for _, entry := range zr.File {
		dir, ok := strings.CutSuffix(entry.Name, "export.xml")
		if ok && (dir == "" || strings.IndexByte(dir, '/') == len(dir)-1) {
			return &Export{
				file: file, zip: zr, xml: entry, dir: dir,
				size: int64(entry.UncompressedSize64), //nolint:gosec // G115: a size above 8 EiB is no export
			}, nil
		}
	}

	return nil, ErrNotAnExport
}

// startsWithTag reports whether the first byte of r other than XML white space is <.
func startsWithTag(r io.Reader) (bool, error) {
	br := bufio.NewReader(r)

	for {
		c, err := br.ReadByte()

		switch {
		case errors.Is(err, io.EOF):
			return false, nil
		case err != nil:
			return false, err
		case c == ' ', c == '\t', c == '\r', c == '\n':
			continue
		default:
			return c == '<', nil
		}
	}
}

// Size returns the uncompressed size of export.xml: that of its central directory entry for a zip,
// whose local headers may defer their sizes to data descriptors, and the size of the file for a
// bare export.xml (FR-7 of XML import).
func (e *Export) Size() int64 {
	return e.size
}

// Records reads the records of the export, as Records reads those of a document. A corrupted entry
// of a zip ends the read with an error wrapping ErrMalformed, and a read error of the file with the
// error of the file.
func (e *Export) Records(ctx context.Context) iter.Seq2[ingest.Record, error] {
	return func(yield func(ingest.Record, error) bool) {
		r, err := e.open()
		if err == nil {
			defer func() { _ = r.Close() }() // a read-only entry or section

			routes := &routes{zip: e.zip, dir: e.dir}
			err = readRecords(ctx, r, routes, func(r ingest.Record) bool { return yield(r, nil) })
		}

		if err != nil {
			yield(ingest.Record{}, err)
		}
	}
}

// open opens export.xml.
func (e *Export) open() (io.ReadCloser, error) {
	if e.zip == nil {
		return io.NopCloser(io.NewSectionReader(e.file, 0, e.size)), nil
	}

	rc, err := e.xml.Open()
	if err != nil {
		return nil, fmt.Errorf("open export.xml: %w", zipError(err))
	}

	return entryReader{rc}, nil
}

// diskFile is the file of an export, whose read errors are diskError, so that an error of a zip
// entry that is not one is the error of a corrupted zip.
type diskFile struct {
	f *os.File
}

func (d *diskFile) ReadAt(p []byte, off int64) (int, error) {
	n, err := d.f.ReadAt(p, off)
	if err != nil && !errors.Is(err, io.EOF) {
		err = diskError{err}
	}

	return n, err
}

// diskError is a read error of the file of an export, which is retried as any job error (FR-5 of
// XML import).
type diskError struct {
	err error
}

func (e diskError) Error() string { return e.err.Error() }

func (e diskError) Unwrap() error { return e.err }

// zipError returns err, an error of a zip entry or of its central directory, as an error of a
// corrupted zip, unless it is a read error of the file.
func zipError(err error) error {
	if errors.Is(err, io.EOF) || errors.As(err, new(diskError)) {
		return err
	}

	return fmt.Errorf("%w: the zip file is corrupted: %w", ErrMalformed, err)
}

// entryReader reads a zip entry, its errors as zipError returns them.
type entryReader struct {
	io.ReadCloser
}

func (r entryReader) Read(p []byte) (int, error) {
	n, err := r.ReadCloser.Read(p)
	if err != nil {
		err = zipError(err)
	}

	return n, err
}
