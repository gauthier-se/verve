package httpapi

import (
	"errors"
	"io"
	"net/http"
)

// errPastLength is the error of a body that goes past the length of its upload.
var errPastLength = errors.New("the body goes past Upload-Length")

// uploadBody reads the body of a PATCH up to the bytes left of its upload. It records whether the
// body goes past them, failing once it read them, and the error of the body, such as a cut
// connection.
type uploadBody struct {
	r    io.Reader
	left int64
	// pastLength reports whether the body holds more than left bytes.
	pastLength bool
	// err is the error of r, other than io.EOF.
	err error
}

func (b *uploadBody) Read(p []byte) (int, error) {
	if b.left == 0 {
		// One more byte tells a body past the length from its end.
		var extra [1]byte

		n, err := io.ReadFull(b.r, extra[:])
		if n > 0 {
			b.pastLength = true

			return 0, errPastLength
		}

		return 0, b.fail(err)
	}

	if int64(len(p)) > b.left {
		p = p[:b.left]
	}

	n, err := b.r.Read(p)
	b.left -= int64(n)

	return n, b.fail(err)
}

// fail records err unless it is io.EOF, and returns it.
func (b *uploadBody) fail(err error) error {
	if err != nil && !errors.Is(err, io.EOF) {
		b.err = err
	}

	return err
}

// bodyPastLength returns the 400 problem of a body that goes past the length of its upload.
func bodyPastLength() *Problem {
	return tusProblem(http.StatusBadRequest, "the body goes past Upload-Length")
}
