package service

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
	"time"
)

const (
	passwordFile = "/bitnami/postgresql/.pgaio_password"
	sessionTTL   = 15 * time.Minute
)

// PasswordAuth authenticates dashboard users and manages their sessions.
type PasswordAuth struct {
	mu       sync.RWMutex
	password string
	sessions map[string]time.Time
}

// NewPasswordAuth loads the configured password or creates and persists one.
func NewPasswordAuth() (*PasswordAuth, error) {
	configuredPassword := strings.TrimSpace(os.Getenv("PGAIO_PASSWORD"))
	password, generated, err := loadPassword(passwordFile, configuredPassword)
	if err != nil {
		return nil, err
	}
	if configuredPassword != "" {
		log.Println("🔐 PGAIO dashboard password loaded from PGAIO_PASSWORD")
	} else if generated {
		log.Printf("🔐 PGAIO dashboard password generated: %s", password)
		log.Println("🔐 Save it now; it is also stored in the persistent PostgreSQL volume")
	} else {
		log.Println("🔐 PGAIO dashboard password loaded from persistent storage")
	}
	return newPasswordAuth(password), nil
}

func loadPassword(path, configuredPassword string) (password string, generated bool, err error) {
	if configuredPassword != "" {
		return configuredPassword, false, nil
	}

	data, err := os.ReadFile(path)
	if err == nil && strings.TrimSpace(string(data)) != "" {
		return strings.TrimSpace(string(data)), false, nil
	}
	if err != nil && !os.IsNotExist(err) {
		return "", false, fmt.Errorf("read dashboard password: %w", err)
	}

	password, err = generatePassword()
	if err != nil {
		return "", false, err
	}
	if err := os.WriteFile(path, []byte(password+"\n"), 0600); err != nil {
		return "", false, fmt.Errorf("persist generated dashboard password: %w", err)
	}
	return password, true, nil
}

func newPasswordAuth(password string) *PasswordAuth {
	return &PasswordAuth{password: password, sessions: make(map[string]time.Time)}
}

func generatePassword() (string, error) {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate dashboard password: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// Login validates the dashboard password and creates a new session.
func (a *PasswordAuth) Login(password string) (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if subtle.ConstantTimeCompare([]byte(password), []byte(a.password)) != 1 {
		return "", fmt.Errorf("invalid password")
	}
	sessionID := a.createSessionLocked()
	log.Println("🔐 Dashboard session created")
	return sessionID, nil
}

// ValidateSession checks a session and refreshes its expiry.
func (a *PasswordAuth) ValidateSession(sessionID string) bool {
	if sessionID == "" {
		return false
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	expiry, ok := a.sessions[sessionID]
	if !ok {
		return false
	}
	if time.Now().After(expiry) {
		delete(a.sessions, sessionID)
		return false
	}
	a.sessions[sessionID] = time.Now().Add(sessionTTL)
	return true
}

// CheckSession checks a session without refreshing its expiry.
func (a *PasswordAuth) CheckSession(sessionID string) bool {
	if sessionID == "" {
		return false
	}
	a.mu.RLock()
	defer a.mu.RUnlock()
	expiry, ok := a.sessions[sessionID]
	return ok && time.Now().Before(expiry)
}

func (a *PasswordAuth) createSessionLocked() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(fmt.Sprintf("generate session ID: %v", err))
	}
	id := hex.EncodeToString(b)
	a.sessions[id] = time.Now().Add(sessionTTL)
	now := time.Now()
	for key, expiry := range a.sessions {
		if now.After(expiry) {
			delete(a.sessions, key)
		}
	}
	return id
}
