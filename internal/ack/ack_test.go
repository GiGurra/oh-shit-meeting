package ack

import (
	"os"
	"path/filepath"
	"testing"
)

func TestIsAcked_NotAcked(t *testing.T) {
	// Use a temporary directory
	tmpDir := t.TempDir()
	originalDirFunc := dirFunc
	dirFunc = func() string { return tmpDir }
	defer func() { dirFunc = originalDirFunc }()

	result := IsAcked("event-123", "global")
	if result {
		t.Error("expected IsAcked to return false for non-existent ack")
	}
}

func TestMarkAcked_CreatesFile(t *testing.T) {
	tmpDir := t.TempDir()
	originalDirFunc := dirFunc
	dirFunc = func() string { return tmpDir }
	defer func() { dirFunc = originalDirFunc }()

	err := MarkAcked("event-123", "global")
	if err != nil {
		t.Fatalf("MarkAcked failed: %v", err)
	}

	// Check file exists
	expectedPath := filepath.Join(tmpDir, "event-123", "global.acked")
	if _, err := os.Stat(expectedPath); os.IsNotExist(err) {
		t.Errorf("expected ack file to exist at %s", expectedPath)
	}
}

func TestMarkAcked_ThenIsAcked(t *testing.T) {
	tmpDir := t.TempDir()
	originalDirFunc := dirFunc
	dirFunc = func() string { return tmpDir }
	defer func() { dirFunc = originalDirFunc }()

	// Initially not acked
	if IsAcked("event-456", "10m") {
		t.Error("expected IsAcked to return false before marking")
	}

	// Mark as acked
	err := MarkAcked("event-456", "10m")
	if err != nil {
		t.Fatalf("MarkAcked failed: %v", err)
	}

	// Now should be acked
	if !IsAcked("event-456", "10m") {
		t.Error("expected IsAcked to return true after marking")
	}
}

func TestMarkAcked_MultipleReminders(t *testing.T) {
	tmpDir := t.TempDir()
	originalDirFunc := dirFunc
	dirFunc = func() string { return tmpDir }
	defer func() { dirFunc = originalDirFunc }()

	// Mark multiple reminders for same event
	if err := MarkAcked("event-789", "30m"); err != nil {
		t.Fatalf("MarkAcked 30m failed: %v", err)
	}
	if err := MarkAcked("event-789", "10m"); err != nil {
		t.Fatalf("MarkAcked 10m failed: %v", err)
	}
	if err := MarkAcked("event-789", "global"); err != nil {
		t.Fatalf("MarkAcked global failed: %v", err)
	}

	// All should be acked
	if !IsAcked("event-789", "30m") {
		t.Error("expected 30m to be acked")
	}
	if !IsAcked("event-789", "10m") {
		t.Error("expected 10m to be acked")
	}
	if !IsAcked("event-789", "global") {
		t.Error("expected global to be acked")
	}

	// Different reminder should not be acked
	if IsAcked("event-789", "5m") {
		t.Error("expected 5m to not be acked")
	}
}

func TestMarkAcked_DifferentEvents(t *testing.T) {
	tmpDir := t.TempDir()
	originalDirFunc := dirFunc
	dirFunc = func() string { return tmpDir }
	defer func() { dirFunc = originalDirFunc }()

	if err := MarkAcked("event-aaa", "global"); err != nil {
		t.Fatalf("MarkAcked event-aaa failed: %v", err)
	}

	// Different event should not be affected
	if IsAcked("event-bbb", "global") {
		t.Error("expected event-bbb to not be acked")
	}
}

func TestPath_SafeEventID(t *testing.T) {
	tmpDir := t.TempDir()
	originalDirFunc := dirFunc
	dirFunc = func() string { return tmpDir }
	defer func() { dirFunc = originalDirFunc }()

	// path() uses filepath.Base to sanitize event IDs
	// This prevents directory traversal attacks
	result := path("../../../etc/passwd", "global")

	// Should not contain the traversal attempt - should just be "passwd"
	expected := filepath.Join(tmpDir, "passwd", "global.acked")
	if result != expected {
		t.Errorf("expected path %s, got %s", expected, result)
	}
}

func TestMarkAcked_Idempotent(t *testing.T) {
	tmpDir := t.TempDir()
	originalDirFunc := dirFunc
	dirFunc = func() string { return tmpDir }
	defer func() { dirFunc = originalDirFunc }()

	// Marking same reminder twice should not error
	if err := MarkAcked("event-idem", "global"); err != nil {
		t.Fatalf("First MarkAcked failed: %v", err)
	}
	if err := MarkAcked("event-idem", "global"); err != nil {
		t.Fatalf("Second MarkAcked failed: %v", err)
	}

	if !IsAcked("event-idem", "global") {
		t.Error("expected event to still be acked after double marking")
	}
}

func TestFileStore_ImplementsInterface(t *testing.T) {
	var store Store = &FileStore{}
	_ = store // Verify it compiles
}

func TestFileStore_Integration(t *testing.T) {
	tmpDir := t.TempDir()
	originalDirFunc := dirFunc
	dirFunc = func() string { return tmpDir }
	defer func() { dirFunc = originalDirFunc }()

	store := &FileStore{}

	// Not acked initially
	if store.IsAcked("evt-fs", "global") {
		t.Error("expected not acked initially")
	}

	// Mark acked
	if err := store.MarkAcked("evt-fs", "global"); err != nil {
		t.Fatalf("FileStore.MarkAcked failed: %v", err)
	}

	// Now acked
	if !store.IsAcked("evt-fs", "global") {
		t.Error("expected acked after marking")
	}
}
