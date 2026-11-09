package auth

import (
	_ "embed" // embeds common-passwords.txt
	"errors"
	"strings"
	"unicode"
	"unicode/utf8"
)

// The lengths of a password and a username, in characters, which the messages that explain the rules
// also quote.
const (
	MinPasswordLength = 12
	MaxPasswordLength = 128
	MaxUsernameLength = 64
)

// The errors of the password, username and email rules. Their messages are the stable codes that the API
// answers with.
var (
	ErrPasswordTooShort  = errors.New("password_too_short")
	ErrPasswordTooLong   = errors.New("password_too_long")
	ErrPasswordTooCommon = errors.New("password_too_common")
	ErrUsernameInvalid   = errors.New("username_invalid")
	ErrEmailInvalid      = errors.New("email_invalid")
)

// IsPasswordPolicyError reports whether err is the error of a rule of the password policy.
func IsPasswordPolicyError(err error) bool {
	return errors.Is(err, ErrPasswordTooShort) || errors.Is(err, ErrPasswordTooLong) || errors.Is(err, ErrPasswordTooCommon)
}

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
	case length < MinPasswordLength:
		return ErrPasswordTooShort
	case length > MaxPasswordLength:
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
// whitespace, control characters or format characters. So a login is never ambiguous between a
// username and an email, and two usernames never look the same because of an invisible character.
// Length counts characters, not bytes.
func ValidateUsername(username string) error {
	if username == "" || utf8.RuneCountInString(username) > MaxUsernameLength || !utf8.ValidString(username) ||
		strings.ContainsFunc(username, forbiddenInUsername) {
		return ErrUsernameInvalid
	}

	return nil
}

func forbiddenInUsername(r rune) bool {
	return r == '@' || unicode.IsSpace(r) || unicode.IsControl(r) || unicode.Is(unicode.Cf, r)
}

// ValidateEmail returns ErrEmailInvalid unless email holds an @, as every email does, so that it can
// never be taken for a username at login.
func ValidateEmail(email string) error {
	if !strings.Contains(email, "@") {
		return ErrEmailInvalid
	}

	return nil
}
