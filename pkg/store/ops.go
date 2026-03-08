package store

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"unicode"

	"github.com/olimci/tohru/pkg/digest"
	"github.com/olimci/tohru/pkg/manifest"
	"github.com/olimci/tohru/pkg/store/config"
	"github.com/olimci/tohru/pkg/store/lock"
	"github.com/olimci/tohru/pkg/utils/fileutils"
	"github.com/olimci/tohru/pkg/version"
)

type Options struct {
	Force          bool
	DiscardChanges bool
}

type manifestOpKind string

const (
	opLink manifestOpKind = "link"
	opFile manifestOpKind = "file"
	opDir  manifestOpKind = "dir"
)

type manifestOp struct {
	Kind   manifestOpKind
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

func (s Store) Load(profile string, opts Options) (LoadResult, error) {
	if err := s.EnsureInstalled(); err != nil {
		return LoadResult{}, err
	}

	cfg, err := s.LoadConfig()
	if err != nil {
		return LoadResult{}, err
	}

	return s.switchProfile(cfg, profile, opts)
}

func (s Store) Reload(opts Options) (LoadResult, error) {
	if !s.IsInstalled() {
		return LoadResult{}, ErrNotInstalled
	}

	cfg, err := s.LoadConfig()
	if err != nil {
		return LoadResult{}, err
	}

	lck, err := s.LoadLock()
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

func (s Store) Unload(opts Options) (UnloadResult, error) {
	if !s.IsInstalled() {
		return UnloadResult{}, ErrNotInstalled
	}

	cfg, err := s.LoadConfig()
	if err != nil {
		return UnloadResult{}, err
	}

	lck, err := s.LoadLock()
	if err != nil {
		return UnloadResult{}, err
	}

	changes := newPathRecorder()

	if len(lck.Files) > 0 {
		if err := unloadTracked(s, lck.Files, nil, opts, changes.Add); err != nil {
			return UnloadResult{}, err
		}
	}
	if err := cleanupAutoDirs(lck.Dirs); err != nil {
		return UnloadResult{}, err
	}

	newLock := DefaultLock()
	if err := s.SaveLock(newLock); err != nil {
		return UnloadResult{}, err
	}
	changes.Add(s.LockPath())

	removedBackups := 0

	if cfg.Options.Clean {
		removedBackups, err = pruneBackups(s, newLock.Files, changes.Add)
		if err != nil {
			return UnloadResult{}, err
		}
	}

	return UnloadResult{
		ProfileName:        displayProfile(lck.Manifest.Slug, lck.Manifest.Name, lck.Manifest.Loc),
		RemovedCount:       len(lck.Files),
		RemovedBackupCount: removedBackups,
		ChangedPaths:       changes.Paths(),
	}, nil
}

func (s Store) Uninstall() error {
	if !s.IsInstalled() {
		return ErrNotInstalled
	}

	return fileutils.RemovePath(s.Root)
}

func (s Store) Tidy() (TidyResult, error) {
	if !s.IsInstalled() {
		return TidyResult{}, ErrNotInstalled
	}

	lck, err := s.LoadLock()
	if err != nil {
		return TidyResult{}, err
	}

	changes := newPathRecorder()
	removed, err := pruneBackups(s, lck.Files, changes.Add)
	if err != nil {
		return TidyResult{}, err
	}

	return TidyResult{
		RemovedCount: removed,
		ChangedPaths: changes.Paths(),
	}, nil
}

func (s Store) switchProfile(cfg config.Config, profile string, opts Options) (LoadResult, error) {
	oldLock, err := s.LoadLock()
	if err != nil {
		return LoadResult{}, err
	}

	loadedProfiles, err := s.LoadProfiles()
	if err != nil {
		return LoadResult{}, err
	}

	target, err := resolveProfileRef(profile, loadedProfiles)
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
	slug, err := parseProfileSlug(m.Profile.Slug)
	if err != nil {
		return LoadResult{}, err
	}
	m.Profile.Slug = slug

	ops, err := planOps(m, profileDir)
	if err != nil {
		return LoadResult{}, err
	}
	changes := newPathRecorder()
	profileCache := cloneProfileCache(loadedProfiles)

	oldByPath := make(map[string]lock.File, len(oldLock.Files))
	for _, f := range oldLock.Files {
		oldByPath[f.Path] = f
	}

	occupiedByNew := make(map[string]struct{}, len(ops))
	for _, op := range ops {
		occupiedByNew[op.Dest] = struct{}{}
	}

	snapshot, err := takeRollbackSnapshot(s, oldLock.Files)
	if err != nil {
		return LoadResult{}, err
	}
	defer func() {
		_ = snapshot.Cleanup()
	}()

	rollbackOnErr := func(err error) (LoadResult, error) {
		if rollbackErr := rollbackSwitch(s, oldLock, snapshot, changes.Paths()); rollbackErr != nil {
			return LoadResult{}, fmt.Errorf("%w (rollback failed: %v)", err, rollbackErr)
		}
		return LoadResult{}, fmt.Errorf("%w (rolled back to previous state)", err)
	}

	if err := unloadTracked(s, oldLock.Files, occupiedByNew, opts, changes.Add); err != nil {
		return rollbackOnErr(err)
	}
	if err := cleanupAutoDirs(oldLock.Dirs); err != nil {
		return rollbackOnErr(err)
	}

	// Persist unloaded state before loading the new profile so failures don't
	// leave lock metadata claiming the old profile is active.
	unloaded := DefaultLock()
	if err := s.SaveLock(unloaded); err != nil {
		return rollbackOnErr(err)
	}
	changes.Add(s.LockPath())

	tracked, autoDirs, err := applyOps(s, cfg, ops, oldByPath, opts.Force, changes.Add)
	if err != nil {
		return rollbackOnErr(err)
	}

	newLock := DefaultLock()
	newLock.Manifest.State = "loaded"
	newLock.Manifest.Kind = "local"
	newLock.Manifest.Loc = profileDir
	newLock.Manifest.Slug = m.Profile.Slug
	newLock.Manifest.Name = strings.TrimSpace(m.Profile.Name)
	newLock.Files = tracked
	newLock.Dirs = autoDirs

	if err := s.SaveLock(newLock); err != nil {
		return rollbackOnErr(err)
	}
	changes.Add(s.LockPath())

	cacheProfile(profileCache, m.Profile, profileDir)
	if err := s.SaveProfiles(profileCache); err != nil {
		return LoadResult{}, fmt.Errorf("save profiles cache: %w", err)
	}
	changes.Add(s.ProfilesFilePath())

	removedBackups := 0

	if cfg.Options.Clean {
		removedBackups, err = pruneBackups(s, newLock.Files, changes.Add)
		if err != nil {
			return LoadResult{}, err
		}
	}

	return LoadResult{
		ProfileDir:           profileDir,
		ProfileName:          displayProfile(m.Profile.Slug, m.Profile.Name, profileDir),
		TrackedCount:         len(tracked),
		UnloadedProfileName:  displayProfile(oldLock.Manifest.Slug, oldLock.Manifest.Name, oldLock.Manifest.Loc),
		UnloadedTrackedCount: len(oldLock.Files),
		RemovedBackupCount:   removedBackups,
		ChangedPaths:         changes.Paths(),
	}, nil
}

func planOps(m manifest.Manifest, sourceDir string) ([]manifestOp, error) {
	resolved := m.Resolved
	ops := make([]manifestOp, 0, len(resolved.Links)+len(resolved.Files)+len(resolved.Dirs))
	seenDest := make(map[string]struct{}, len(resolved.Links)+len(resolved.Files)+len(resolved.Dirs))

	add := func(op manifestOp) error {
		if _, ok := seenDest[op.Dest]; ok {
			return fmt.Errorf("duplicate destination in manifest: %s", op.Dest)
		}
		seenDest[op.Dest] = struct{}{}
		ops = append(ops, op)
		return nil
	}

	for _, l := range resolved.Links {
		src, err := resolveSource(sourceDir, l.To)
		if err != nil {
			return nil, fmt.Errorf("link.to %q: %w", l.To, err)
		}
		dest, err := fileutils.AbsPath(l.From)
		if err != nil {
			return nil, fmt.Errorf("link.from %q: %w", l.From, err)
		}

		if err := add(manifestOp{
			Kind:   opLink,
			Source: src,
			Dest:   dest,
			Track:  true,
		}); err != nil {
			return nil, err
		}
	}

	for _, f := range resolved.Files {
		src, err := resolveSource(sourceDir, f.Source)
		if err != nil {
			return nil, fmt.Errorf("file.source %q: %w", f.Source, err)
		}
		dest, err := fileutils.AbsPath(f.Dest)
		if err != nil {
			return nil, fmt.Errorf("file.dest %q: %w", f.Dest, err)
		}

		if err := add(manifestOp{
			Kind:   opFile,
			Source: src,
			Dest:   dest,
			Track:  f.Tracked == nil || *f.Tracked,
		}); err != nil {
			return nil, err
		}
	}

	for _, d := range resolved.Dirs {
		dest, err := fileutils.AbsPath(d.Path)
		if err != nil {
			return nil, fmt.Errorf("dir.path %q: %w", d.Path, err)
		}

		if err := add(manifestOp{
			Kind:  opDir,
			Dest:  dest,
			Track: d.Tracked == nil || *d.Tracked,
		}); err != nil {
			return nil, err
		}
	}

	return ops, nil
}

func applyOps(store Store, cfg config.Config, ops []manifestOp, oldByPath map[string]lock.File, force bool, recordPath func(string)) ([]lock.File, []lock.Dir, error) {
	if recordPath == nil {
		recordPath = func(string) {}
	}

	tracked := make([]lock.File, 0, len(ops))
	autoDirSet := make(map[string]struct{}, 16)

	for _, op := range ops {
		var prev *lock.Object
		if old, ok := oldByPath[op.Dest]; ok {
			prev = old.Prev
		}

		prevAfterPrepare, err := prepareDest(store, cfg, op, prev, force, recordPath)
		if err != nil {
			return nil, nil, fmt.Errorf("%s %s: %w", op.Kind, op.Dest, err)
		}

		createdParents, err := mkdirParents(op.Dest)
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
			if err := fileutils.CopyFile(op.Source, op.Dest); err != nil {
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

		tracked = append(tracked, lock.File{
			Path: op.Dest,
			Curr: curr,
			Prev: prevAfterPrepare,
		})
	}

	autoDirs := make([]lock.Dir, 0, len(autoDirSet))
	for path := range autoDirSet {
		autoDirs = append(autoDirs, lock.Dir{Path: path})
	}
	sort.Slice(autoDirs, func(i, j int) bool {
		return autoDirs[i].Path < autoDirs[j].Path
	})

	return tracked, autoDirs, nil
}

func prepareDest(store Store, cfg config.Config, op manifestOp, prev *lock.Object, force bool, recordPath func(string)) (*lock.Object, error) {
	if recordPath == nil {
		recordPath = func(string) {}
	}

	current, exists, err := snapshotIfExists(op.Dest)
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
		storedPrev, err := saveBackup(store, current, recordPath)
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

func unloadTracked(store Store, files []lock.File, occupiedByNew map[string]struct{}, opts Options, recordPath func(string)) error {
	for _, managed := range sortRemovalOrder(files) {
		if err := removeTracked(managed, opts, recordPath); err != nil {
			return err
		}

		if managed.Prev != nil && managed.Prev.Digest != "" {
			if _, stillOccupied := occupiedByNew[managed.Path]; stillOccupied {
				continue
			}
			if err := restoreBackup(store, managed.Prev, managed.Path, opts.Force, recordPath); err != nil {
				return err
			}
		}
	}

	return nil
}

func removeTracked(managed lock.File, opts Options, recordPath func(string)) error {
	if recordPath == nil {
		recordPath = func(string) {}
	}

	path := strings.TrimSpace(managed.Path)
	if path == "" {
		return nil
	}

	current, exists, err := snapshotIfExists(path)
	if err != nil {
		return fmt.Errorf("check managed path %s: %w", path, err)
	}
	if !exists {
		if opts.Force {
			return nil
		}
		return fmt.Errorf("managed path missing: %s", path)
	}

	expected, err := digest.Parse(managed.Curr.Digest)
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

func saveBackup(store Store, object lock.Object, recordPath func(string)) (*lock.Object, error) {
	if recordPath == nil {
		recordPath = func(string) {}
	}

	d, err := digest.Parse(object.Digest)
	if err != nil {
		return nil, fmt.Errorf("parse backup digest for %s: %w", object.Path, err)
	}
	if d.IsZero() {
		return nil, fmt.Errorf("cannot backup object %s with empty digest", object.Path)
	}

	cid := d.String()
	objectPath := backupObjPath(store, cid)

	existingBackup, exists, err := snapshotIfExists(objectPath)
	if err != nil {
		return nil, fmt.Errorf("check backup object at %s: %w", objectPath, err)
	}
	if exists {
		if existingBackup.Digest != d.String() {
			return nil, fmt.Errorf("backup collision for CID %s at %s", cid, objectPath)
		}
		return &lock.Object{Path: objectPath, Digest: d.String()}, nil
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

	return &lock.Object{Path: objectPath, Digest: d.String()}, nil
}

func restoreBackup(store Store, prev *lock.Object, destination string, force bool, recordPath func(string)) error {
	if recordPath == nil {
		recordPath = func(string) {}
	}

	if prev == nil {
		return nil
	}

	backupPath := strings.TrimSpace(prev.Path)
	if backupPath == "" {
		d, err := digest.Parse(prev.Digest)
		if err != nil {
			return fmt.Errorf("parse previous digest for %s: %w", destination, err)
		}
		if d.IsZero() {
			return nil
		}
		backupPath = backupObjPath(store, d.String())
	}

	backup, exists, err := snapshotIfExists(backupPath)
	if err != nil {
		return fmt.Errorf("check backup object %s: %w", backupPath, err)
	}
	if !exists {
		if force {
			return nil
		}
		return fmt.Errorf("missing backup object %s for %s", backupPath, destination)
	}

	if prev.Digest != "" && backup.Digest != prev.Digest {
		if !force {
			return fmt.Errorf("backup digest mismatch for %s", backupPath)
		}
	}

	_, destinationExists, err := snapshotIfExists(destination)
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

	if err := fileutils.CopyPath(backupPath, destination); err != nil {
		return fmt.Errorf("restore backup %s to %s: %w", backupPath, destination, err)
	}
	recordPath(destination)

	return nil
}

func pruneBackups(store Store, tracked []lock.File, recordPath func(string)) (int, error) {
	if recordPath == nil {
		recordPath = func(string) {}
	}

	referenced := make(map[string]struct{}, len(tracked))
	for _, f := range tracked {
		if f.Prev == nil || f.Prev.Digest == "" {
			continue
		}
		d, err := digest.Parse(f.Prev.Digest)
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

func backupObjPath(store Store, cid string) string {
	return filepath.Join(store.BackupsPath(), cid, "object")
}

func sortRemovalOrder(files []lock.File) []lock.File {
	sorted := append([]lock.File(nil), files...)
	sort.Slice(sorted, func(i, j int) bool {
		di := fileutils.PathDepth(sorted[i].Path)
		dj := fileutils.PathDepth(sorted[j].Path)
		if di == dj {
			return sorted[i].Path > sorted[j].Path
		}
		return di > dj
	})
	return sorted
}

func takeRollbackSnapshot(store Store, files []lock.File) (rollbackSnapshot, error) {
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
		_, exists, statErr := snapshotIfExists(path)
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

func rollbackSwitch(store Store, oldLock lock.Lock, snapshot rollbackSnapshot, changedPaths []string) error {
	for _, path := range sortPathsByDepth(changedPaths, true) {
		if path == store.LockPath() {
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
	for _, path := range sortPathsByDepth(removeTargets, true) {
		if err := fileutils.RemovePath(path); err != nil {
			return fmt.Errorf("rollback clear managed path %s: %w", path, err)
		}
	}

	restoreEntries := append([]snapshotEntry(nil), snapshot.entries...)
	sort.Slice(restoreEntries, func(i, j int) bool {
		di := fileutils.PathDepth(restoreEntries[i].Path)
		dj := fileutils.PathDepth(restoreEntries[j].Path)
		if di == dj {
			return restoreEntries[i].Path < restoreEntries[j].Path
		}
		return di < dj
	})

	for _, entry := range restoreEntries {
		if !entry.HadObject {
			continue
		}
		if err := fileutils.CopyPath(entry.Backup, entry.Path); err != nil {
			return fmt.Errorf("rollback restore managed path %s: %w", entry.Path, err)
		}
	}

	if err := store.SaveLock(oldLock); err != nil {
		return fmt.Errorf("rollback restore lock: %w", err)
	}

	return nil
}

func sortPathsByDepth(paths []string, descending bool) []string {
	sorted := make([]string, 0, len(paths))
	seen := make(map[string]struct{}, len(paths))
	for _, raw := range paths {
		path := strings.TrimSpace(raw)
		if path == "" {
			continue
		}
		if _, exists := seen[path]; exists {
			continue
		}
		seen[path] = struct{}{}
		sorted = append(sorted, path)
	}

	sort.Slice(sorted, func(i, j int) bool {
		di := fileutils.PathDepth(sorted[i])
		dj := fileutils.PathDepth(sorted[j])
		if di == dj {
			if descending {
				return sorted[i] > sorted[j]
			}
			return sorted[i] < sorted[j]
		}
		if descending {
			return di > dj
		}
		return di < dj
	})

	return sorted
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
	return append([]string(nil), r.paths...)
}

func displayProfile(slug, name, loc string) string {
	if trimmedName := strings.TrimSpace(name); trimmedName != "" {
		return trimmedName
	}
	if trimmedSlug := strings.TrimSpace(slug); trimmedSlug != "" {
		return trimmedSlug
	}
	trimmedLoc := strings.TrimSpace(loc)
	if trimmedLoc == "" {
		return ""
	}
	return filepath.Base(filepath.Clean(trimmedLoc))
}

func resolveProfileRef(input string, cache map[string]lock.Profile) (string, error) {
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

	slug := normalizeSlug(ref)
	if cached, ok := cache[slug]; ok {
		if loc := strings.TrimSpace(cached.Loc); loc != "" {
			return loc, nil
		}
	}

	return "", fmt.Errorf("profile %q not found as a path and not found in cached profiles", ref)
}

func cacheProfile(cache map[string]lock.Profile, profile manifest.Profile, loc string) {
	if cache == nil {
		return
	}
	slug := normalizeSlug(profile.Slug)
	if slug == "" {
		return
	}
	cache[slug] = lock.Profile{
		Slug: slug,
		Name: strings.TrimSpace(profile.Name),
		Loc:  strings.TrimSpace(loc),
	}
}

func cloneProfileCache(in map[string]lock.Profile) map[string]lock.Profile {
	if len(in) == 0 {
		return map[string]lock.Profile{}
	}
	out := make(map[string]lock.Profile, len(in))
	for slug, profile := range in {
		out[slug] = profile
	}
	return out
}

func parseProfileSlug(raw string) (string, error) {
	slug := normalizeSlug(raw)
	if slug == "" {
		return "", nil
	}

	for _, r := range slug {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-' || r == '_' {
			continue
		}
		return "", fmt.Errorf("profile.slug %q contains invalid character %q (allowed: letters, digits, '-', '_')", raw, r)
	}
	return slug, nil
}

func normalizeSlug(raw string) string {
	return strings.ToLower(strings.TrimSpace(raw))
}

func resolveSource(sourceDir, raw string) (string, error) {
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

	up := ".." + string(filepath.Separator)
	if rel == ".." || strings.HasPrefix(rel, up) {
		return "", fmt.Errorf("path escapes source root %s: %s", root, resolved)
	}

	return resolved, nil
}

func mkdirParents(path string) ([]string, error) {
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

func cleanupAutoDirs(dirs []lock.Dir) error {
	ordered := append([]lock.Dir(nil), dirs...)
	sort.Slice(ordered, func(i, j int) bool {
		di := fileutils.PathDepth(ordered[i].Path)
		dj := fileutils.PathDepth(ordered[j].Path)
		if di == dj {
			return ordered[i].Path > ordered[j].Path
		}
		return di > dj
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
	}

	return nil
}

func snapshotIfExists(path string) (lock.Object, bool, error) {
	obj, err := snapshot(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return lock.Object{}, false, nil
		}
		return lock.Object{}, false, err
	}
	return obj, true, nil
}

func snapshot(path string) (lock.Object, error) {
	d, err := digest.ForPath(path)
	if err != nil {
		return lock.Object{}, err
	}

	return lock.Object{
		Path:   path,
		Digest: d.String(),
	}, nil
}
