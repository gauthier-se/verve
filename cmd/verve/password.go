package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"golang.org/x/term"
)

// minPasswordLength is the floor for a local-auth password. It is a light guard
// against trivially weak passwords, not a policy engine.
const minPasswordLength = 8

// readNewPassword obtains a new password for a create/passwd command. With
// fromStdin it reads a single line from app.stdin (the docker-login pattern:
// scriptable, keeps the secret out of argv). Otherwise it prompts interactively,
// hiding input on a terminal and asking for a confirmation.
func (app *application) readNewPassword(fromStdin bool) (string, error) {
	sr := &secretReader{in: app.stdin, out: app.stdout}

	if fromStdin {
		pw, err := sr.line()
		if err != nil {
			return "", err
		}
		return validatePassword(pw)
	}

	first, err := sr.prompt("New password: ")
	if err != nil {
		return "", err
	}
	if _, err := validatePassword(first); err != nil {
		return "", err
	}
	second, err := sr.prompt("Confirm password: ")
	if err != nil {
		return "", err
	}
	if first != second {
		return "", errors.New("passwords do not match")
	}
	return first, nil
}

// validatePassword enforces the minimum length.
func validatePassword(pw string) (string, error) {
	if len(pw) < minPasswordLength {
		return "", fmt.Errorf("password must be at least %d characters", minPasswordLength)
	}
	return pw, nil
}

// secretReader reads passwords from a single input, hiding them when the input is
// a terminal. For a non-terminal input (a pipe) it shares one buffered reader
// across reads, so a prompt+confirm pair doesn't lose the second line to
// buffer read-ahead.
type secretReader struct {
	in  *os.File
	out io.Writer
	buf *bufio.Reader
}

// prompt writes label to out, then reads a secret — without echo on a terminal,
// or as a plain line from a pipe.
func (s *secretReader) prompt(label string) (string, error) {
	fmt.Fprint(s.out, label)
	if term.IsTerminal(int(s.in.Fd())) {
		b, err := term.ReadPassword(int(s.in.Fd()))
		fmt.Fprintln(s.out)
		if err != nil {
			return "", err
		}
		return string(b), nil
	}
	return s.line()
}

// line reads one newline-terminated line, trimming the line ending.
func (s *secretReader) line() (string, error) {
	if s.buf == nil {
		s.buf = bufio.NewReader(s.in)
	}
	line, err := s.buf.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	return strings.TrimRight(line, "\r\n"), nil
}
