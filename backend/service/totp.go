package service

import (
	"crypto/rand"
	"encoding/base32"
	"encoding/hex"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/pquerna/otp/totp"
)

const (
	totpIssuer      = "PGAIO"
	totpAccountName = "admin@pgaio"
	totpSecretFile  = "/bitnami/postgresql/.pgaio_totp_secret"
	sessionTTL      = 15 * time.Minute
)

// TOTP manages time-based one-time password authentication with session support.
type TOTP struct {
	mu            sync.RWMutex
	secret        string // persisted secret (empty = not yet set up)
	pendingSecret string // temporary secret during setup flow
	sessions      map[string]time.Time
}

// NewTOTP creates a TOTP service. It loads the secret from file/env if exists.
func NewTOTP() *TOTP {
	t := &TOTP{
		sessions: make(map[string]time.Time),
	}

	// Try loading from env first
	if secret := os.Getenv("PGAIO_TOTP_SECRET"); secret != "" {
		t.secret = secret
		log.Println("🔐 TOTP secret loaded from environment")
		return t
	}

	// Try loading from persistent file
	data, err := os.ReadFile(totpSecretFile)
	if err == nil && len(data) > 0 {
		t.secret = strings.TrimSpace(string(data))
		log.Println("🔐 TOTP secret loaded from file")
		return t
	}

	log.Println("🔐 TOTP not yet configured — setup required via Web UI")
	return t
}

// IsSetup returns true if TOTP has been configured and saved.
func (t *TOTP) IsSetup() bool {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.secret != ""
}

// GeneratePendingSecret creates a new temporary secret for the setup flow.
// Returns secret string + otpauth URL. Does NOT save to file.
func (t *TOTP) GeneratePendingSecret() (map[string]string, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	key, err := totp.Generate(totp.GenerateOpts{
		Issuer:      totpIssuer,
		AccountName: totpAccountName,
		SecretSize:  20,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to generate TOTP secret: %w", err)
	}

	t.pendingSecret = key.Secret()

	url := fmt.Sprintf("otpauth://totp/%s:%s?secret=%s&issuer=%s&algorithm=SHA1&digits=6&period=30",
		totpIssuer, totpAccountName, t.pendingSecret, totpIssuer)

	return map[string]string{
		"secret":  formatSecret(t.pendingSecret),
		"url":     url,
		"issuer":  totpIssuer,
		"account": totpAccountName,
	}, nil
}

// ConfirmSetup verifies the code against the pending secret.
// If valid, saves the secret to file and returns a new session ID.
func (t *TOTP) ConfirmSetup(code string) (string, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.secret != "" {
		return "", fmt.Errorf("TOTP already configured")
	}
	if t.pendingSecret == "" {
		return "", fmt.Errorf("no pending setup — call setup first")
	}
	if !totp.Validate(code, t.pendingSecret) {
		return "", fmt.Errorf("invalid TOTP code")
	}

	// Save to file
	t.secret = t.pendingSecret
	t.pendingSecret = ""
	if err := os.WriteFile(totpSecretFile, []byte(t.secret), 0600); err != nil {
		log.Printf("⚠️  Failed to persist TOTP secret: %v", err)
	}
	log.Println("🔐 TOTP setup confirmed and saved")

	// Create session
	sessionID := t.createSessionLocked()
	return sessionID, nil
}

// Validate checks if the provided TOTP code is valid against the saved secret.
func (t *TOTP) Validate(code string) bool {
	t.mu.RLock()
	defer t.mu.RUnlock()
	if t.secret == "" {
		return false
	}
	return totp.Validate(code, t.secret)
}

// Login validates the TOTP code and creates a new session.
func (t *TOTP) Login(code string) (string, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.secret == "" {
		return "", fmt.Errorf("TOTP not configured")
	}
	if !totp.Validate(code, t.secret) {
		return "", fmt.Errorf("invalid TOTP code")
	}
	sessionID := t.createSessionLocked()
	log.Println("🔐 Session created")
	return sessionID, nil
}

// ValidateSession checks if a session is valid and refreshes its expiry (sliding window).
func (t *TOTP) ValidateSession(sessionID string) bool {
	if sessionID == "" {
		return false
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	expiry, ok := t.sessions[sessionID]
	if !ok {
		return false
	}
	if time.Now().After(expiry) {
		delete(t.sessions, sessionID)
		return false
	}
	// Refresh — sliding window
	t.sessions[sessionID] = time.Now().Add(sessionTTL)
	return true
}

// CheckSession checks if a session is valid WITHOUT refreshing it.
func (t *TOTP) CheckSession(sessionID string) bool {
	if sessionID == "" {
		return false
	}
	t.mu.RLock()
	defer t.mu.RUnlock()
	expiry, ok := t.sessions[sessionID]
	if !ok {
		return false
	}
	return time.Now().Before(expiry)
}

// createSessionLocked generates a random session ID and stores it. Caller must hold mu.
func (t *TOTP) createSessionLocked() string {
	b := make([]byte, 32)
	rand.Read(b)
	id := hex.EncodeToString(b)
	t.sessions[id] = time.Now().Add(sessionTTL)
	// Cleanup old sessions
	now := time.Now()
	for k, v := range t.sessions {
		if now.After(v) {
			delete(t.sessions, k)
		}
	}
	return id
}

// GetSetupInfo returns the TOTP info for already-configured secret (used by legacy).
func (t *TOTP) GetSetupInfo() map[string]string {
	t.mu.RLock()
	defer t.mu.RUnlock()
	url := fmt.Sprintf("otpauth://totp/%s:%s?secret=%s&issuer=%s&algorithm=SHA1&digits=6&period=30",
		totpIssuer, totpAccountName, t.secret, totpIssuer)
	return map[string]string{
		"secret":  formatSecret(t.secret),
		"url":     url,
		"issuer":  totpIssuer,
		"account": totpAccountName,
	}
}

func formatSecret(s string) string {
	s = strings.ToUpper(strings.TrimRight(s, "="))
	if _, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(s); err != nil {
		return s
	}
	var parts []string
	for i := 0; i < len(s); i += 4 {
		end := i + 4
		if end > len(s) {
			end = len(s)
		}
		parts = append(parts, s[i:end])
	}
	return strings.Join(parts, " ")
}
