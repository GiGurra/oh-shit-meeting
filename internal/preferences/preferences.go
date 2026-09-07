package preferences

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
)

const fileName = "preferences.json"

// Store owns the small set of dashboard-editable preferences. Reads are safe
// from the reminder loop while the HTTP handler updates a value.
type Store struct {
	mu                         sync.RWMutex
	path                       string
	alertUnansweredInvitations bool
}

type diskPreferences struct {
	// A pointer distinguishes a missing value (default on) from an explicitly
	// saved false value.
	AlertUnansweredInvitations *bool `json:"alert_unanswered_invitations,omitempty"`
}

// OpenDefault loads preferences from the platform user config directory.
func OpenDefault() (*Store, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return &Store{alertUnansweredInvitations: true}, err
	}
	return Open(filepath.Join(dir, "oh-shit-meeting", fileName))
}

// Open loads a store from path. Missing files use safe, backwards-compatible
// defaults. A store is still returned alongside malformed/read errors so the
// daemon can continue and allow the user to overwrite the bad value.
func Open(path string) (*Store, error) {
	s := &Store{path: path, alertUnansweredInvitations: true}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return s, err
	}
	var saved diskPreferences
	if err := json.Unmarshal(data, &saved); err != nil {
		return s, err
	}
	if saved.AlertUnansweredInvitations != nil {
		s.alertUnansweredInvitations = *saved.AlertUnansweredInvitations
	}
	return s, nil
}

func (s *Store) AlertUnansweredInvitations() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.alertUnansweredInvitations
}

func (s *Store) SetAlertUnansweredInvitations(enabled bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := json.MarshalIndent(diskPreferences{AlertUnansweredInvitations: &enabled}, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	// Write directly for portable replacement semantics: os.Rename cannot
	// replace an existing destination on Windows.
	if err := os.WriteFile(s.path, data, 0o600); err != nil {
		return err
	}
	if err := os.Chmod(s.path, 0o600); err != nil {
		return err
	}
	s.alertUnansweredInvitations = enabled
	return nil
}
