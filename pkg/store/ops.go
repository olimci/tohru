package store

import (
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"

	"github.com/olimci/tohru/pkg/digest"
	"github.com/olimci/tohru/pkg/manifest"
	"github.com/olimci/tohru/pkg/store/config"
	"github.com/olimci/tohru/pkg/store/state"
	"github.com/olimci/tohru/pkg/utils/fileutils"
	"github.com/olimci/tohru/pkg/utils/profileutils"
	"github.com/olimci/tohru/pkg/version"
)

type Options struct {
	Force          bool
	DiscardChanges bool
}

type opKind string

const (
	opLink opKind = "link"
	opFile opKind = "file"
	opDir  opKind = "dir"
)

type op struct {
	Kind   opKind
	Source string
	Dest   string
	Track  bool
}

type rollbackSnapshot struct {
	root    string
	entries []snapshotEntry
}

type snapshotEntry struct {
	Path      string
	Backup    string
	HadObject bool
}

var (
	saveProfilesCache = func(s Store, profiles map[string]state.Profile) error {
		return s.SaveProfiles(profiles)
	}
	pruneBackupsFunc = pruneBackups
)

func (s Store) Load(profile string, opts Options) (LoadResult, error) {
	var result LoadResult
	guard, err := s.Lock()
	if err != nil {
		return result, err
	}
	defer guard.Unlock()

	result, err = s.loadUnlocked(profile, opts)
	return result, err
}

func (s Store) Reload(opts Options) (LoadResult, error) {
	var result LoadResult
	guard, err := s.Lock()
	if err != nil {
		return result, err
	}
	defer guard.Unlock()

	result, err = s.reloadUnlocked(opts)
	return result, err
}

func (s Store) Unload(opts Options) (UnloadResult, error) {
	var result UnloadResult
	guard, err := s.Lock()
	if err != nil {
		return result, err
	}
	defer guard.Unlock()

	result, err = s.unloadUnlocked(opts)
	return result, err
}

func (s Store) Uninstall() error {
	guard, err := s.Lock()
	if err != nil {
		return err
	}
	defer guard.Unlock()

	if !s.IsInstalled() {
		return ErrNotInstalled
	}

	return fileutils.RemovePath(s.Root)
}

func (s Store) Tidy() (TidyResult, error) {
	var result TidyResult
	guard, err := s.Lock()
	if err != nil {
		return result, err
	}
	defer guard.Unlock()

	result, err = s.tidyUnlocked()
	return result, err
}

func (s Store) InstallAndLoad(profile string, opts Options) (LoadResult, error) {
	var result LoadResult
	guard, err := s.Lock()
	if err != nil {
		return result, err
	}
	defer guard.Unlock()

	if s.IsInstalled() {
		return result, ErrAlreadyInstalled
	}

	if _, err := s.installMissing(); err != nil {
		return result, err
	}
	if strings.TrimSpace(profile) == "" {
		return result, nil
	}

	cfg, err := s.LoadConfig()
	if err != nil {
		return result, err
	}

	result, err = s.switchProfile(cfg, profile, opts)
	return result, err
}

func (s Store) UnloadAndUninstall(opts Options) (UnloadResult, error) {
	var result UnloadResult
	guard, err := s.Lock()
	if err != nil {
		return result, err
	}
	defer guard.Unlock()

	result, err = s.unloadUnlocked(opts)
	if err != nil {
		return result, err
	}
	err = fileutils.RemovePath(s.Root)
	return result, err
}

func (s Store) loadUnlocked(profile string, opts Options) (LoadResult, error) {
	if _, err := s.installMissing(); err != nil {
		return LoadResult{}, err
	}

	cfg, err := s.LoadConfig()
	if err != nil {
		return LoadResult{}, err
	}

	return s.switchProfile(cfg, profile, opts)
}

func (s Store) reloadUnlocked(opts Options) (LoadResult, error) {
	if !s.IsInstalled() {
		return LoadResult{}, ErrNotInstalled
	}

	cfg, err := s.LoadConfig()
	if err != nil {
		return LoadResult{}, err
	}

	lck, err := s.LoadState()
	if err != nil {
		return LoadResult{}, err
	}

	if strings.ToLower(lck.Manifest.State) != "loaded" {
		return LoadResult{}, fmt.Errorf("no loaded profile to reload")
	}
	if lck.Manifest.Kind != "local" {
		return LoadResult{}, fmt.Errorf("unsupported profile kind %q", lck.Manifest.Kind)
	}
	if lck.Manifest.Loc == "" {
		return LoadResult{}, fmt.Errorf("loaded profile location is empty")
	}

	return s.switchProfile(cfg, lck.Manifest.Loc, opts)
}

func (s Store) unloadUnlocked(opts Options) (UnloadResult, error) {
	if !s.IsInstalled() {
		return UnloadResult{}, ErrNotInstalled
	}

	cfg, err := s.LoadConfig()
	if err != nil {
		return UnloadResult{}, err
	}

	lck, err := s.LoadState()
	if err != nil {
		return UnloadResult{}, err
	}

	changes := newPathRecorder()
	snapshot, err := takeSnapshot(s, lck.Files)
	if err != nil {
		return UnloadResult{}, err
	}
	defer snapshot.Cleanup()

	rollbackOnErr := func(err error) (UnloadResult, error) {
		if rollbackErr := rollback(s, lck, snapshot, changes.Paths()); rollbackErr != nil {
			return UnloadResult{}, fmt.Errorf("%w (rollback failed: %v)", err, rollbackErr)
		}
		return UnloadResult{}, fmt.Errorf("%w (rolled back to previous state)", err)
	}

	if len(lck.Files) > 0 {
		if err := unloadTracked(s, lck.Files, nil, opts, changes.Add); err != nil {
			return rollbackOnErr(err)
		}
	}
	if err := pruneAutoDirs(lck.Dirs, changes.Add); err != nil {
		return rollbackOnErr(err)
	}

	newLock := DefaultState()
	if err := s.SaveState(newLock); err != nil {
		return rollbackOnErr(err)
	}
	changes.Add(s.StatePath())

	removedBackups := 0
	warnings := make([]string, 0, 1)

	if cfg.Options.Clean {
		removedBackups, err = pruneBackupsFunc(s, newLock.Files, changes.Add)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("backup cleanup failed: %v", err))
		}
	}

	return UnloadResult{
		ProfileName:        profileutils.DisplayName(lck.Manifest.Slug, lck.Manifest.Name, lck.Manifest.Loc),
		RemovedCount:       len(lck.Files),
		RemovedBackupCount: removedBackups,
		ChangedPaths:       changes.Paths(),
		Warnings:           warnings,
	}, nil
}

func (s Store) tidyUnlocked() (TidyResult, error) {
	if !s.IsInstalled() {
		return TidyResult{}, ErrNotInstalled
	}

	lck, err := s.LoadState()
	if err != nil {
		return TidyResult{}, err
	}

	changes := newPathRecorder()
	removed, err := pruneBackupsFunc(s, lck.Files, changes.Add)
	if err != nil {
		return TidyResult{}, err
	}

	return TidyResult{
		RemovedCount: removed,
		ChangedPaths: changes.Paths(),
	}, nil
}

func (s Store) switchProfile(cfg config.Config, profile string, opts Options) (LoadResult, error) {
	oldLock, err := s.LoadState()
	if err != nil {
		return LoadResult{}, err
	}

	loadedProfiles, err := s.LoadProfiles()
	if err != nil {
		return LoadResult{}, err
	}

	target, err := resolveProfile(profile, loadedProfiles)
	if err != nil {
		return LoadResult{}, err
	}

	m, profileDir, err := manifest.Load(target)
	if err != nil {
		return LoadResult{}, err
	}
	if err := version.EnsureCompatible(m.Tohru.Version); err != nil {
		return LoadResult{}, fmt.Errorf("unsupported profile version %q: %w", m.Tohru.Version, err)
	}
	slug, err := profileutils.ValidateSlug(m.Profile.Slug, "profile.slug", true)
	if err != nil {
		return LoadResult{}, err
	}
	m.Profile.Slug = slug

	ops, err := plan(m, profileDir)
	if err != nil {
		return LoadResult{}, err
	}
	changes := newPathRecorder()
	profileCache := maps.Clone(loadedProfiles)

	oldByPath := make(map[string]state.File, len(oldLock.Files))
	for _, f := range oldLock.Files {
		oldByPath[f.Path] = f
	}

	occupiedByNew := make(map[string]struct{}, len(ops))
	for _, op := range ops {
		occupiedByNew[op.Dest] = struct{}{}
	}

	snapshot, err := takeSnapshot(s, oldLock.Files)
	if err != nil {
		return LoadResult{}, err
	}
	defer snapshot.Cleanup()

	rollbackOnErr := func(err error) (LoadResult, error) {
		if rollbackErr := rollback(s, oldLock, snapshot, changes.Paths()); rollbackErr != nil {
			return LoadResult{}, fmt.Errorf("%w (rollback failed: %v)", err, rollbackErr)
		}
		return LoadResult{}, fmt.Errorf("%w (rolled back to previous state)", err)
	}

	if err := unloadTracked(s, oldLock.Files, occupiedByNew, opts, changes.Add); err != nil {
		return rollbackOnErr(err)
	}
	if err := pruneAutoDirs(oldLock.Dirs, changes.Add); err != nil {
		return rollbackOnErr(err)
	}

	// Persist unloaded state before loading the new profile so failures don't
	// leave state metadata claiming the old profile is active.
	unloaded := DefaultState()
	if err := s.SaveState(unloaded); err != nil {
		return rollbackOnErr(err)
	}
	changes.Add(s.StatePath())

	tracked, autoDirs, err := apply(s, cfg, ops, oldByPath, opts.Force, changes.Add)
	if err != nil {
		return rollbackOnErr(err)
	}

	newLock := DefaultState()
	newLock.Manifest.State = "loaded"
	newLock.Manifest.Kind = "local"
	newLock.Manifest.Loc = profileDir
	newLock.Manifest.Slug = m.Profile.Slug
	newLock.Manifest.Name = strings.TrimSpace(m.Profile.Name)
	newLock.Files = tracked
	newLock.Dirs = autoDirs

	if err := s.SaveState(newLock); err != nil {
		return rollbackOnErr(err)
	}
	changes.Add(s.StatePath())

	warnings := make([]string, 0, 2)

	cacheProfile(profileCache, m.Profile, profileDir)
	if err := saveProfilesCache(s, profileCache); err != nil {
		warnings = append(warnings, fmt.Sprintf("profile cache update failed: %v", err))
	} else {
		changes.Add(s.ProfilesFilePath())
	}

	removedBackups := 0

	if cfg.Options.Clean {
		removedBackups, err = pruneBackupsFunc(s, newLock.Files, changes.Add)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("backup cleanup failed: %v", err))
		}
	}

	return LoadResult{
		ProfileDir:           profileDir,
		ProfileName:          profileutils.DisplayName(m.Profile.Slug, m.Profile.Name, profileDir),
		TrackedCount:         len(tracked),
		UnloadedProfileName:  profileutils.DisplayName(oldLock.Manifest.Slug, oldLock.Manifest.Name, oldLock.Manifest.Loc),
		UnloadedTrackedCount: len(oldLock.Files),
		RemovedBackupCount:   removedBackups,
		ChangedPaths:         changes.Paths(),
		Warnings:             warnings,
	}, nil
}

func plan(m manifest.Manifest, sourceDir string) ([]op, error) {
	compiled := m.Plan
	ops := make([]op, 0, len(compiled.Links)+len(compiled.Files)+len(compiled.Dirs))
	seenDest := make(map[string]struct{}, len(compiled.Links)+len(compiled.Files)+len(compiled.Dirs))

	add := func(op op) error {
		if _, ok := seenDest[op.Dest]; ok {
			return fmt.Errorf("duplicate destination in manifest: %s", op.Dest)
		}
		seenDest[op.Dest] = struct{}{}
		ops = append(ops, op)
		return nil
	}

	for _, l := range compiled.Links {
		src, err := resolvePath(sourceDir, l.To)
		if err != nil {
			return nil, fmt.Errorf("link.to %q: %w", l.To, err)
		}
		dest, err := fileutils.AbsPath(l.From)
		if err != nil {
			return nil, fmt.Errorf("link.from %q: %w", l.From, err)
		}

		if err := add(op{
			Kind:   opLink,
			Source: src,
			Dest:   dest,
			Track:  true,
		}); err != nil {
			return nil, err
		}
	}

	for _, f := range compiled.Files {
		src, err := resolvePath(sourceDir, f.Source)
		if err != nil {
			return nil, fmt.Errorf("file.source %q: %w", f.Source, err)
		}
		dest, err := fileutils.AbsPath(f.Dest)
		if err != nil {
			return nil, fmt.Errorf("file.dest %q: %w", f.Dest, err)
		}

		if err := add(op{
			Kind:   opFile,
			Source: src,
			Dest:   dest,
			Track:  f.Tracked == nil || *f.Tracked,
		}); err != nil {
			return nil, err
		}
	}

	for _, d := range compiled.Dirs {
		dest, err := fileutils.AbsPath(d.Path)
		if err != nil {
			return nil, fmt.Errorf("dir.path %q: %w", d.Path, err)
		}

		if err := add(op{
			Kind:  opDir,
			Dest:  dest,
			Track: d.Tracked == nil || *d.Tracked,
		}); err != nil {
			return nil, err
		}
	}

	return ops, nil
}

func apply(store Store, cfg config.Config, ops []op, oldByPath map[string]state.File, force bool, recordPath func(string)) ([]state.File, []state.Dir, error) {
	tracked := make([]state.File, 0, len(ops))
	autoDirSet := make(map[string]struct{}, 16)

	for _, op := range ops {
		var prev *state.Object
		if old, ok := oldByPath[op.Dest]; ok {
			prev = old.Previous
		}

		prevAfterPrepare, err := prepare(store, cfg, op, prev, force, recordPath)
		if err != nil {
			return nil, nil, fmt.Errorf("%s %s: %w", op.Kind, op.Dest, err)
		}

		createdParents, err := makeParents(op.Dest)
		if err != nil {
			return nil, nil, err
		}
		for _, dir := range createdParents {
			autoDirSet[dir] = struct{}{}
			recordPath(dir)
		}

		switch op.Kind {
		case opLink:
			if err := os.Symlink(op.Source, op.Dest); err != nil {
				return nil, nil, fmt.Errorf("create symlink %s -> %s: %w", op.Dest, op.Source, err)
			}
			recordPath(op.Dest)
		case opFile:
			info, err := os.Lstat(op.Source)
			if err != nil {
				return nil, nil, fmt.Errorf("stat manifest source %s: %w", op.Source, err)
			}
			if info.IsDir() && info.Mode()&os.ModeSymlink == 0 {
				return nil, nil, fmt.Errorf("manifest file source is a directory: %s", op.Source)
			}
			if err := fileutils.CopyPath(op.Source, op.Dest); err != nil {
				return nil, nil, err
			}
			recordPath(op.Dest)
		case opDir:
			if err := os.MkdirAll(op.Dest, 0o755); err != nil {
				return nil, nil, fmt.Errorf("create directory %s: %w", op.Dest, err)
			}
			recordPath(op.Dest)
		default:
			return nil, nil, fmt.Errorf("unsupported operation kind %q", op.Kind)
		}

		if !op.Track {
			continue
		}

		curr, err := snapshot(op.Dest)
		if err != nil {
			return nil, nil, fmt.Errorf("snapshot tracked path %s: %w", op.Dest, err)
		}

		tracked = append(tracked, state.File{
			Path:     op.Dest,
			Current:  curr,
			Previous: prevAfterPrepare,
		})
	}

	autoDirs := make([]state.Dir, 0, len(autoDirSet))
	for path := range autoDirSet {
		autoDirs = append(autoDirs, state.Dir{Path: path})
	}
	slices.SortFunc(autoDirs, func(a, b state.Dir) int {
		return strings.Compare(a.Path, b.Path)
	})

	return tracked, autoDirs, nil
}

func prepare(store Store, cfg config.Config, op op, prev *state.Object, force bool, recordPath func(string)) (*state.Object, error) {
	current, exists, err := maybeSnapshot(op.Dest)
	if err != nil {
		return nil, err
	}
	if !exists {
		return prev, nil
	}

	if op.Kind == opDir {
		currentDigest, parseErr := digest.Parse(current.Digest)
		if parseErr != nil {
			return nil, fmt.Errorf("parse digest for %s: %w", op.Dest, parseErr)
		}
		if currentDigest.Kind == digest.KindDir {
			if !op.Track {
				return prev, nil
			}
			return nil, fmt.Errorf("tracked dir destination already exists: %s", op.Dest)
		}
		if op.Track {
			return nil, fmt.Errorf("tracked dir destination exists and is not a directory: %s", op.Dest)
		}
	}

	if !op.Track {
		if !force {
			return nil, fmt.Errorf("destination exists (would clobber), use --force to overwrite")
		}
		if err := fileutils.RemovePath(op.Dest); err != nil {
			return nil, err
		}
		recordPath(op.Dest)
		return prev, nil
	}

	if prev == nil && cfg.Options.Backup {
		storedPrev, err := storeBackup(store, current, recordPath)
		if err != nil {
			return nil, err
		}
		if err := fileutils.RemovePath(op.Dest); err != nil {
			return nil, err
		}
		recordPath(op.Dest)
		return storedPrev, nil
	}

	if !force {
		if prev == nil && !cfg.Options.Backup {
			return nil, fmt.Errorf("destination exists and options.backup=false, refusing to clobber without --force")
		}
		return nil, fmt.Errorf("destination exists (would clobber), use --force to overwrite")
	}

	if err := fileutils.RemovePath(op.Dest); err != nil {
		return nil, err
	}
	recordPath(op.Dest)

	return prev, nil
}

func unloadTracked(store Store, files []state.File, occupiedByNew map[string]struct{}, opts Options, recordPath func(string)) error {
	managedFiles := slices.Clone(files)
	slices.SortFunc(managedFiles, func(a, b state.File) int {
		return -fileutils.CompareDepth(a.Path, b.Path)
	})

	for _, managed := range managedFiles {
		if err := removeManaged(managed, opts, recordPath); err != nil {
			return err
		}

		if managed.Previous != nil && managed.Previous.Digest != "" {
			if _, stillOccupied := occupiedByNew[managed.Path]; stillOccupied {
				continue
			}
			if err := restoreBackup(store, managed.Previous, managed.Path, opts.Force, recordPath); err != nil {
				return err
			}
		}
	}

	return nil
}

func removeManaged(managed state.File, opts Options, recordPath func(string)) error {
	path := strings.TrimSpace(managed.Path)
	if path == "" {
		return nil
	}

	current, exists, err := maybeSnapshot(path)
	if err != nil {
		return fmt.Errorf("check managed path %s: %w", path, err)
	}
	if !exists {
		if opts.Force {
			return nil
		}
		return fmt.Errorf("managed path missing: %s", path)
	}

	expected, err := digest.Parse(managed.Current.Digest)
	if err != nil {
		return fmt.Errorf("invalid digest for managed path %s: %w", path, err)
	}
	actual, err := digest.Parse(current.Digest)
	if err != nil {
		return fmt.Errorf("invalid current digest for managed path %s: %w", path, err)
	}
	if !(opts.Force || opts.DiscardChanges) && !expected.IsZero() && expected.String() != actual.String() {
		return fmt.Errorf("managed path was modified: %s", path)
	}

	if err := fileutils.RemovePath(path); err != nil {
		return fmt.Errorf("remove managed path %s: %w", path, err)
	}
	recordPath(path)

	return nil
}

func storeBackup(store Store, object state.Object, recordPath func(string)) (*state.Object, error) {
	d, err := digest.Parse(object.Digest)
	if err != nil {
		return nil, fmt.Errorf("parse backup digest for %s: %w", object.Path, err)
	}
	if d.IsZero() {
		return nil, fmt.Errorf("cannot backup object %s with empty digest", object.Path)
	}

	cid := d.String()
	objectPath := backupPath(store, cid)

	existingBackup, exists, err := maybeSnapshot(objectPath)
	if err != nil {
		return nil, fmt.Errorf("check backup object at %s: %w", objectPath, err)
	}
	if exists {
		if existingBackup.Digest != d.String() {
			return nil, fmt.Errorf("backup collision for CID %s at %s", cid, objectPath)
		}
		return &state.Object{Path: objectPath, Digest: d.String()}, nil
	}

	if err := os.MkdirAll(filepath.Dir(objectPath), 0o755); err != nil {
		return nil, fmt.Errorf("create backup directory for %s: %w", objectPath, err)
	}
	if err := fileutils.CopyPath(object.Path, objectPath); err != nil {
		return nil, fmt.Errorf("backup %s into %s: %w", object.Path, objectPath, err)
	}
	recordPath(objectPath)

	written, err := snapshot(objectPath)
	if err != nil {
		return nil, fmt.Errorf("snapshot backup object %s: %w", objectPath, err)
	}
	if written.Digest != d.String() {
		_ = fileutils.RemovePath(objectPath)
		return nil, fmt.Errorf("backup digest mismatch for %s", objectPath)
	}

	return &state.Object{Path: objectPath, Digest: d.String()}, nil
}

func restoreBackup(store Store, prev *state.Object, destination string, force bool, recordPath func(string)) error {
	if prev == nil {
		return nil
	}

	path := strings.TrimSpace(prev.Path)
	if path == "" {
		d, err := digest.Parse(prev.Digest)
		if err != nil {
			return fmt.Errorf("parse previous digest for %s: %w", destination, err)
		}
		if d.IsZero() {
			return nil
		}
		path = backupPath(store, d.String())
	}

	backup, exists, err := maybeSnapshot(path)
	if err != nil {
		return fmt.Errorf("check backup object %s: %w", path, err)
	}
	if !exists {
		if force {
			return nil
		}
		return fmt.Errorf("missing backup object %s for %s", path, destination)
	}

	if prev.Digest != "" && backup.Digest != prev.Digest {
		if !force {
			return fmt.Errorf("backup digest mismatch for %s", path)
		}
	}

	_, destinationExists, err := maybeSnapshot(destination)
	if err != nil {
		return fmt.Errorf("check restore destination %s: %w", destination, err)
	}
	if destinationExists {
		if !force {
			return fmt.Errorf("restore destination exists for %s", destination)
		}
		if err := fileutils.RemovePath(destination); err != nil {
			return fmt.Errorf("remove restore destination %s: %w", destination, err)
		}
		recordPath(destination)
	}

	if err := fileutils.CopyPath(path, destination); err != nil {
		return fmt.Errorf("restore backup %s to %s: %w", path, destination, err)
	}
	recordPath(destination)

	return nil
}

func pruneBackups(store Store, tracked []state.File, recordPath func(string)) (int, error) {
	referenced := make(map[string]struct{}, len(tracked))
	for _, f := range tracked {
		if f.Previous == nil || f.Previous.Digest == "" {
			continue
		}
		d, err := digest.Parse(f.Previous.Digest)
		if err != nil {
			return 0, fmt.Errorf("parse previous digest for %s: %w", f.Path, err)
		}
		if d.IsZero() {
			continue
		}
		referenced[d.String()] = struct{}{}
	}

	entries, err := os.ReadDir(store.BackupsPath())
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return 0, nil
		}
		return 0, fmt.Errorf("read backups directory %s: %w", store.BackupsPath(), err)
	}

	var removed int
	for _, entry := range entries {
		cid := entry.Name()
		if _, keep := referenced[cid]; keep {
			continue
		}

		path := filepath.Join(store.BackupsPath(), cid)
		if err := fileutils.RemovePath(path); err != nil {
			return 0, fmt.Errorf("remove unreferenced backup %s: %w", path, err)
		}
		recordPath(path)
		removed++
	}

	return removed, nil
}

func backupPath(store Store, cid string) string {
	return filepath.Join(store.BackupsPath(), cid, "object")
}

func takeSnapshot(store Store, files []state.File) (rollbackSnapshot, error) {
	root, err := os.MkdirTemp(store.Root, "switch-rollback-")
	if err != nil {
		return rollbackSnapshot{}, fmt.Errorf("create rollback snapshot directory: %w", err)
	}

	snapshot := rollbackSnapshot{
		root:    root,
		entries: make([]snapshotEntry, 0, len(files)),
	}

	seen := make(map[string]struct{}, len(files))
	for i, tracked := range files {
		path := strings.TrimSpace(tracked.Path)
		if path == "" {
			continue
		}
		if _, exists := seen[path]; exists {
			continue
		}
		seen[path] = struct{}{}

		entry := snapshotEntry{Path: path}
		_, exists, statErr := maybeSnapshot(path)
		if statErr != nil {
			_ = snapshot.Cleanup()
			return rollbackSnapshot{}, fmt.Errorf("snapshot managed path %s: %w", path, statErr)
		}
		if !exists {
			snapshot.entries = append(snapshot.entries, entry)
			continue
		}

		backupPath := filepath.Join(root, fmt.Sprintf("%06d", i), "object")
		if err := os.MkdirAll(filepath.Dir(backupPath), 0o755); err != nil {
			_ = snapshot.Cleanup()
			return rollbackSnapshot{}, fmt.Errorf("create rollback snapshot parent for %s: %w", backupPath, err)
		}
		if err := fileutils.CopyPath(path, backupPath); err != nil {
			_ = snapshot.Cleanup()
			return rollbackSnapshot{}, fmt.Errorf("copy managed path %s into rollback snapshot: %w", path, err)
		}

		entry.Backup = backupPath
		entry.HadObject = true
		snapshot.entries = append(snapshot.entries, entry)
	}

	return snapshot, nil
}

func (s rollbackSnapshot) Cleanup() error {
	if strings.TrimSpace(s.root) == "" {
		return nil
	}
	return fileutils.RemovePath(s.root)
}

func rollback(store Store, oldLock state.State, snapshot rollbackSnapshot, changedPaths []string) error {
	for _, path := range fileutils.SortByDepth(changedPaths, true) {
		if path == store.StatePath() {
			continue
		}
		if err := fileutils.RemovePath(path); err != nil {
			return fmt.Errorf("rollback remove changed path %s: %w", path, err)
		}
	}

	removeTargets := make([]string, 0, len(snapshot.entries))
	for _, entry := range snapshot.entries {
		removeTargets = append(removeTargets, entry.Path)
	}
	for _, path := range fileutils.SortByDepth(removeTargets, true) {
		if err := fileutils.RemovePath(path); err != nil {
			return fmt.Errorf("rollback clear managed path %s: %w", path, err)
		}
	}

	restoreEntries := slices.Clone(snapshot.entries)
	slices.SortFunc(restoreEntries, func(a, b snapshotEntry) int {
		return fileutils.CompareDepth(a.Path, b.Path)
	})

	for _, entry := range restoreEntries {
		if !entry.HadObject {
			continue
		}
		if err := fileutils.CopyPath(entry.Backup, entry.Path); err != nil {
			return fmt.Errorf("rollback restore managed path %s: %w", entry.Path, err)
		}
	}

	if err := store.SaveState(oldLock); err != nil {
		return fmt.Errorf("rollback restore lock: %w", err)
	}

	return nil
}

type pathRecorder struct {
	seen  map[string]struct{}
	paths []string
}

func newPathRecorder() *pathRecorder {
	return &pathRecorder{
		seen: make(map[string]struct{}, 16),
	}
}

func (r *pathRecorder) Add(path string) {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return
	}
	if _, exists := r.seen[trimmed]; exists {
		return
	}
	r.seen[trimmed] = struct{}{}
	r.paths = append(r.paths, trimmed)
}

func (r *pathRecorder) Paths() []string {
	return slices.Clone(r.paths)
}

func resolveProfile(input string, cache map[string]state.Profile) (string, error) {
	ref := strings.TrimSpace(input)
	if ref == "" {
		return "", fmt.Errorf("profile reference is empty")
	}

	expanded := fileutils.ExpandHome(ref)
	if _, err := os.Stat(expanded); err == nil {
		return expanded, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("stat profile reference %q: %w", ref, err)
	}

	slug := profileutils.NormalizeSlug(ref)
	if cached, ok := cache[slug]; ok {
		if loc := strings.TrimSpace(cached.Loc); loc != "" {
			return loc, nil
		}
	}

	return "", fmt.Errorf("profile %q not found as a path and not found in cached profiles", ref)
}

func cacheProfile(cache map[string]state.Profile, profile manifest.Profile, loc string) {
	slug := profileutils.NormalizeSlug(profile.Slug)
	if slug == "" {
		return
	}
	cache[slug] = state.Profile{
		Slug: slug,
		Name: strings.TrimSpace(profile.Name),
		Loc:  strings.TrimSpace(loc),
	}
}

func resolvePath(sourceDir, raw string) (string, error) {
	path := strings.TrimSpace(raw)
	if path == "" {
		return "", fmt.Errorf("path is empty")
	}

	path = fileutils.ExpandHome(path)
	root := filepath.Clean(sourceDir)

	var resolved string
	if filepath.IsAbs(path) {
		resolved = filepath.Clean(path)
	} else {
		resolved = filepath.Clean(filepath.Join(root, path))
	}

	rel, err := filepath.Rel(root, resolved)
	if err != nil {
		return "", fmt.Errorf("compute path relative to source root %s: %w", root, err)
	}

	if fileutils.Escapes(rel) {
		return "", fmt.Errorf("path escapes source root %s: %s", root, resolved)
	}

	return resolved, nil
}

func makeParents(path string) ([]string, error) {
	parent := filepath.Clean(filepath.Dir(path))
	if parent == "." || parent == string(filepath.Separator) {
		return nil, nil
	}

	missing := make([]string, 0, 4)
	cur := parent
	for {
		info, err := os.Stat(cur)
		if err == nil {
			if !info.IsDir() {
				return nil, fmt.Errorf("path exists and is not a directory: %s", cur)
			}
			break
		}
		if !errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("stat parent directory %s: %w", cur, err)
		}

		missing = append(missing, cur)
		next := filepath.Dir(cur)
		if next == cur || next == "." {
			break
		}
		cur = next
	}

	created := make([]string, 0, len(missing))
	for i := len(missing) - 1; i >= 0; i-- {
		dir := missing[i]
		if err := os.Mkdir(dir, 0o755); err != nil {
			if errors.Is(err, os.ErrExist) {
				info, statErr := os.Stat(dir)
				if statErr == nil && info.IsDir() {
					continue
				}
			}
			return nil, fmt.Errorf("create parent directory %s: %w", dir, err)
		}
		created = append(created, dir)
	}

	return created, nil
}

func pruneAutoDirs(dirs []state.Dir, recordPath func(string)) error {
	ordered := slices.Clone(dirs)
	slices.SortFunc(ordered, func(a, b state.Dir) int {
		return -fileutils.CompareDepth(a.Path, b.Path)
	})

	for _, d := range ordered {
		path := strings.TrimSpace(d.Path)
		if path == "" {
			continue
		}

		clean := filepath.Clean(path)
		if clean == "." || clean == string(filepath.Separator) {
			continue
		}

		info, err := os.Lstat(clean)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return fmt.Errorf("stat auto dir %s: %w", clean, err)
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			continue
		}

		if err := os.Remove(clean); err != nil {
			if errors.Is(err, os.ErrNotExist) || errors.Is(err, os.ErrPermission) {
				continue
			}
			var pathErr *os.PathError
			if errors.As(err, &pathErr) {
				if errors.Is(pathErr.Err, syscall.ENOTEMPTY) || errors.Is(pathErr.Err, syscall.EEXIST) {
					continue
				}
			}
			return fmt.Errorf("remove auto dir %s: %w", clean, err)
		}
		recordPath(clean)
	}

	return nil
}

func maybeSnapshot(path string) (state.Object, bool, error) {
	obj, err := snapshot(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return state.Object{}, false, nil
		}
		return state.Object{}, false, err
	}
	return obj, true, nil
}

func snapshot(path string) (state.Object, error) {
	d, err := digest.ForPath(path)
	if err != nil {
		return state.Object{}, err
	}

	return state.Object{
		Path:   path,
		Digest: d.String(),
	}, nil
}
