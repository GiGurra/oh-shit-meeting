// Package secret wraps the system keychain with an opt-in plaintext file
// fallback, enabled via AcceptInsecure(true). The fallback is only used when
// the keychain is unavailable (e.g. WSL, headless Linux, some Docker setups).
package secret

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"

	"github.com/zalando/go-keyring"
)

// ErrNotFound is returned when no entry exists in either the keychain or the file store.
var ErrNotFound = keyring.ErrNotFound

var (
	mu               sync.Mutex
	acceptInsecure   bool
	insecureWarnOnce sync.Once
)

// AcceptInsecure toggles the plaintext file fallback. Call once at startup.
func AcceptInsecure(enabled bool) {
	mu.Lock()
	acceptInsecure = enabled
	mu.Unlock()
}

func insecureEnabled() bool {
	mu.Lock()
	defer mu.Unlock()
	return acceptInsecure
}

// Get retrieves a secret. Tries the keychain first, then the file fallback
// if insecure storage is accepted. Returns ErrNotFound if neither has it.
func Get(service, user string) (string, error) {
	v, err := keyring.Get(service, user)
	if err == nil {
		return v, nil
	}
	if !insecureEnabled() {
		return "", err
	}
	v2, err2 := fileGet(service, user)
	if err2 == nil {
		return v2, nil
	}
	// Both missing — report a not-found.
	if errors.Is(err, ErrNotFound) || errors.Is(err2, ErrNotFound) {
		return "", ErrNotFound
	}
	return "", fmt.Errorf("keychain: %v; file: %v", err, err2)
}

// Set stores a secret. Tries the keychain first; on failure and if insecure
// storage is accepted, writes to the plaintext file with a warning.
func Set(service, user, value string) error {
	err := keyring.Set(service, user, value)
	if err == nil {
		return nil
	}
	if !insecureEnabled() {
		return err
	}
	insecureWarnOnce.Do(func() {
		slog.Warn("keychain unavailable — falling back to plaintext file storage",
			"path", filePath(),
			"keychainError", err)
	})
	return fileSet(service, user, value)
}

// Location reports where a secret currently lives: "keychain", "file", or ""
// if absent. Useful for honest user-facing messages after Set.
func Location(service, user string) string {
	if _, err := keyring.Get(service, user); err == nil {
		return "keychain"
	}
	if !insecureEnabled() {
		return ""
	}
	if _, err := fileGet(service, user); err == nil {
		return "file (" + filePath() + ")"
	}
	return ""
}

// Delete removes a secret from the keychain and the file fallback (best-effort).
func Delete(service, user string) error {
	kerr := keyring.Delete(service, user)
	ferr := fileDelete(service, user)
	// Success if at least one side removed something (or neither had it).
	if (kerr == nil || errors.Is(kerr, ErrNotFound)) &&
		(ferr == nil || errors.Is(ferr, ErrNotFound)) {
		return nil
	}
	if kerr != nil && ferr != nil {
		return fmt.Errorf("keychain: %v; file: %v", kerr, ferr)
	}
	if kerr != nil {
		return kerr
	}
	return ferr
}

// ---------- file fallback ----------

func filePath() string {
	dir, err := os.UserConfigDir()
	if err != nil || dir == "" {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, ".config")
	}
	return filepath.Join(dir, "oh-shit-meeting", "secrets.json")
}

func fileKey(service, user string) string {
	return service + "/" + user
}

func loadFile() (map[string]string, error) {
	data, err := os.ReadFile(filePath())
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]string{}, nil
		}
		return nil, err
	}
	if len(data) == 0 {
		return map[string]string{}, nil
	}
	m := map[string]string{}
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, err
	}
	return m, nil
}

func saveFile(m map[string]string) error {
	path := filePath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func fileGet(service, user string) (string, error) {
	m, err := loadFile()
	if err != nil {
		return "", err
	}
	v, ok := m[fileKey(service, user)]
	if !ok {
		return "", ErrNotFound
	}
	return v, nil
}

func fileSet(service, user, value string) error {
	m, err := loadFile()
	if err != nil {
		return err
	}
	m[fileKey(service, user)] = value
	return saveFile(m)
}

func fileDelete(service, user string) error {
	m, err := loadFile()
	if err != nil {
		return err
	}
	k := fileKey(service, user)
	if _, ok := m[k]; !ok {
		return ErrNotFound
	}
	delete(m, k)
	return saveFile(m)
}
