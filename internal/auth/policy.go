package auth

import (
	_ "embed" // embeds common-passwords.txt
	"errors"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	minPasswordLength = 12
	maxPasswordLength = 128
	maxUsernameLength = 64
)

// The errors of the password and username rules. Their messages are the stable codes that the API
// answers with.
var (
	ErrPasswordTooShort  = errors.New("password_too_short")
	ErrPasswordTooLong   = errors.New("password_too_long")
	ErrPasswordTooCommon = errors.New("password_too_common")
	ErrUsernameInvalid   = errors.New("username_invalid")
)

//go:embed common-passwords.txt
var commonPasswordsFile string

// PasswordPolicy holds the rules of a new password: its length, and not being a common password.
// There are no composition rules.
type PasswordPolicy struct {
	// common holds the sorted common passwords, each followed by a newline.
	common string
}

// NewPasswordPolicy returns the policy of the embedded list.
func NewPasswordPolicy() PasswordPolicy {
	_, common, _ := strings.Cut(commonPasswordsFile, "\n\n")

	return PasswordPolicy{common: common}
}

// Check returns the error of the first rule that password breaks, or nil. Length counts characters,
// not bytes.
func (p PasswordPolicy) Check(password string) error {
	switch length := utf8.RuneCountInString(password); {
	case length < minPasswordLength:
		return ErrPasswordTooShort
	case length > maxPasswordLength:
		return ErrPasswordTooLong
	case listed(p.common, strings.ToLower(password)):
		return ErrPasswordTooCommon
	default:
		return nil
	}
}

// listed reports whether entry is a line of list, whose lines are sorted and each end with a
// newline. The binary search runs on the text itself, because a set of the 700 000 entries would
// cost the memory that a Raspberry Pi needs elsewhere.
func listed(list, entry string) bool {
	low, high := 0, len(list)

	for low < high {
		mid := (low + high) / 2
		start := low + strings.LastIndexByte(list[low:mid], '\n') + 1
		end := mid + strings.IndexByte(list[mid:high], '\n')

		switch line := list[start:end]; {
		case line == entry:
			return true
		case line < entry:
			low = end + 1
		default:
			high = start
		}
	}

	return false
}

// ValidateUsername returns ErrUsernameInvalid unless username has 1 to 64 characters, without @,
// whitespace or control characters, so that a login is never ambiguous between a username and an
// email. Length counts characters, not bytes.
func ValidateUsername(username string) error {
	if username == "" || utf8.RuneCountInString(username) > maxUsernameLength || !utf8.ValidString(username) ||
		strings.ContainsFunc(username, forbiddenInUsername) {
		return ErrUsernameInvalid
	}

	return nil
}

func forbiddenInUsername(r rune) bool {
	return r == '@' || unicode.IsSpace(r) || unicode.IsControl(r)
}
