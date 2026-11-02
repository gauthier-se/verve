package auth_test

import (
	"os"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/require"

	"github.com/gauthier-se/verve/internal/auth"
)

const strongPassword = "kettles drift over violet rooftops"

// passwordOf returns a password that is not on the list, of exactly n characters.
func passwordOf(n int) string {
	return string([]rune(strings.Repeat(strongPassword+" ", n))[:n])
}

// Given a password, then the policy returns the code of the first rule it breaks. Length counts
// characters, not bytes, and there are no composition rules.
func TestPasswordPolicy_Check(t *testing.T) {
	t.Parallel()

	policy := auth.NewPasswordPolicy()

	tests := []struct {
		name     string
		password string
		want     error
	}{
		{"Empty", "", auth.ErrPasswordTooShort},
		{"ElevenCharacters", passwordOf(11), auth.ErrPasswordTooShort},
		{"TwelveCharacters", passwordOf(12), nil},
		{"ElevenCharactersOfTwoBytes", strings.Repeat("é", 11), auth.ErrPasswordTooShort},
		{"TwelveCharactersOfTwoBytes", strings.Repeat("é", 12), nil},
		{"TwelveCharactersOfFourBytes", strings.Repeat("🔒", 12), nil},
		{"HundredAndTwentyEightCharacters", passwordOf(128), nil},
		{"HundredAndTwentyNineCharacters", passwordOf(129), auth.ErrPasswordTooLong},
		{"HundredAndTwentyEightCharactersOfTwoBytes", strings.Repeat("é", 128), nil},
		{"HundredAndTwentyNineCharactersOfTwoBytes", strings.Repeat("é", 129), auth.ErrPasswordTooLong},
		{"CommonInLowerCase", "password1234", auth.ErrPasswordTooCommon},
		{"CommonInUpperCase", "PASSWORD1234", auth.ErrPasswordTooCommon},
		{"CommonInMixedCase", "PaSsWoRd1234", auth.ErrPasswordTooCommon},
		{"LongWithoutAnyDigitOrSymbol", strongPassword, nil},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			err := policy.Check(tc.password)
			if tc.want == nil {
				require.NoError(t, err)

				return
			}

			require.ErrorIs(t, err, tc.want)
		})
	}
}

// Given the rule errors, then their messages are the stable codes that the API answers with.
func TestPolicyErrors_AreStableCodes(t *testing.T) {
	t.Parallel()

	require.EqualError(t, auth.ErrPasswordTooShort, "password_too_short")
	require.EqualError(t, auth.ErrPasswordTooLong, "password_too_long")
	require.EqualError(t, auth.ErrPasswordTooCommon, "password_too_common")
	require.EqualError(t, auth.ErrUsernameInvalid, "username_invalid")
}

// commonPasswords returns the header and the entries of the embedded list.
func commonPasswords(t *testing.T) (string, []string) {
	t.Helper()

	data, err := os.ReadFile("common-passwords.txt")
	require.NoError(t, err)

	header, list, found := strings.Cut(string(data), "\n\n")
	require.True(t, found, "a blank line ends the header")
	require.True(t, strings.HasSuffix(list, "\n"), "every entry ends with a newline")

	return header, strings.Split(strings.TrimSuffix(list, "\n"), "\n")
}

// Given the embedded list, then it records its source and license, and its entries are what the
// policy can reach: lower case, of an accepted length, and sorted by byte value without duplicates,
// which its binary search relies on.
func TestCommonPasswords_ListIsSortedAndRecordsItsSource(t *testing.T) {
	t.Parallel()

	header, entries := commonPasswords(t)

	require.Contains(t, header, "https://github.com/danielmiessler/SecLists")
	require.Contains(t, header, "MIT License")
	require.Contains(t, header, "Copyright (c) 2018 Daniel Miessler")
	require.NotEmpty(t, entries)

	for i, entry := range entries {
		if length := utf8.RuneCountInString(entry); length < 12 || length > 128 {
			t.Fatalf("entry %d has %d characters", i, length)
		}

		if strings.ToLower(entry) != entry {
			t.Fatalf("entry %d is not in lower case", i)
		}

		if i > 0 && entries[i-1] >= entry {
			t.Fatalf("entry %d is not after the previous one", i)
		}
	}
}

// Given entries spread over the list, including its first and last ones, then the policy finds them
// and passwords right next to them stay accepted.
func TestPasswordPolicy_SearchesTheWholeList(t *testing.T) {
	t.Parallel()

	const step = 1009

	policy := auth.NewPasswordPolicy()
	_, entries := commonPasswords(t)

	sample := []string{entries[0], entries[1], entries[len(entries)-2], entries[len(entries)-1]}
	for i := step; i < len(entries); i += step {
		sample = append(sample, entries[i])
	}

	for _, entry := range sample {
		require.ErrorIs(t, policy.Check(entry), auth.ErrPasswordTooCommon, entry)

		// The list holds no control character, so these sort right around entry and are not on it.
		require.NoError(t, policy.Check("\x01"+entry), entry)
		require.NoError(t, policy.Check(entry+"\x01"), entry)
	}

	require.NoError(t, policy.Check("\U0010FFFF"+entries[len(entries)-1]))
}

// Given a username, then it is accepted, or rejected when it holds what could make a login
// ambiguous. Length counts characters, not bytes.
func TestValidateUsername(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		username string
		valid    bool
	}{
		{"Plain", "alice", true},
		{"SingleCharacter", "a", true},
		{"Accented", "Élodie", true},
		{"Punctuation", "alice.martin-2_x", true},
		{"SixtyFourCharacters", strings.Repeat("a", 64), true},
		{"SixtyFourCharactersOfTwoBytes", strings.Repeat("é", 64), true},
		{"Empty", "", false},
		{"SixtyFiveCharacters", strings.Repeat("a", 65), false},
		{"SixtyFiveCharactersOfTwoBytes", strings.Repeat("é", 65), false},
		{"AtSign", "alice@example.com", false},
		{"LeadingAtSign", "@alice", false},
		{"Space", "alice martin", false},
		{"LeadingSpace", " alice", false},
		{"TrailingSpace", "alice ", false},
		{"Tab", "alice\tmartin", false},
		{"Newline", "alice\n", false},
		{"NoBreakSpace", "alice martin", false},
		{"EmSpace", "alice martin", false},
		{"IdeographicSpace", "alice　martin", false},
		{"Null", "alice\x00", false},
		{"Delete", "alice\x7f", false},
		{"ControlOfTheLatin1Block", "alice\u0085", false},
		{"InvalidUTF8", "alice\xff", false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			err := auth.ValidateUsername(tc.username)
			if tc.valid {
				require.NoError(t, err)

				return
			}

			require.ErrorIs(t, err, auth.ErrUsernameInvalid)
		})
	}
}
