package service

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPasswordAuthLoginAndSession(t *testing.T) {
	auth := newPasswordAuth("correct-password")

	if _, err := auth.Login("wrong-password"); err == nil {
		t.Fatal("expected incorrect password to be rejected")
	}

	sessionID, err := auth.Login("correct-password")
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	if !auth.CheckSession(sessionID) {
		t.Fatal("new session should be valid")
	}
	if !auth.ValidateSession(sessionID) {
		t.Fatal("new session should validate")
	}
}

func TestGeneratePassword(t *testing.T) {
	password, err := generatePassword()
	if err != nil {
		t.Fatalf("generate password: %v", err)
	}
	if len(password) != 32 {
		t.Fatalf("password length = %d, want 32", len(password))
	}
}

func TestLoadPasswordGeneratesAndPersists(t *testing.T) {
	path := filepath.Join(t.TempDir(), "password")
	password, generated, err := loadPassword(path, "")
	if err != nil {
		t.Fatalf("first load: %v", err)
	}
	if !generated {
		t.Fatal("expected first load to generate a password")
	}
	if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("password file permissions = %v, err = %v; want 0600", info.Mode().Perm(), err)
	}

	persisted, generated, err := loadPassword(path, "")
	if err != nil {
		t.Fatalf("second load: %v", err)
	}
	if generated || persisted != password {
		t.Fatal("expected the generated password to be reused")
	}
}
