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

	res, err := store.Tidy()
	if err != nil {
		t.Fatalf("Tidy returned error: %v", err)
	}
	if res.RemovedCount != 1 {
		t.Fatalf("Tidy removed %d backup(s), want 1", res.RemovedCount)
	}
	if len(res.ChangedPaths) != 1 || res.ChangedPaths[0] != filepath.Dir(unreferencedPath) {
		t.Fatalf("unexpected changed paths: %#v", res.ChangedPaths)
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

	res, err := store.Tidy()
	if err != nil {
		t.Fatalf("Tidy returned error: %v", err)
	}
	if res.RemovedCount != 1 {
		t.Fatalf("Tidy removed %d backup(s), want 1", res.RemovedCount)
	}
	if len(res.ChangedPaths) != 1 || res.ChangedPaths[0] != filepath.Dir(backupPath) {
		t.Fatalf("unexpected changed paths: %#v", res.ChangedPaths)
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

func TestLoadRejectsSourcePathOutsideRoot(t *testing.T) {
	t.Parallel()

	store := testInstalledStore(t)
	sourceDir := t.TempDir()
	destDir := t.TempDir()

	manifest := fmt.Sprintf(`[tohru]
version = "%s"

[source]
name = "example"
description = "example source"

[[file]]
source = "../outside"
dest = %q
`, version.Version, filepath.Join(destDir, "managed"))

	if err := os.WriteFile(filepath.Join(sourceDir, "tohru.toml"), []byte(manifest), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	_, err := store.Load(sourceDir, false)
	if err == nil {
		t.Fatal("expected Load to reject source path escaping source root")
	}
	if !strings.Contains(err.Error(), "path escapes source root") {
		t.Fatalf("unexpected error: %v", err)
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

func TestLoadReportsSourceNamesAndUnloadedSource(t *testing.T) {
	t.Parallel()

	store := testInstalledStore(t)
	targetRoot := t.TempDir()

	alphaSource := writeSourceManifest(t, "alpha", filepath.Join(targetRoot, "alpha-managed"))
	betaSource := writeSourceManifest(t, "beta", filepath.Join(targetRoot, "beta-managed"))

	first, err := store.Load(alphaSource, false)
	if err != nil {
		t.Fatalf("Load alpha returned error: %v", err)
	}
	if first.SourceName != "alpha" {
		t.Fatalf("first SourceName = %q, want alpha", first.SourceName)
	}
	if first.UnloadedSourceName != "" || first.UnloadedTrackedCount != 0 {
		t.Fatalf("first unload details = (%q, %d), want empty", first.UnloadedSourceName, first.UnloadedTrackedCount)
	}

	second, err := store.Load(betaSource, false)
	if err != nil {
		t.Fatalf("Load beta returned error: %v", err)
	}
	if second.SourceName != "beta" {
		t.Fatalf("second SourceName = %q, want beta", second.SourceName)
	}
	if second.UnloadedSourceName != "alpha" {
		t.Fatalf("second UnloadedSourceName = %q, want alpha", second.UnloadedSourceName)
	}
	if second.UnloadedTrackedCount != 1 {
		t.Fatalf("second UnloadedTrackedCount = %d, want 1", second.UnloadedTrackedCount)
	}

	lck, err := store.LoadLock()
	if err != nil {
		t.Fatalf("LoadLock returned error: %v", err)
	}
	if lck.Manifest.Name != "beta" {
		t.Fatalf("Lock manifest name = %q, want beta", lck.Manifest.Name)
	}
}

func TestUnloadReportsLoadedSourceName(t *testing.T) {
	t.Parallel()

	store := testInstalledStore(t)
	targetRoot := t.TempDir()
	source := writeSourceManifest(t, "named-source", filepath.Join(targetRoot, "managed"))

	if _, err := store.Load(source, false); err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	res, err := store.Unload(false)
	if err != nil {
		t.Fatalf("Unload returned error: %v", err)
	}
	if res.SourceName != "named-source" {
		t.Fatalf("Unload SourceName = %q, want named-source", res.SourceName)
	}
	if res.RemovedCount != 1 {
		t.Fatalf("Unload RemovedCount = %d, want 1", res.RemovedCount)
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

func writeSourceManifest(t *testing.T, name, managedPath string) string {
	t.Helper()

	sourceDir := t.TempDir()
	manifestContent := fmt.Sprintf(`[tohru]
version = "%s"

[source]
name = %q
description = "test source"

[[dir]]
path = %q
`, version.Version, name, managedPath)

	if err := os.WriteFile(filepath.Join(sourceDir, "tohru.toml"), []byte(manifestContent), 0o644); err != nil {
		t.Fatalf("write source manifest: %v", err)
	}
	return sourceDir
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
