package service

import (
	"encoding/base32"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"

	"github.com/pquerna/otp"
	"github.com/pquerna/otp/totp"
)

const (
	totpIssuer      = "PGAIO"
	totpAccountName = "admin@pgaio"
	totpSecretFile  = "/bitnami/postgresql/.pgaio_totp_secret"
)

// TOTP manages time-based one-time password authentication.
type TOTP struct {
	mu     sync.RWMutex
	secret string
	key    *otp.Key
}

// NewTOTP creates a TOTP service. It loads the secret from file or generates a new one.
func NewTOTP() *TOTP {
	t := &TOTP{}

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

	// Generate new secret
	key, err := totp.Generate(totp.GenerateOpts{
		Issuer:      totpIssuer,
		AccountName: totpAccountName,
		SecretSize:  20,
	})
	if err != nil {
		log.Fatalf("failed to generate TOTP secret: %v", err)
	}
	t.secret = key.Secret()
	t.key = key

	// Persist to file
	if err := os.WriteFile(totpSecretFile, []byte(t.secret), 0600); err != nil {
		log.Printf("⚠️  Failed to persist TOTP secret to file: %v", err)
	}
	log.Println("🔐 TOTP secret generated and saved — scan QR code to set up authenticator")

	return t
}

// IsSetup returns true if TOTP has been configured.
func (t *TOTP) IsSetup() bool {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.secret != ""
}

// Validate checks if the provided TOTP code is valid.
func (t *TOTP) Validate(code string) bool {
	t.mu.RLock()
	defer t.mu.RUnlock()
	if t.secret == "" {
		return false
	}
	return totp.Validate(code, t.secret)
}

// GetSetupInfo returns the TOTP setup information (secret + otpauth URL).
func (t *TOTP) GetSetupInfo() map[string]string {
	t.mu.RLock()
	defer t.mu.RUnlock()

	url := fmt.Sprintf("otpauth://totp/%s:%s?secret=%s&issuer=%s&algorithm=SHA1&digits=6&period=30",
		totpIssuer, totpAccountName, t.secret, totpIssuer)

	// Format secret in groups of 4 for readability
	formatted := formatSecret(t.secret)

	return map[string]string{
		"secret":  formatted,
		"url":     url,
		"issuer":  totpIssuer,
		"account": totpAccountName,
	}
}

// GetSecret returns the raw base32 secret.
func (t *TOTP) GetSecret() string {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.secret
}

func formatSecret(s string) string {
	// Ensure valid base32 (uppercase, no padding issues)
	s = strings.ToUpper(strings.TrimRight(s, "="))
	// Validate it's proper base32
	if _, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(s); err != nil {
		return s // Return as-is if not valid base32
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
