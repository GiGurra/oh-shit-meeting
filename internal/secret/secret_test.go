package secret

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// isolateFile points filePath() at a temp dir via XDG_CONFIG_HOME (Linux) or
// overrides UserConfigDir's fallbacks. For cross-platform safety we set HOME
// and XDG_CONFIG_HOME both — os.UserConfigDir consults these.
func isolateFile(t *testing.T) string {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
	t.Setenv("HOME", tmp)
	t.Setenv("APPDATA", tmp) // Windows
	return tmp
}

func TestFileRoundTrip(t *testing.T) {
	isolateFile(t)

	if err := fileSet("svc", "user1", "hunter2"); err != nil {
		t.Fatalf("fileSet: %v", err)
	}

	got, err := fileGet("svc", "user1")
	if err != nil {
		t.Fatalf("fileGet: %v", err)
	}
	if got != "hunter2" {
		t.Fatalf("got %q, want hunter2", got)
	}

	if err := fileDelete("svc", "user1"); err != nil {
		t.Fatalf("fileDelete: %v", err)
	}

	_, err = fileGet("svc", "user1")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("after delete: want ErrNotFound, got %v", err)
	}
}

func TestFileNotFound(t *testing.T) {
	isolateFile(t)
	_, err := fileGet("svc", "missing")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

func TestFilePermissions(t *testing.T) {
	if os.Getenv("GOOS") == "windows" {
		t.Skip("unix perms only")
	}
	isolateFile(t)

	if err := fileSet("svc", "u", "v"); err != nil {
		t.Fatalf("fileSet: %v", err)
	}

	fi, err := os.Stat(filePath())
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if mode := fi.Mode().Perm(); mode != 0o600 {
		t.Errorf("file mode = %o, want 0600", mode)
	}

	di, err := os.Stat(filepath.Dir(filePath()))
	if err != nil {
		t.Fatalf("stat dir: %v", err)
	}
	if mode := di.Mode().Perm(); mode != 0o700 {
		t.Errorf("dir mode = %o, want 0700", mode)
	}
}

func TestMultipleKeys(t *testing.T) {
	isolateFile(t)

	pairs := map[string]string{
		"svc/a": "1",
		"svc/b": "2",
		"other/a": "3",
	}
	for k, v := range pairs {
		// parse "svc/user" back into service, user
		var svc, user string
		for i := 0; i < len(k); i++ {
			if k[i] == '/' {
				svc, user = k[:i], k[i+1:]
				break
			}
		}
		if err := fileSet(svc, user, v); err != nil {
			t.Fatalf("fileSet(%q): %v", k, err)
		}
	}

	got, _ := fileGet("svc", "a")
	if got != "1" {
		t.Errorf("svc/a = %q, want 1", got)
	}
	got, _ = fileGet("other", "a")
	if got != "3" {
		t.Errorf("other/a = %q, want 3", got)
	}

	if err := fileDelete("svc", "a"); err != nil {
		t.Fatalf("fileDelete: %v", err)
	}
	got, _ = fileGet("svc", "b")
	if got != "2" {
		t.Errorf("svc/b after deleting svc/a = %q, want 2", got)
	}
}
