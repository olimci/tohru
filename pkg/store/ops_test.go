package store

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/olimci/tohru/pkg/manifest"
	"github.com/olimci/tohru/pkg/store/state"
	"github.com/olimci/tohru/pkg/version"
)

func TestLoadReturnsWarningWhenProfileCacheWriteFails(t *testing.T) {
	t.Cleanup(func() {
		saveProfilesCache = func(s Store, profiles map[string]state.Profile) error {
			return s.SaveProfiles(profiles)
		}
	})

	saveProfilesCache = func(Store, map[string]state.Profile) error {
		return errors.New("cache offline")
	}

	root := t.TempDir()
	s := Store{Root: filepath.Join(root, "store")}
	profileDir := filepath.Join(root, "profile")
	targetDir := filepath.Join(root, "target")

	srcDir := filepath.Join(profileDir, "src")
	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		t.Fatalf("mkdir source dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "dot_zshrc"), []byte("export TEST=1\n"), 0o644); err != nil {
		t.Fatalf("write source file: %v", err)
	}

	m := manifest.Manifest{
		Tohru: manifest.Tohru{Version: version.Version},
		Profile: manifest.Profile{
			Slug: "test-profile",
			Name: "Test Profile",
		},
		Trees: map[string]manifest.Tree{
			"src": {
				Dest: targetDir,
				Files: map[string]any{
					".zshrc": map[string]any{"mode": "copy"},
				},
			},
		},
	}
	if err := manifest.Write(filepath.Join(profileDir, manifest.Name), m); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	res, err := s.Load(profileDir, Options{})
	if err != nil {
		t.Fatalf("load profile: %v", err)
	}
	if len(res.Warnings) != 1 || !strings.Contains(res.Warnings[0], "profile cache update failed") {
		t.Fatalf("expected profile cache warning, got %v", res.Warnings)
	}

	got, err := os.ReadFile(filepath.Join(targetDir, ".zshrc"))
	if err != nil {
		t.Fatalf("read loaded target: %v", err)
	}
	if string(got) != "export TEST=1\n" {
		t.Fatalf("unexpected target contents: %q", got)
	}

	lck, err := s.LoadState()
	if err != nil {
		t.Fatalf("load state: %v", err)
	}
	if lck.Manifest.State != "loaded" || lck.Manifest.Loc != profileDir {
		t.Fatalf("unexpected state after load: %+v", lck.Manifest)
	}
}

func TestLoadPreservesCopiedSymlinks(t *testing.T) {
	root := t.TempDir()
	s := Store{Root: filepath.Join(root, "store")}
	profileDir := filepath.Join(root, "profile")
	targetDir := filepath.Join(root, "target")

	srcDir := filepath.Join(profileDir, "src")
	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		t.Fatalf("mkdir source dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "real.txt"), []byte("hello\n"), 0o644); err != nil {
		t.Fatalf("write symlink target: %v", err)
	}
	if err := os.Symlink("real.txt", filepath.Join(srcDir, "dot_link")); err != nil {
		t.Fatalf("create source symlink: %v", err)
	}

	m := manifest.Manifest{
		Tohru: manifest.Tohru{Version: version.Version},
		Profile: manifest.Profile{
			Slug: "symlink-profile",
			Name: "Symlink Profile",
		},
		Trees: map[string]manifest.Tree{
			"src": {
				Dest: targetDir,
				Files: map[string]any{
					".link": map[string]any{"mode": "copy"},
				},
			},
		},
	}
	if err := manifest.Write(filepath.Join(profileDir, manifest.Name), m); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	if _, err := s.Load(profileDir, Options{}); err != nil {
		t.Fatalf("load profile: %v", err)
	}

	destPath := filepath.Join(targetDir, ".link")
	info, err := os.Lstat(destPath)
	if err != nil {
		t.Fatalf("lstat loaded path: %v", err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("expected loaded path to remain a symlink, mode=%s", info.Mode())
	}
	target, err := os.Readlink(destPath)
	if err != nil {
		t.Fatalf("read loaded symlink: %v", err)
	}
	if target != "real.txt" {
		t.Fatalf("unexpected symlink target %q", target)
	}
}

func TestUnloadRollsBackOnRestoreFailure(t *testing.T) {
	root := t.TempDir()
	s := Store{Root: filepath.Join(root, "store")}
	if err := s.Install(); err != nil {
		t.Fatalf("install store: %v", err)
	}

	targetDir := filepath.Join(root, "target")
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		t.Fatalf("mkdir target dir: %v", err)
	}

	pathA := filepath.Join(targetDir, "a")
	pathB := filepath.Join(targetDir, "b")
	if err := os.WriteFile(pathA, []byte("managed a\n"), 0o644); err != nil {
		t.Fatalf("write %s: %v", pathA, err)
	}
	if err := os.WriteFile(pathB, []byte("managed b\n"), 0o644); err != nil {
		t.Fatalf("write %s: %v", pathB, err)
	}

	currA, err := snapshot(pathA)
	if err != nil {
		t.Fatalf("snapshot %s: %v", pathA, err)
	}
	currB, err := snapshot(pathB)
	if err != nil {
		t.Fatalf("snapshot %s: %v", pathB, err)
	}

	lck := DefaultState()
	lck.Manifest.State = "loaded"
	lck.Manifest.Loc = filepath.Join(root, "profile")
	lck.Files = []state.File{
		{
			Path:    pathA,
			Current: currA,
			Previous: &state.Object{
				Path:   filepath.Join(s.BackupsPath(), "missing", "object"),
				Digest: "file:sha256:deadbeef",
			},
		},
		{
			Path:    pathB,
			Current: currB,
		},
	}
	if err := s.SaveState(lck); err != nil {
		t.Fatalf("save state: %v", err)
	}

	if _, err := s.Unload(Options{}); err == nil {
		t.Fatalf("expected unload to fail")
	} else if !strings.Contains(err.Error(), "rolled back to previous state") {
		t.Fatalf("expected rollback error, got %v", err)
	}

	gotA, err := os.ReadFile(pathA)
	if err != nil {
		t.Fatalf("read %s after rollback: %v", pathA, err)
	}
	if string(gotA) != "managed a\n" {
		t.Fatalf("unexpected contents for %s after rollback: %q", pathA, gotA)
	}

	gotB, err := os.ReadFile(pathB)
	if err != nil {
		t.Fatalf("read %s after rollback: %v", pathB, err)
	}
	if string(gotB) != "managed b\n" {
		t.Fatalf("unexpected contents for %s after rollback: %q", pathB, gotB)
	}

	after, err := s.LoadState()
	if err != nil {
		t.Fatalf("load state after rollback: %v", err)
	}
	if after.Manifest.State != "loaded" || len(after.Files) != 2 {
		t.Fatalf("unexpected state after rollback: %+v", after)
	}
}

func TestUnloadReturnsWarningWhenCleanupFailsAfterCommit(t *testing.T) {
	t.Cleanup(func() {
		pruneBackupsFunc = pruneBackups
	})

	pruneBackupsFunc = func(Store, []state.File, func(string)) (int, error) {
		return 0, errors.New("cleanup unavailable")
	}

	root := t.TempDir()
	s := Store{Root: filepath.Join(root, "store")}
	if err := s.Install(); err != nil {
		t.Fatalf("install store: %v", err)
	}

	lck := DefaultState()
	lck.Manifest.State = "loaded"
	lck.Manifest.Loc = filepath.Join(root, "profile")
	if err := s.SaveState(lck); err != nil {
		t.Fatalf("save state: %v", err)
	}

	res, err := s.Unload(Options{})
	if err != nil {
		t.Fatalf("unload: %v", err)
	}
	if len(res.Warnings) != 1 || !strings.Contains(res.Warnings[0], "backup cleanup failed") {
		t.Fatalf("expected cleanup warning, got %v", res.Warnings)
	}

	after, err := s.LoadState()
	if err != nil {
		t.Fatalf("load state after unload: %v", err)
	}
	if after.Manifest.State != "unloaded" {
		t.Fatalf("expected unloaded state, got %+v", after.Manifest)
	}
}

func TestLockSerializesConcurrentCalls(t *testing.T) {
	s := Store{Root: filepath.Join(t.TempDir(), "store")}

	firstEntered := make(chan struct{})
	releaseFirst := make(chan struct{})
	secondEntered := make(chan struct{})
	done := make(chan error, 2)

	go func() {
		lock, err := s.Lock()
		if err != nil {
			done <- err
			return
		}
		close(firstEntered)
		<-releaseFirst
		done <- lock.Unlock()
	}()

	<-firstEntered

	go func() {
		lock, err := s.Lock()
		if err != nil {
			done <- err
			return
		}
		close(secondEntered)
		done <- lock.Unlock()
	}()

	select {
	case <-secondEntered:
		t.Fatalf("second lock acquired before first released")
	case <-time.After(150 * time.Millisecond):
	}

	close(releaseFirst)

	select {
	case <-secondEntered:
	case <-time.After(time.Second):
		t.Fatalf("timed out waiting for second lock acquisition")
	}

	for i := 0; i < 2; i++ {
		if err := <-done; err != nil {
			t.Fatalf("lock returned error: %v", err)
		}
	}
}
