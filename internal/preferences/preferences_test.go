package preferences

import (
	"os"
	"path/filepath"
	"testing"
)

func TestOpenDefaultsAlertUnansweredInvitationsOn(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "preferences.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !store.AlertUnansweredInvitations() {
		t.Fatal("unanswered invitation alerts should default on")
	}
}

func TestSetAlertUnansweredInvitationsPersists(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "preferences.json")
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetAlertUnansweredInvitations(false); err != nil {
		t.Fatal(err)
	}
	reloaded, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.AlertUnansweredInvitations() {
		t.Fatal("saved false value was not restored")
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("preferences mode = %o, want 600", info.Mode().Perm())
	}
}

func TestOpenMissingFieldKeepsDefault(t *testing.T) {
	path := filepath.Join(t.TempDir(), "preferences.json")
	if err := os.WriteFile(path, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if !store.AlertUnansweredInvitations() {
		t.Fatal("missing preference should preserve the default")
	}
}
