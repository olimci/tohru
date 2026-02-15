package store

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/olimci/tohru/pkg/digest"
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

	if _, err := store.Unload(Options{}); err != nil {
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

	_, err := store.Load(sourceDir, Options{})
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

	_, err := store.Load(sourceDir, Options{})
	if err == nil {
		t.Fatal("expected Load to reject source path escaping source root")
	}
	if !strings.Contains(err.Error(), "path escapes source root") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadAllowsExistingUntrackedDir(t *testing.T) {
	t.Parallel()

	store := testInstalledStore(t)
	sourceDir := t.TempDir()
	targetDir := filepath.Join(t.TempDir(), "existing")

	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		t.Fatalf("mkdir target dir: %v", err)
	}
	sentinel := filepath.Join(targetDir, "sentinel.txt")
	if err := os.WriteFile(sentinel, []byte("keep"), 0o644); err != nil {
		t.Fatalf("write sentinel file: %v", err)
	}

	manifest := fmt.Sprintf(`[tohru]
version = "%s"

[source]
name = "example"
description = "example source"

[[dir]]
path = %q
tracked = false
`, version.Version, targetDir)

	if err := os.WriteFile(filepath.Join(sourceDir, "tohru.toml"), []byte(manifest), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	res, err := store.Load(sourceDir, Options{})
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if res.TrackedCount != 0 {
		t.Fatalf("Load tracked count = %d, want 0", res.TrackedCount)
	}

	if _, err := os.Stat(sentinel); err != nil {
		t.Fatalf("sentinel file should remain, stat err: %v", err)
	}
}

func TestLoadRejectsExistingTrackedDir(t *testing.T) {
	t.Parallel()

	store := testInstalledStore(t)
	sourceDir := t.TempDir()
	targetDir := filepath.Join(t.TempDir(), "existing")

	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		t.Fatalf("mkdir target dir: %v", err)
	}

	manifest := fmt.Sprintf(`[tohru]
version = "%s"

[source]
name = "example"
description = "example source"

[[dir]]
path = %q
`, version.Version, targetDir)

	if err := os.WriteFile(filepath.Join(sourceDir, "tohru.toml"), []byte(manifest), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	_, err := store.Load(sourceDir, Options{})
	if err == nil {
		t.Fatal("expected Load to reject existing tracked dir")
	}
	if !strings.Contains(err.Error(), "tracked dir destination already exists") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestStatusSummarizesTrackedAndBackups(t *testing.T) {
	t.Parallel()

	store := testInstalledStore(t)
	trackedRoot := t.TempDir()

	pathA := filepath.Join(trackedRoot, "a")
	pathB := filepath.Join(trackedRoot, "b")
	pathC := filepath.Join(trackedRoot, "c")
	if err := os.WriteFile(pathA, []byte("alpha"), 0o644); err != nil {
		t.Fatalf("write path a: %v", err)
	}
	if err := os.WriteFile(pathB, []byte("bravo"), 0o644); err != nil {
		t.Fatalf("write path b: %v", err)
	}
	if err := os.WriteFile(pathC, []byte("charlie"), 0o644); err != nil {
		t.Fatalf("write path c: %v", err)
	}

	digestA, err := digest.ForPath(pathA)
	if err != nil {
		t.Fatalf("digest path a: %v", err)
	}
	digestB, err := digest.ForPath(pathB)
	if err != nil {
		t.Fatalf("digest path b: %v", err)
	}
	digestC, err := digest.ForPath(pathC)
	if err != nil {
		t.Fatalf("digest path c: %v", err)
	}

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
			Path: pathA,
			Curr: lock.Object{Path: pathA, Digest: digestA.String()},
			Prev: &lock.Object{Digest: presentCID},
		},
		{
			Path: pathB,
			Curr: lock.Object{Path: pathB, Digest: digestB.String()},
			Prev: &lock.Object{Digest: missingCID},
		},
		{
			Path: pathC,
			Curr: lock.Object{Path: pathC, Digest: digestC.String()},
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
	if trackedByPath[pathA].PrevDigest != presentCID || !trackedByPath[pathA].BackupPresent || trackedByPath[pathA].Drifted || trackedByPath[pathA].Missing {
		t.Fatalf("unexpected tracked status for pathA: %#v", trackedByPath[pathA])
	}
	if trackedByPath[pathB].PrevDigest != missingCID || trackedByPath[pathB].BackupPresent || trackedByPath[pathB].Drifted || trackedByPath[pathB].Missing {
		t.Fatalf("unexpected tracked status for pathB: %#v", trackedByPath[pathB])
	}
	if trackedByPath[pathC].PrevDigest != "" || trackedByPath[pathC].BackupPresent || trackedByPath[pathC].Drifted || trackedByPath[pathC].Missing {
		t.Fatalf("unexpected tracked status for pathC: %#v", trackedByPath[pathC])
	}

	refsByDigest := make(map[string]BackupRefStatus, len(status.BackupRefs))
	for _, ref := range status.BackupRefs {
		refsByDigest[ref.Digest] = ref
	}
	if ref, ok := refsByDigest[presentCID]; !ok || !ref.Present || len(ref.Paths) != 1 || ref.Paths[0] != pathA {
		t.Fatalf("unexpected backup ref for present CID: %#v", ref)
	}
	if ref, ok := refsByDigest[missingCID]; !ok || ref.Present || len(ref.Paths) != 1 || ref.Paths[0] != pathB {
		t.Fatalf("unexpected backup ref for missing CID: %#v", ref)
	}

	if len(status.OrphanedBackups) != 1 || status.OrphanedBackups[0] != orphanCID {
		t.Fatalf("unexpected orphaned backups: %#v", status.OrphanedBackups)
	}
}

func TestStatusDetectsDriftAndMissingTrackedObjects(t *testing.T) {
	t.Parallel()

	store := testInstalledStore(t)
	trackedRoot := t.TempDir()

	okPath := filepath.Join(trackedRoot, "ok")
	modifiedPath := filepath.Join(trackedRoot, "modified")
	missingPath := filepath.Join(trackedRoot, "missing")

	if err := os.WriteFile(okPath, []byte("ok"), 0o644); err != nil {
		t.Fatalf("write ok path: %v", err)
	}
	if err := os.WriteFile(modifiedPath, []byte("old"), 0o644); err != nil {
		t.Fatalf("write modified path: %v", err)
	}
	if err := os.WriteFile(missingPath, []byte("gone"), 0o644); err != nil {
		t.Fatalf("write missing path: %v", err)
	}

	okDigest, err := digest.ForPath(okPath)
	if err != nil {
		t.Fatalf("digest ok path: %v", err)
	}
	modifiedDigest, err := digest.ForPath(modifiedPath)
	if err != nil {
		t.Fatalf("digest modified path: %v", err)
	}
	missingDigest, err := digest.ForPath(missingPath)
	if err != nil {
		t.Fatalf("digest missing path: %v", err)
	}

	if err := os.WriteFile(modifiedPath, []byte("new"), 0o644); err != nil {
		t.Fatalf("rewrite modified path: %v", err)
	}
	if err := os.Remove(missingPath); err != nil {
		t.Fatalf("remove missing path: %v", err)
	}

	lck := DefaultLock()
	lck.Manifest.State = "loaded"
	lck.Files = []lock.File{
		{Path: okPath, Curr: lock.Object{Path: okPath, Digest: okDigest.String()}},
		{Path: modifiedPath, Curr: lock.Object{Path: modifiedPath, Digest: modifiedDigest.String()}},
		{Path: missingPath, Curr: lock.Object{Path: missingPath, Digest: missingDigest.String()}},
	}
	if err := store.SaveLock(lck); err != nil {
		t.Fatalf("SaveLock returned error: %v", err)
	}

	status, err := store.Status()
	if err != nil {
		t.Fatalf("Status returned error: %v", err)
	}

	trackedByPath := make(map[string]TrackedStatus, len(status.Tracked))
	for _, tracked := range status.Tracked {
		trackedByPath[tracked.Path] = tracked
	}

	if trackedByPath[okPath].Drifted || trackedByPath[okPath].Missing {
		t.Fatalf("ok path should not be drifted: %#v", trackedByPath[okPath])
	}
	if !trackedByPath[modifiedPath].Drifted || trackedByPath[modifiedPath].Missing {
		t.Fatalf("modified path should be marked modified: %#v", trackedByPath[modifiedPath])
	}
	if !trackedByPath[missingPath].Drifted || !trackedByPath[missingPath].Missing {
		t.Fatalf("missing path should be marked missing: %#v", trackedByPath[missingPath])
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

	first, err := store.Load(alphaSource, Options{})
	if err != nil {
		t.Fatalf("Load alpha returned error: %v", err)
	}
	if first.SourceName != "alpha" {
		t.Fatalf("first SourceName = %q, want alpha", first.SourceName)
	}
	if first.UnloadedSourceName != "" || first.UnloadedTrackedCount != 0 {
		t.Fatalf("first unload details = (%q, %d), want empty", first.UnloadedSourceName, first.UnloadedTrackedCount)
	}

	second, err := store.Load(betaSource, Options{})
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

	if _, err := store.Load(source, Options{}); err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	res, err := store.Unload(Options{})
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

func TestUnloadWithDiscardChangesRemovesModifiedManagedFiles(t *testing.T) {
	t.Parallel()

	store := testInstalledStore(t)
	targetRoot := t.TempDir()
	managedPath := filepath.Join(targetRoot, "managed")

	sourceDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(sourceDir, "source.txt"), []byte("initial"), 0o644); err != nil {
		t.Fatalf("write source file: %v", err)
	}
	manifestContent := fmt.Sprintf(`[tohru]
version = "%s"

[source]
name = "discard-test"
description = "discard test"

[[file]]
source = "source.txt"
dest = %q
`, version.Version, managedPath)
	if err := os.WriteFile(filepath.Join(sourceDir, "tohru.toml"), []byte(manifestContent), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	if _, err := store.Load(sourceDir, Options{}); err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if err := os.WriteFile(managedPath, []byte("modified"), 0o644); err != nil {
		t.Fatalf("modify managed file: %v", err)
	}

	if _, err := store.Unload(Options{}); err == nil || !strings.Contains(err.Error(), "managed path was modified") {
		t.Fatalf("expected modified-path error without discard-changes, got: %v", err)
	}

	res, err := store.Unload(Options{DiscardChanges: true})
	if err != nil {
		t.Fatalf("UnloadWithOptions returned error: %v", err)
	}
	if res.RemovedCount != 1 {
		t.Fatalf("UnloadWithOptions removed %d object(s), want 1", res.RemovedCount)
	}
	if _, err := os.Stat(managedPath); !os.IsNotExist(err) {
		t.Fatalf("managed path should be removed, stat err=%v", err)
	}
}

func TestUnloadWithDiscardChangesStillFailsForMissingManagedPath(t *testing.T) {
	t.Parallel()

	store := testInstalledStore(t)
	targetRoot := t.TempDir()
	managedPath := filepath.Join(targetRoot, "managed")
	sourceDir := writeSourceManifestWithFile(t, "missing-path-test", managedPath, "source.txt", "value")

	if _, err := store.Load(sourceDir, Options{}); err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if err := os.Remove(managedPath); err != nil {
		t.Fatalf("remove managed path: %v", err)
	}

	_, err := store.Unload(Options{DiscardChanges: true})
	if err == nil {
		t.Fatal("expected unload to fail for missing managed path without force")
	}
	if !strings.Contains(err.Error(), "managed path missing") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateReturnsOperationSummary(t *testing.T) {
	t.Parallel()

	store := Store{Root: t.TempDir()}
	sourceDir := t.TempDir()
	destinationRoot := t.TempDir()

	if err := os.WriteFile(filepath.Join(sourceDir, "link-target"), []byte("link"), 0o644); err != nil {
		t.Fatalf("write link target: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sourceDir, "file-source"), []byte("file"), 0o644); err != nil {
		t.Fatalf("write file source: %v", err)
	}

	manifestContent := fmt.Sprintf(`[tohru]
version = "%s"

[source]
name = "validate-source"
description = "validate source"

[[link]]
to = "link-target"
from = %q

[[file]]
source = "file-source"
dest = %q

[[dir]]
path = %q
`, version.Version, filepath.Join(destinationRoot, "link"), filepath.Join(destinationRoot, "file"), filepath.Join(destinationRoot, "dir"))
	if err := os.WriteFile(filepath.Join(sourceDir, "tohru.toml"), []byte(manifestContent), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	res, err := store.Validate(sourceDir)
	if err != nil {
		t.Fatalf("Validate returned error: %v", err)
	}
	if res.SourceDir != sourceDir {
		t.Fatalf("Validate SourceDir = %q, want %q", res.SourceDir, sourceDir)
	}
	if res.SourceName != "validate-source" {
		t.Fatalf("Validate SourceName = %q, want validate-source", res.SourceName)
	}
	if res.OpCount != 3 || res.LinkCount != 1 || res.FileCount != 1 || res.DirCount != 1 {
		t.Fatalf("unexpected Validate counts: %#v", res)
	}
	if !strings.HasSuffix(res.ImportTree.Path, string(filepath.Separator)+"tohru.toml") {
		t.Fatalf("unexpected Validate import tree root path: %q", res.ImportTree.Path)
	}
	if len(res.ImportTree.Imports) != 0 {
		t.Fatalf("unexpected Validate import tree children: %#v", res.ImportTree.Imports)
	}
}

func TestValidateWithoutArgumentUsesLoadedSource(t *testing.T) {
	t.Parallel()

	store := testInstalledStore(t)
	targetRoot := t.TempDir()
	sourceDir := writeSourceManifest(t, "loaded-source", filepath.Join(targetRoot, "managed"))

	if _, err := store.Load(sourceDir, Options{}); err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	res, err := store.Validate("")
	if err != nil {
		t.Fatalf("Validate returned error: %v", err)
	}
	if res.SourceDir != sourceDir {
		t.Fatalf("Validate SourceDir = %q, want %q", res.SourceDir, sourceDir)
	}
	if res.SourceName != "loaded-source" {
		t.Fatalf("Validate SourceName = %q, want loaded-source", res.SourceName)
	}
}

func TestValidateWithoutArgumentFailsWhenNoLoadedSource(t *testing.T) {
	t.Parallel()

	store := Store{Root: t.TempDir()}

	if _, err := store.Validate(""); err == nil {
		t.Fatal("expected Validate to fail without a source when store is not installed")
	}

	installed := testInstalledStore(t)
	if _, err := installed.Validate(""); err == nil {
		t.Fatal("expected Validate to fail without a source when no source is loaded")
	}
}

func TestValidateRejectsFileEntryWithDirectorySource(t *testing.T) {
	t.Parallel()

	store := Store{Root: t.TempDir()}
	sourceDir := t.TempDir()
	destinationRoot := t.TempDir()

	if err := os.MkdirAll(filepath.Join(sourceDir, "dir-source"), 0o755); err != nil {
		t.Fatalf("mkdir dir source: %v", err)
	}
	manifestContent := fmt.Sprintf(`[tohru]
version = "%s"

[source]
name = "invalid-source"
description = "invalid source"

[[file]]
source = "dir-source"
dest = %q
`, version.Version, filepath.Join(destinationRoot, "managed"))
	if err := os.WriteFile(filepath.Join(sourceDir, "tohru.toml"), []byte(manifestContent), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	_, err := store.Validate(sourceDir)
	if err == nil {
		t.Fatal("expected Validate to reject directory file source")
	}
	if !strings.Contains(err.Error(), "source is not a regular file") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadRollsBackToPreviousSourceOnApplyFailure(t *testing.T) {
	t.Parallel()

	store := testInstalledStore(t)
	targetRoot := t.TempDir()
	managedPath := filepath.Join(targetRoot, "managed")
	secondPath := filepath.Join(targetRoot, "second")

	oldSourceDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(oldSourceDir, "old.txt"), []byte("old"), 0o644); err != nil {
		t.Fatalf("write old source file: %v", err)
	}
	oldManifest := fmt.Sprintf(`[tohru]
version = "%s"

[source]
name = "old-source"
description = "old source"

[[file]]
source = "old.txt"
dest = %q
`, version.Version, managedPath)
	if err := os.WriteFile(filepath.Join(oldSourceDir, "tohru.toml"), []byte(oldManifest), 0o644); err != nil {
		t.Fatalf("write old manifest: %v", err)
	}
	if _, err := store.Load(oldSourceDir, Options{}); err != nil {
		t.Fatalf("initial load failed: %v", err)
	}

	oldContents, err := os.ReadFile(managedPath)
	if err != nil {
		t.Fatalf("read managed path after old load: %v", err)
	}

	newSourceDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(newSourceDir, "new.txt"), []byte("new"), 0o644); err != nil {
		t.Fatalf("write new source file: %v", err)
	}
	newManifest := fmt.Sprintf(`[tohru]
version = "%s"

[source]
name = "new-source"
description = "new source"

[[file]]
source = "new.txt"
dest = %q

[[file]]
source = "missing.txt"
dest = %q
`, version.Version, managedPath, secondPath)
	if err := os.WriteFile(filepath.Join(newSourceDir, "tohru.toml"), []byte(newManifest), 0o644); err != nil {
		t.Fatalf("write new manifest: %v", err)
	}

	_, err = store.Load(newSourceDir, Options{})
	if err == nil {
		t.Fatal("expected load to fail")
	}
	if !strings.Contains(err.Error(), "rolled back to previous state") {
		t.Fatalf("expected rollback error marker, got: %v", err)
	}

	currentContents, err := os.ReadFile(managedPath)
	if err != nil {
		t.Fatalf("read managed path after failed load: %v", err)
	}
	if string(currentContents) != string(oldContents) {
		t.Fatalf("managed path content changed after rollback: got %q want %q", string(currentContents), string(oldContents))
	}
	if _, err := os.Stat(secondPath); !os.IsNotExist(err) {
		t.Fatalf("secondary path should not exist after rollback, stat err=%v", err)
	}

	lck, err := store.LoadLock()
	if err != nil {
		t.Fatalf("LoadLock returned error: %v", err)
	}
	if lck.Manifest.State != "loaded" {
		t.Fatalf("lock state after rollback = %q, want loaded", lck.Manifest.State)
	}
	if lck.Manifest.Name != "old-source" {
		t.Fatalf("lock source name after rollback = %q, want old-source", lck.Manifest.Name)
	}
	if lck.Manifest.Loc != oldSourceDir {
		t.Fatalf("lock source location after rollback = %q, want %q", lck.Manifest.Loc, oldSourceDir)
	}
}

func TestLoadRollsBackArtifactsWhenNoSourceWasPreviouslyLoaded(t *testing.T) {
	t.Parallel()

	store := testInstalledStore(t)
	targetRoot := t.TempDir()
	firstPath := filepath.Join(targetRoot, "nested", "first")
	secondPath := filepath.Join(targetRoot, "nested", "second")

	sourceDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(sourceDir, "ok.txt"), []byte("ok"), 0o644); err != nil {
		t.Fatalf("write source file: %v", err)
	}
	manifest := fmt.Sprintf(`[tohru]
version = "%s"

[source]
name = "broken-source"
description = "broken source"

[[file]]
source = "ok.txt"
dest = %q

[[file]]
source = "missing.txt"
dest = %q
`, version.Version, firstPath, secondPath)
	if err := os.WriteFile(filepath.Join(sourceDir, "tohru.toml"), []byte(manifest), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	_, err := store.Load(sourceDir, Options{})
	if err == nil {
		t.Fatal("expected load to fail")
	}
	if !strings.Contains(err.Error(), "rolled back to previous state") {
		t.Fatalf("expected rollback error marker, got: %v", err)
	}

	if _, err := os.Stat(firstPath); !os.IsNotExist(err) {
		t.Fatalf("first path should be removed by rollback, stat err=%v", err)
	}
	if _, err := os.Stat(secondPath); !os.IsNotExist(err) {
		t.Fatalf("second path should not exist, stat err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(targetRoot, "nested")); !os.IsNotExist(err) {
		t.Fatalf("nested parent dir should be removed by rollback, stat err=%v", err)
	}

	lck, err := store.LoadLock()
	if err != nil {
		t.Fatalf("LoadLock returned error: %v", err)
	}
	if lck.Manifest.State != "unloaded" {
		t.Fatalf("lock state after rollback = %q, want unloaded", lck.Manifest.State)
	}
	if lck.Manifest.Loc != "" {
		t.Fatalf("lock source location after rollback = %q, want empty", lck.Manifest.Loc)
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

func writeSourceManifestWithFile(t *testing.T, name, managedPath, sourceRelPath, sourceContents string) string {
	t.Helper()

	sourceDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(sourceDir, sourceRelPath), []byte(sourceContents), 0o644); err != nil {
		t.Fatalf("write source file: %v", err)
	}

	manifestContent := fmt.Sprintf(`[tohru]
version = "%s"

[source]
name = %q
description = "test source"

[[file]]
source = %q
dest = %q
`, version.Version, name, sourceRelPath, managedPath)

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
