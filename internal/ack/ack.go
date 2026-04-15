package ack

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"
)

// Store defines the interface for acknowledgment storage
type Store interface {
	IsAcked(eventID, reminderID string) bool
	MarkAcked(eventID, reminderID string) error
}

// FileStore implements Store using the filesystem
type FileStore struct{}

func (s *FileStore) IsAcked(eventID, reminderID string) bool {
	return IsAcked(eventID, reminderID)
}

func (s *FileStore) MarkAcked(eventID, reminderID string) error {
	return MarkAcked(eventID, reminderID)
}

// dirFunc returns the directory for storing ack files.
// It's a variable so tests can override it.
var dirFunc = func() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ".oh-shit-meeting"
	}
	return filepath.Join(home, ".oh-shit-meeting")
}

func path(eventID, reminderID string) string {
	safeEventID := filepath.Base(eventID)
	return filepath.Join(dirFunc(), safeEventID, reminderID+".acked")
}

func IsAcked(eventID, reminderID string) bool {
	_, err := os.Stat(path(eventID, reminderID))
	return err == nil
}

func MarkAcked(eventID, reminderID string) error {
	ackPath := path(eventID, reminderID)
	ackDir := filepath.Dir(ackPath)

	if err := os.MkdirAll(ackDir, 0755); err != nil {
		return fmt.Errorf("failed to create ack directory: %w", err)
	}

	f, err := os.Create(ackPath)
	if err != nil {
		return fmt.Errorf("failed to create ack file: %w", err)
	}
	return f.Close()
}

// Cleanup removes ack directories older than maxAge.
func Cleanup(maxAge time.Duration) {
	dir := dirFunc()
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}

	cutoff := time.Now().Add(-maxAge)
	removed := 0
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		if info.ModTime().Before(cutoff) {
			if err := os.RemoveAll(filepath.Join(dir, entry.Name())); err == nil {
				removed++
			}
		}
	}
	if removed > 0 {
		slog.Info("Cleaned up old ack files", "removed", removed)
	}
}
