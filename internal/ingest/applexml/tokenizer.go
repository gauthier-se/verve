package applexml

import (
	"bufio"
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
)

// tokenizer reads the tokens of a document and checks what xml.Decoder.RawToken leaves out: that
// every end tag matches its start tag, and that the document has one root element with nothing but
// space around it.
type tokenizer struct {
	dec *xml.Decoder
	src *sourceReader
	// dtd is where the DTD was in the document, which the decoder never reads.
	dtd gap
	// open are the names of the elements that are open.
	open []xml.Name
	// closed is true once the root element is closed.
	closed bool
	// start is the byte offset in the document of the last token read.
	start int64
}

// gap is a range of bytes of the document that the decoder does not read: size bytes at the offset
// at of what it reads.
type gap struct {
	at, size int64
}

// newTokenizer returns a tokenizer of the document of r, and the error of r when it fails while
// the inline DTD is skipped.
func newTokenizer(r io.Reader) (*tokenizer, error) {
	src := &sourceReader{r: r}

	rest, dtd, err := skipDTD(src)
	if err != nil {
		return nil, fmt.Errorf("read export.xml: %w", err)
	}

	return &tokenizer{dec: xml.NewDecoder(rest), src: src, dtd: dtd}, nil
}

// maxProlog is how far from the start of the document the inline DTD is looked for.
const maxProlog = 64 << 10

// doctype starts the inline DTD, after the less-than sign.
const doctype = "!DOCTYPE"

// skipDTD returns the document of r without its inline DTD, from <!DOCTYPE to the first ]>, so that
// the decoder never parses it, since some iOS versions produce a malformed DTD (FR-5 of XML
// import). The DTD is looked for in the prolog only: before the first less-than sign that starts
// neither a declaration, a comment nor a processing instruction. It returns where the DTD was, and
// the error of r when it fails.
func skipDTD(r io.Reader) (io.Reader, gap, error) {
	br := bufio.NewReader(r)

	var head []byte

	for len(head) < maxProlog {
		chunk, err := br.ReadSlice('<')
		head = append(head, chunk...)

		switch {
		case errors.Is(err, bufio.ErrBufferFull):
			continue
		case errors.Is(err, io.EOF):
			return bytes.NewReader(head), gap{}, nil
		case err != nil:
			return nil, gap{}, err
		}

		next, err := br.Peek(len(doctype))
		if err != nil && !errors.Is(err, io.EOF) {
			return nil, gap{}, err
		}

		if string(next) == doctype {
			size, err := discardDTD(br)
			if err != nil {
				return nil, gap{}, err
			}

			head = head[:len(head)-1]

			return io.MultiReader(bytes.NewReader(head), br), gap{at: int64(len(head)), size: 1 + size}, nil
		}

		if len(next) == 0 || (next[0] != '?' && next[0] != '!') {
			break
		}
	}

	return io.MultiReader(bytes.NewReader(head), br), gap{}, nil
}

// discardDTD discards the bytes of br up to the first ]> included, or to the end of the document,
// and returns how many it discarded.
func discardDTD(br *bufio.Reader) (int64, error) {
	var size int64

	for {
		chunk, err := br.ReadSlice(']')
		size += int64(len(chunk))

		switch {
		case errors.Is(err, bufio.ErrBufferFull):
			continue
		case errors.Is(err, io.EOF):
			return size, nil
		case err != nil:
			return 0, err
		}

		next, err := br.Peek(1)
		if err != nil && !errors.Is(err, io.EOF) {
			return 0, err
		}

		if string(next) == ">" {
			_, _ = br.Discard(1)

			return size + 1, nil
		}
	}
}

// depth returns the number of open elements.
func (t *tokenizer) depth() int {
	return len(t.open)
}

// next returns the next token, and io.EOF once the document ends after its root element.
func (t *tokenizer) next() (xml.Token, error) {
	t.start = t.dec.InputOffset()
	if t.start >= t.dtd.at {
		t.start += t.dtd.size
	}

	tok, err := t.dec.RawToken()

	switch {
	case t.src.err != nil:
		return nil, fmt.Errorf("read export.xml: %w", t.src.err)
	case errors.Is(err, io.EOF) && !t.closed:
		return nil, t.malformed(errors.New("document ends before its root element is closed"))
	case errors.Is(err, io.EOF):
		return nil, io.EOF
	case err != nil:
		return nil, t.malformed(err)
	}

	switch tok := tok.(type) {
	case xml.StartElement:
		if t.closed {
			return nil, t.malformed(errors.New("element after the root element"))
		}

		t.open = append(t.open, tok.Name)
	case xml.EndElement:
		if len(t.open) == 0 || t.open[len(t.open)-1] != tok.Name {
			return nil, t.malformed(errors.New("end tag does not match its start tag"))
		}

		t.open = t.open[:len(t.open)-1]
		t.closed = len(t.open) == 0
	case xml.CharData:
		if len(t.open) == 0 && !isSpace(tok) {
			return nil, t.malformed(errors.New("text outside the root element"))
		}
	}

	return tok, nil
}

// skip reads the tokens up to the end tag of the element whose start tag was read last.
func (t *tokenizer) skip() error {
	depth := t.depth()

	for t.depth() >= depth {
		if _, err := t.next(); err != nil {
			return err
		}
	}

	return nil
}

// malformed returns err as an error of a malformed document, at the offset of the last token.
func (t *tokenizer) malformed(err error) error {
	return fmt.Errorf("%w at byte %d: %w", ErrMalformed, t.start, err)
}

// isSpace reports whether data holds only XML white space.
func isSpace(data []byte) bool {
	for _, c := range data {
		switch c {
		case ' ', '\t', '\r', '\n':
		default:
			return false
		}
	}

	return true
}

// sourceReader reads the document, and keeps the first error of its reader other than io.EOF, so
// that a read error is never taken for a malformed document.
type sourceReader struct {
	r   io.Reader
	err error
}

func (s *sourceReader) Read(p []byte) (int, error) {
	n, err := s.r.Read(p)
	if err != nil && !errors.Is(err, io.EOF) && s.err == nil {
		s.err = err
	}

	return n, err
}
