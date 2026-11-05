package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/gauthier-se/verve/internal/clock"
	"github.com/gauthier-se/verve/internal/store"
)

const (
	// sessionIDLength is the length of a session identifier, in bytes: 256 bits.
	sessionIDLength = 32
	// publicIDLength is the length of the public id of a session, in bytes.
	publicIDLength = 16

	// The instance settings that replace the lifetimes of a session, in days: how long it stays
	// valid without activity, and how long at most after its creation.
	settingIdleDays = "session.idle_days"
	settingMaxDays  = "session.max_days"

	// The lifetimes of a session without the instance settings, in days.
	defaultIdleDays = 30
	defaultMaxDays  = 180

	// maxLifetimeDays is the longest lifetime that an instance setting may set, 100 years, so that an
	// expiry cannot overflow.
	maxLifetimeDays = 36_500

	// renewInterval is the shortest time between two writes of the last seen time of a session, so
	// that requests do not each write.
	renewInterval = time.Minute
)

// NewSession describes the session that a login creates.
type NewSession struct {
	UserID int64
	Method store.LoginMethod
	// ClientIP and UserAgent describe the client that logged in, or are empty when unknown.
	ClientIP  string
	UserAgent string
}

// Sessions creates, looks up and revokes the server-side sessions (ADR 0005).
type Sessions struct {
	clk clock.Clock
}

// NewSessions returns the sessions service, which takes the current time from clk.
func NewSessions(clk clock.Clock) Sessions {
	return Sessions{clk: clk}
}

// Create stores a new session through repos, and returns its identifier and the session. The
// identifier is returned once: the store only holds its hash.
func (s Sessions) Create(ctx context.Context, repos store.Repositories, session NewSession) (string, store.Session, error) {
	lifetimes, err := lifetimesOf(ctx, repos.InstanceSettings())
	if err != nil {
		return "", store.Session{}, err
	}

	id := randomText(sessionIDLength)
	now := s.clk.Now()

	created, err := repos.Sessions().Create(ctx, store.NewSession{
		IDHash:    hashSessionID(id),
		PublicID:  randomText(publicIDLength),
		UserID:    session.UserID,
		Method:    session.Method,
		ExpiresAt: lifetimes.expiry(now, now),
		ClientIP:  session.ClientIP,
		UserAgent: session.UserAgent,
	})
	if err != nil {
		return "", store.Session{}, err
	}

	return id, created, nil
}

// ValidSession is a session that Lookup found valid, and its user.
type ValidSession struct {
	Session store.Session
	User    store.User
	// Renewed reports whether the lookup renewed the session, which moved its expiry: the cookie
	// that holds the identifier must follow it.
	Renewed bool
}

// Lookup returns the session of the identifier id and its user, read on the reader of the store. It
// returns store.ErrNotFound alone for an unknown, expired or revoked session, and for a user who is
// not active, so that callers cannot tell them apart.
//
// A lookup renews the session at most once per minute, and returns it renewed: it writes the last
// seen time and an expiry from the current instance settings. A renewal fails the lookup when the
// session ended under those settings, after they were lowered.
func (s Sessions) Lookup(ctx context.Context, repos store.Repositories, id string) (ValidSession, error) {
	idHash := hashSessionID(id)

	session, user, err := repos.Sessions().Get(ctx, idHash)
	if errors.Is(err, store.ErrNotFound) {
		return ValidSession{}, store.ErrNotFound
	}

	if err != nil {
		return ValidSession{}, err
	}

	now := s.clk.Now()
	if now.After(session.ExpiresAt) || user.Status != store.UserActive {
		return ValidSession{}, store.ErrNotFound
	}

	if now.Sub(session.LastSeenAt) < renewInterval {
		return ValidSession{Session: session, User: user}, nil
	}

	lifetimes, err := lifetimesOf(ctx, repos.InstanceSettings())
	if err != nil {
		return ValidSession{}, err
	}

	if lifetimes.ended(session, now) {
		return ValidSession{}, store.ErrNotFound
	}

	// The session may have been revoked since it was read.
	renewed, err := repos.Sessions().Touch(ctx, idHash, lifetimes.expiry(now, session.CreatedAt))
	if errors.Is(err, store.ErrNotFound) {
		return ValidSession{}, store.ErrNotFound
	}

	if err != nil {
		return ValidSession{}, err
	}

	return ValidSession{Session: renewed, User: user, Renewed: true}, nil
}

// Revoke deletes the session of the identifier id, or returns store.ErrNotFound. The next lookup of
// the session fails.
func (s Sessions) Revoke(ctx context.Context, repos store.Repositories, id string) error {
	return repos.Sessions().Delete(ctx, hashSessionID(id))
}

// lifetimes are how long a session stays valid without activity, and at most after its creation.
type lifetimes struct {
	idle    time.Duration
	maximum time.Duration
}

// lifetimesOf returns the lifetimes of the instance settings, each replaced by its default when it
// is missing or invalid.
func lifetimesOf(ctx context.Context, settings store.InstanceSettingsRepository) (lifetimes, error) {
	idle, err := settingLifetime(ctx, settings, settingIdleDays, defaultIdleDays)
	if err != nil {
		return lifetimes{}, err
	}

	maximum, err := settingLifetime(ctx, settings, settingMaxDays, defaultMaxDays)
	if err != nil {
		return lifetimes{}, err
	}

	return lifetimes{idle: idle, maximum: maximum}, nil
}

// settingLifetime returns the lifetime of the instance setting key, a number of days, or fallback
// days when the setting is missing or is not a whole number from 1 to maxLifetimeDays. An invalid
// value must not lock every user out.
func settingLifetime(ctx context.Context, settings store.InstanceSettingsRepository, key string, fallback int) (time.Duration, error) {
	days := fallback

	setting, err := settings.Get(ctx, key)

	switch {
	case errors.Is(err, store.ErrNotFound):
	case err != nil:
		return 0, fmt.Errorf("read session lifetime: %w", err)
	default:
		if n, err := strconv.Atoi(setting.Value); err == nil && n >= 1 && n <= maxLifetimeDays {
			days = n
		}
	}

	return time.Duration(days) * 24 * time.Hour, nil
}

// ended reports whether session has ended at now under these lifetimes: it was idle for longer than
// the idle lifetime, or was created longer ago than the maximum lifetime.
func (l lifetimes) ended(session store.Session, now time.Time) bool {
	return now.After(session.LastSeenAt.Add(l.idle)) || now.After(session.CreatedAt.Add(l.maximum))
}

// expiry returns the expiry of a session created at createdAt and active at now: the idle lifetime
// after now, but never more than the maximum lifetime after createdAt.
func (l lifetimes) expiry(now, createdAt time.Time) time.Time {
	expiresAt := now.Add(l.idle)
	if maximum := createdAt.Add(l.maximum); expiresAt.After(maximum) {
		return maximum
	}

	return expiresAt
}

// randomText returns length random bytes, encoded in unpadded base64url so that they fit in a
// cookie.
func randomText(length int) string {
	b := make([]byte, length)
	// crypto/rand.Read never returns an error.
	_, _ = rand.Read(b)

	return base64.RawURLEncoding.EncodeToString(b)
}

// hashSessionID returns the SHA-256 hash of the session identifier id, as the client sends it.
func hashSessionID(id string) []byte {
	hash := sha256.Sum256([]byte(id))

	return hash[:]
}
