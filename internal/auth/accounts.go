// Package auth provides authentication and session management for HFS.
package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"
)

// SessionStore manages user sessions.
type SessionStore struct {
	sessions map[string]*Session
	mu       sync.RWMutex
	timeout  time.Duration
}

// Session represents an authenticated user session.
type Session struct {
	User     string
	Created  time.Time
	LastSeen time.Time
}

// NewSessionStore creates a new session store.
func NewSessionStore(timeout time.Duration) *SessionStore {
	ss := &SessionStore{
		sessions: make(map[string]*Session),
		timeout:  timeout,
	}
	go ss.cleanup()
	return ss
}

const sessionCookie = "HFS_SID"

// CreateSession creates a new session for a user and sets the cookie.
func (ss *SessionStore) CreateSession(w http.ResponseWriter, user string) string {
	sid := generateSID()
	ss.mu.Lock()
	ss.sessions[sid] = &Session{
		User:     user,
		Created:  time.Now(),
		LastSeen: time.Now(),
	}
	ss.mu.Unlock()

	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    sid,
		Path:     "/",
		HttpOnly: true,
		Secure:   false, // set true if using TLS
		SameSite: http.SameSiteLaxMode,
	})
	return sid
}

// GetUser returns the user for a session cookie, or empty string.
func (ss *SessionStore) GetUser(r *http.Request) (string, bool) {
	cookie, err := r.Cookie(sessionCookie)
	if err != nil {
		return "", false
	}
	ss.mu.RLock()
	session, ok := ss.sessions[cookie.Value]
	ss.mu.RUnlock()
	if !ok {
		return "", false
	}
	if time.Since(session.LastSeen) > ss.timeout {
		ss.mu.Lock()
		delete(ss.sessions, cookie.Value)
		ss.mu.Unlock()
		return "", false
	}
	session.LastSeen = time.Now()
	return session.User, true
}

// DestroySession removes a session.
func (ss *SessionStore) DestroySession(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie(sessionCookie)
	if err != nil {
		return
	}
	ss.mu.Lock()
	delete(ss.sessions, cookie.Value)
	ss.mu.Unlock()

	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    "",
		Path:     "/",
		Expires:  time.Unix(0, 0),
		HttpOnly: true,
	})
}

// cleanup periodically removes expired sessions.
func (ss *SessionStore) cleanup() {
	ticker := time.NewTicker(30 * time.Minute)
	for range ticker.C {
		ss.mu.Lock()
		for sid, s := range ss.sessions {
			if time.Since(s.LastSeen) > ss.timeout {
				delete(ss.sessions, sid)
			}
		}
		ss.mu.Unlock()
	}
}

func generateSID() string {
	b := make([]byte, 32)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// HashPassword creates a bcrypt hash of a password.
func HashPassword(password string) (string, error) {
	bytes, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(bytes), nil
}

// CheckPassword compares a password against a bcrypt hash.
func CheckPassword(hash, password string) bool {
	err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
	return err == nil
}

// HMACSignedCookie creates an HMAC-signed cookie value.
func HMACSignedCookie(value, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(value))
	sig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return value + "." + sig
}

// VerifyHMACCookie verifies and extracts the value from an HMAC-signed cookie.
func VerifyHMACCookie(signed, secret string) (string, error) {
	parts := strings.SplitN(signed, ".", 2)
	if len(parts) != 2 {
		return "", fmt.Errorf("invalid cookie format")
	}
	expected := HMACSignedCookie(parts[0], secret)
	if !hmac.Equal([]byte(expected), []byte(signed)) {
		return "", fmt.Errorf("invalid cookie signature")
	}
	return parts[0], nil
}
