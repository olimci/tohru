package store

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/olimci/tohru/pkg/store/config"
	"github.com/olimci/tohru/pkg/store/lock"
	"github.com/olimci/tohru/pkg/version"
)

func TestTidyRemovesOnlyUnreferencedBackups(t *testing.T) {
	t.Parallel()

	store := testInstalledStore(t)

	referencedCID := "file:sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	unreferencedCID := "file:sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"

	referencedPath := writeBackupObject(t, store, referencedCID)
	unreferencedPath := writeBackupObject(t, store, unreferencedCID)

	lck := DefaultLock()
	lck.Files = []lock.File{
		{
			Path: "/tmp/managed-path",
			Curr: lock.Object{
				Path:   "/tmp/managed-path",
				Digest: "file:sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
			},
			Prev: &lock.Object{
				Digest: referencedCID,
			},
		},
	}
	if err := store.SaveLock(lck); err != nil {
		t.Fatalf("SaveLock returned error: %v", err)
	}

	removed, err := store.Tidy()
	if err != nil {
		t.Fatalf("Tidy returned error: %v", err)
	}
	if removed != 1 {
		t.Fatalf("Tidy removed %d backup(s), want 1", removed)
	}

	if _, err := os.Stat(referencedPath); err != nil {
		t.Fatalf("referenced backup should remain, stat returned: %v", err)
	}
	if _, err := os.Stat(unreferencedPath); !os.IsNotExist(err) {
		t.Fatalf("unreferenced backup should be removed, stat err = %v", err)
	}
}

func TestTidyPurgesBackupsWhenAutomaticCleanIsDisabled(t *testing.T) {
	t.Parallel()

	store := testInstalledStore(t)

	cfg, err := store.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig returned error: %v", err)
	}
	cfg.Options.Clean = false
	if err := store.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig returned error: %v", err)
	}

	backupCID := "file:sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"
	backupPath := writeBackupObject(t, store, backupCID)

	if _, err := store.Unload(false); err != nil {
		t.Fatalf("Unload returned error: %v", err)
	}
	if _, err := os.Stat(backupPath); err != nil {
		t.Fatalf("backup should remain when options.clean=false, stat returned: %v", err)
	}

	removed, err := store.Tidy()
	if err != nil {
		t.Fatalf("Tidy returned error: %v", err)
	}
	if removed != 1 {
		t.Fatalf("Tidy removed %d backup(s), want 1", removed)
	}
	if _, err := os.Stat(backupPath); !os.IsNotExist(err) {
		t.Fatalf("backup should be removed by tidy, stat err = %v", err)
	}
}

func TestLoadConfigRejectsUnsupportedVersion(t *testing.T) {
	t.Parallel()

	store := testInstalledStore(t)

	cfg := config.Config{
		Tohru: config.Tohru{
			Version: incompatibleVersion(t),
		},
		Options: config.Options{
			Backup: true,
			Clean:  true,
		},
	}
	if err := store.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig returned error: %v", err)
	}

	_, err := store.LoadConfig()
	if err == nil {
		t.Fatal("expected LoadConfig to reject unsupported version")
	}
	if !strings.Contains(err.Error(), "unsupported config version") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadRejectsUnsupportedSourceVersion(t *testing.T) {
	t.Parallel()

	store := testInstalledStore(t)
	sourceDir := t.TempDir()

	manifest := fmt.Sprintf(`[tohru]
version = "%s"

[source]
name = "example"
description = "example source"
`, incompatibleVersion(t))

	if err := os.WriteFile(filepath.Join(sourceDir, "tohru.toml"), []byte(manifest), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	_, err := store.Load(sourceDir, false)
	if err == nil {
		t.Fatal("expected Load to reject unsupported source version")
	}
	if !strings.Contains(err.Error(), "unsupported source version") {
		t.Fatalf("unexpected error: %v", err)
	}

	lck, lockErr := store.LoadLock()
	if lockErr != nil {
		t.Fatalf("LoadLock returned error: %v", lockErr)
	}
	if lck.Manifest.State != "unloaded" {
		t.Fatalf("lock state changed on failed load: got %q, want unloaded", lck.Manifest.State)
	}
}

func TestStatusSummarizesTrackedAndBackups(t *testing.T) {
	t.Parallel()

	store := testInstalledStore(t)

	presentCID := "file:sha256:1111111111111111111111111111111111111111111111111111111111111111"
	missingCID := "file:sha256:2222222222222222222222222222222222222222222222222222222222222222"
	orphanCID := "file:sha256:3333333333333333333333333333333333333333333333333333333333333333"

	_ = writeBackupObject(t, store, presentCID)
	_ = writeBackupObject(t, store, orphanCID)

	lck := DefaultLock()
	lck.Manifest.State = "loaded"
	lck.Manifest.Loc = "/example/source"
	lck.Files = []lock.File{
		{
			Path: "/tmp/a",
			Curr: lock.Object{Path: "/tmp/a", Digest: "file:sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
			Prev: &lock.Object{Digest: presentCID},
		},
		{
			Path: "/tmp/b",
			Curr: lock.Object{Path: "/tmp/b", Digest: "file:sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"},
			Prev: &lock.Object{Digest: missingCID},
		},
		{
			Path: "/tmp/c",
			Curr: lock.Object{Path: "/tmp/c", Digest: "file:sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"},
			Prev: nil,
		},
	}
	if err := store.SaveLock(lck); err != nil {
		t.Fatalf("SaveLock returned error: %v", err)
	}

	status, err := store.Status()
	if err != nil {
		t.Fatalf("Status returned error: %v", err)
	}

	if status.Manifest.State != "loaded" {
		t.Fatalf("Status.Manifest.State = %q, want loaded", status.Manifest.State)
	}
	if len(status.Tracked) != 3 {
		t.Fatalf("Status.Tracked length = %d, want 3", len(status.Tracked))
	}

	trackedByPath := make(map[string]TrackedStatus, len(status.Tracked))
	for _, tracked := range status.Tracked {
		trackedByPath[tracked.Path] = tracked
	}
	if trackedByPath["/tmp/a"].PrevDigest != presentCID || !trackedByPath["/tmp/a"].BackupPresent {
		t.Fatalf("unexpected tracked status for /tmp/a: %#v", trackedByPath["/tmp/a"])
	}
	if trackedByPath["/tmp/b"].PrevDigest != missingCID || trackedByPath["/tmp/b"].BackupPresent {
		t.Fatalf("unexpected tracked status for /tmp/b: %#v", trackedByPath["/tmp/b"])
	}
	if trackedByPath["/tmp/c"].PrevDigest != "" || trackedByPath["/tmp/c"].BackupPresent {
		t.Fatalf("unexpected tracked status for /tmp/c: %#v", trackedByPath["/tmp/c"])
	}

	refsByDigest := make(map[string]BackupRefStatus, len(status.BackupRefs))
	for _, ref := range status.BackupRefs {
		refsByDigest[ref.Digest] = ref
	}
	if ref, ok := refsByDigest[presentCID]; !ok || !ref.Present || len(ref.Paths) != 1 || ref.Paths[0] != "/tmp/a" {
		t.Fatalf("unexpected backup ref for present CID: %#v", ref)
	}
	if ref, ok := refsByDigest[missingCID]; !ok || ref.Present || len(ref.Paths) != 1 || ref.Paths[0] != "/tmp/b" {
		t.Fatalf("unexpected backup ref for missing CID: %#v", ref)
	}

	if len(status.OrphanedBackups) != 1 || status.OrphanedBackups[0] != orphanCID {
		t.Fatalf("unexpected orphaned backups: %#v", status.OrphanedBackups)
	}
}

func TestStatusIncludesBrokenBackups(t *testing.T) {
	t.Parallel()

	store := testInstalledStore(t)
	brokenCID := "file:sha256:4444444444444444444444444444444444444444444444444444444444444444"

	brokenDir := filepath.Join(store.BackupsPath(), brokenCID)
	if err := os.MkdirAll(brokenDir, 0o755); err != nil {
		t.Fatalf("mkdir broken backup dir: %v", err)
	}

	status, err := store.Status()
	if err != nil {
		t.Fatalf("Status returned error: %v", err)
	}

	if len(status.BrokenBackups) != 1 || status.BrokenBackups[0] != brokenCID {
		t.Fatalf("unexpected broken backups: %#v", status.BrokenBackups)
	}
}

func testInstalledStore(t *testing.T) Store {
	t.Helper()

	store := Store{Root: t.TempDir()}
	if err := store.Install(); err != nil {
		t.Fatalf("Install returned error: %v", err)
	}
	return store
}

func writeBackupObject(t *testing.T, store Store, cid string) string {
	t.Helper()

	path := backupObjectPath(store, cid)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir backup path: %v", err)
	}
	if err := os.WriteFile(path, []byte("backup"), 0o644); err != nil {
		t.Fatalf("write backup object: %v", err)
	}
	return path
}

func incompatibleVersion(t *testing.T) string {
	t.Helper()

	current, err := version.ParseSemVer(version.Version)
	if err != nil {
		t.Fatalf("parse current version: %v", err)
	}

	return version.SemVer{
		Major: current.Major,
		Minor: current.Minor,
		Patch: current.Patch + 1,
	}.String()
}
