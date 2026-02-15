package store

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"

	"github.com/olimci/tohru/pkg/digest"
	"github.com/olimci/tohru/pkg/manifest"
	"github.com/olimci/tohru/pkg/store/config"
	"github.com/olimci/tohru/pkg/store/lock"
	"github.com/olimci/tohru/pkg/utils/fileutils"
	"github.com/olimci/tohru/pkg/version"
)

type LoadResult struct {
	SourceDir            string
	SourceName           string
	TrackedCount         int
	UnloadedSourceName   string
	UnloadedTrackedCount int
	RemovedBackupCount   int
	ChangedPaths         []string
}

type UnloadResult struct {
	SourceName         string
	RemovedCount       int
	RemovedBackupCount int
	ChangedPaths       []string
}

type TidyResult struct {
	RemovedCount int
	ChangedPaths []string
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

func (s Store) Switch(source string, force bool) (LoadResult, error) {
	return s.Load(source, force)
}

func (s Store) Load(source string, force bool) (LoadResult, error) {
	if err := s.EnsureInstalled(); err != nil {
		return LoadResult{}, err
	}

	cfg, err := s.LoadConfig()
	if err != nil {
		return LoadResult{}, err
	}

	return s.switchWithConfig(cfg, source, force)
}

func (s Store) Reload(force bool) (LoadResult, error) {
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
		return LoadResult{}, fmt.Errorf("no loaded source to reload")
	}
	if lck.Manifest.Kind != "local" {
		return LoadResult{}, fmt.Errorf("unsupported source kind %q", lck.Manifest.Kind)
	}
	if lck.Manifest.Loc == "" {
		return LoadResult{}, fmt.Errorf("loaded source location is empty")
	}

	return s.switchWithConfig(cfg, lck.Manifest.Loc, force)
}

// Upgrade is kept as a compatibility alias for older callers.
func (s Store) Upgrade(force bool) (LoadResult, error) {
	return s.Reload(force)
}

func (s Store) Unload(force bool) (UnloadResult, error) {
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
		if err := unloadManagedPaths(s, lck.Files, nil, force, changes.Add); err != nil {
			return UnloadResult{}, err
		}
	}
	if err := cleanupTrackedAutoDirs(lck.Dirs); err != nil {
		return UnloadResult{}, err
	}

	newLock := DefaultLock()
	if err := s.SaveLock(newLock); err != nil {
		return UnloadResult{}, err
	}
	changes.Add(s.LockPath())

	removedBackups := 0

	if cfg.Options.Clean {
		removedBackups, err = cleanBackupStore(s, newLock.Files, changes.Add)
		if err != nil {
			return UnloadResult{}, err
		}
	}

	return UnloadResult{
		SourceName:         sourceDisplayName(lck.Manifest.Name, lck.Manifest.Loc),
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
	removed, err := cleanBackupStore(s, lck.Files, changes.Add)
	if err != nil {
		return TidyResult{}, err
	}

	return TidyResult{
		RemovedCount: removed,
		ChangedPaths: changes.Paths(),
	}, nil
}

func (s Store) switchWithConfig(cfg config.Config, source string, force bool) (LoadResult, error) {
	m, sourceDir, err := manifest.Load(source)
	if err != nil {
		return LoadResult{}, err
	}
	if err := version.EnsureCompatible(m.Tohru.Version); err != nil {
		return LoadResult{}, fmt.Errorf("unsupported source version %q: %w", m.Tohru.Version, err)
	}

	ops, err := buildManifestOps(m, sourceDir)
	if err != nil {
		return LoadResult{}, err
	}

	oldLock, err := s.LoadLock()
	if err != nil {
		return LoadResult{}, err
	}
	changes := newPathRecorder()

	oldByPath := make(map[string]lock.File, len(oldLock.Files))
	for _, f := range oldLock.Files {
		oldByPath[f.Path] = f
	}

	occupiedByNew := make(map[string]struct{}, len(ops))
	for _, op := range ops {
		occupiedByNew[op.Dest] = struct{}{}
	}

	if err := unloadManagedPaths(s, oldLock.Files, occupiedByNew, force, changes.Add); err != nil {
		return LoadResult{}, err
	}
	if err := cleanupTrackedAutoDirs(oldLock.Dirs); err != nil {
		return LoadResult{}, err
	}

	// Persist unloaded state before loading the new source so failures don't
	// leave lock metadata claiming the old source is active.
	unloaded := DefaultLock()
	if err := s.SaveLock(unloaded); err != nil {
		return LoadResult{}, err
	}
	changes.Add(s.LockPath())

	tracked, autoDirs, err := applyManifestOps(s, cfg, ops, oldByPath, force, changes.Add)
	if err != nil {
		return LoadResult{}, err
	}

	newLock := DefaultLock()
	newLock.Manifest.State = "loaded"
	newLock.Manifest.Kind = "local"
	newLock.Manifest.Loc = sourceDir
	newLock.Manifest.Name = strings.TrimSpace(m.Source.Name)
	newLock.Files = tracked
	newLock.Dirs = autoDirs

	if err := s.SaveLock(newLock); err != nil {
		return LoadResult{}, err
	}
	changes.Add(s.LockPath())

	removedBackups := 0

	if cfg.Options.Clean {
		removedBackups, err = cleanBackupStore(s, newLock.Files, changes.Add)
		if err != nil {
			return LoadResult{}, err
		}
	}

	return LoadResult{
		SourceDir:            sourceDir,
		SourceName:           sourceDisplayName(m.Source.Name, sourceDir),
		TrackedCount:         len(tracked),
		UnloadedSourceName:   sourceDisplayName(oldLock.Manifest.Name, oldLock.Manifest.Loc),
		UnloadedTrackedCount: len(oldLock.Files),
		RemovedBackupCount:   removedBackups,
		ChangedPaths:         changes.Paths(),
	}, nil
}

func buildManifestOps(m manifest.Manifest, sourceDir string) ([]manifestOp, error) {
	ops := make([]manifestOp, 0, len(m.Links)+len(m.Files)+len(m.Dirs))
	seenDest := make(map[string]struct{}, len(m.Links)+len(m.Files)+len(m.Dirs))

	add := func(op manifestOp) error {
		if _, ok := seenDest[op.Dest]; ok {
			return fmt.Errorf("duplicate destination in manifest: %s", op.Dest)
		}
		seenDest[op.Dest] = struct{}{}
		ops = append(ops, op)
		return nil
	}

	for _, l := range m.Links {
		src, err := resolveSourcePath(sourceDir, l.To)
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

	for _, f := range m.Files {
		src, err := resolveSourcePath(sourceDir, f.Source)
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
			Track:  f.IsTracked(),
		}); err != nil {
			return nil, err
		}
	}

	for _, d := range m.Dirs {
		dest, err := fileutils.AbsPath(d.Path)
		if err != nil {
			return nil, fmt.Errorf("dir.path %q: %w", d.Path, err)
		}

		if err := add(manifestOp{
			Kind:  opDir,
			Dest:  dest,
			Track: d.IsTracked(),
		}); err != nil {
			return nil, err
		}
	}

	return ops, nil
}

func applyManifestOps(store Store, cfg config.Config, ops []manifestOp, oldByPath map[string]lock.File, force bool, recordPath func(string)) ([]lock.File, []lock.Dir, error) {
	tracked := make([]lock.File, 0, len(ops))
	autoDirSet := make(map[string]struct{}, 16)

	for _, op := range ops {
		var prev *lock.Object
		if old, ok := oldByPath[op.Dest]; ok {
			prev = old.Prev
		}

		prevAfterPrepare, err := prepareDestinationForApply(store, cfg, op, prev, force, recordPath)
		if err != nil {
			return nil, nil, fmt.Errorf("%s %s: %w", op.Kind, op.Dest, err)
		}

		createdParents, err := ensureParentDirs(op.Dest)
		if err != nil {
			return nil, nil, err
		}
		for _, dir := range createdParents {
			autoDirSet[dir] = struct{}{}
		}

		switch op.Kind {
		case opLink:
			if err := os.Symlink(op.Source, op.Dest); err != nil {
				return nil, nil, fmt.Errorf("create symlink %s -> %s: %w", op.Dest, op.Source, err)
			}
			recordChange(recordPath, op.Dest)
		case opFile:
			if err := fileutils.CopyFile(op.Source, op.Dest); err != nil {
				return nil, nil, err
			}
			recordChange(recordPath, op.Dest)
		case opDir:
			if err := os.MkdirAll(op.Dest, 0o755); err != nil {
				return nil, nil, fmt.Errorf("create directory %s: %w", op.Dest, err)
			}
			recordChange(recordPath, op.Dest)
		default:
			return nil, nil, fmt.Errorf("unsupported operation kind %q", op.Kind)
		}

		if !op.Track {
			continue
		}

		curr, err := snapshotObject(op.Dest)
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

func prepareDestinationForApply(store Store, cfg config.Config, op manifestOp, prev *lock.Object, force bool, recordPath func(string)) (*lock.Object, error) {
	current, exists, err := snapshotObjectIfExists(op.Dest)
	if err != nil {
		return nil, err
	}
	if !exists {
		return prev, nil
	}

	if !op.Track {
		if !force {
			return nil, fmt.Errorf("destination exists (would clobber), use --force to overwrite")
		}
		if err := fileutils.RemovePath(op.Dest); err != nil {
			return nil, err
		}
		recordChange(recordPath, op.Dest)
		return prev, nil
	}

	if prev == nil && cfg.Options.Backup {
		storedPrev, err := persistBackupObject(store, current, recordPath)
		if err != nil {
			return nil, err
		}
		if err := fileutils.RemovePath(op.Dest); err != nil {
			return nil, err
		}
		recordChange(recordPath, op.Dest)
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
	recordChange(recordPath, op.Dest)

	return prev, nil
}

func unloadManagedPaths(store Store, files []lock.File, occupiedByNew map[string]struct{}, force bool, recordPath func(string)) error {
	for _, managed := range orderManagedFilesForRemoval(files) {
		if err := removeManagedObject(managed, force, recordPath); err != nil {
			return err
		}

		if managed.Prev != nil && managed.Prev.Digest != "" {
			if _, stillOccupied := occupiedByNew[managed.Path]; stillOccupied {
				continue
			}
			if err := restoreBackupObject(store, managed.Prev, managed.Path, force, recordPath); err != nil {
				return err
			}
		}
	}

	return nil
}

func removeManagedObject(managed lock.File, force bool, recordPath func(string)) error {
	path := strings.TrimSpace(managed.Path)
	if path == "" {
		return nil
	}

	current, exists, err := snapshotObjectIfExists(path)
	if err != nil {
		return fmt.Errorf("check managed path %s: %w", path, err)
	}
	if !exists {
		if force {
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
	if !force && !expected.IsZero() && expected.String() != actual.String() {
		return fmt.Errorf("managed path was modified: %s", path)
	}

	if err := fileutils.RemovePath(path); err != nil {
		return fmt.Errorf("remove managed path %s: %w", path, err)
	}
	recordChange(recordPath, path)

	return nil
}

func persistBackupObject(store Store, object lock.Object, recordPath func(string)) (*lock.Object, error) {
	d, err := digest.Parse(object.Digest)
	if err != nil {
		return nil, fmt.Errorf("parse backup digest for %s: %w", object.Path, err)
	}
	if d.IsZero() {
		return nil, fmt.Errorf("cannot backup object %s with empty digest", object.Path)
	}

	cid := d.String()
	objectPath := backupObjectPath(store, cid)

	existingBackup, exists, err := snapshotObjectIfExists(objectPath)
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
	recordChange(recordPath, objectPath)

	written, err := snapshotObject(objectPath)
	if err != nil {
		return nil, fmt.Errorf("snapshot backup object %s: %w", objectPath, err)
	}
	if written.Digest != d.String() {
		_ = fileutils.RemovePath(objectPath)
		return nil, fmt.Errorf("backup digest mismatch for %s", objectPath)
	}

	return &lock.Object{Path: objectPath, Digest: d.String()}, nil
}

func restoreBackupObject(store Store, prev *lock.Object, destination string, force bool, recordPath func(string)) error {
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
		backupPath = backupObjectPath(store, d.String())
	}

	backup, exists, err := snapshotObjectIfExists(backupPath)
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

	_, destinationExists, err := snapshotObjectIfExists(destination)
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
		recordChange(recordPath, destination)
	}

	if err := fileutils.CopyPath(backupPath, destination); err != nil {
		return fmt.Errorf("restore backup %s to %s: %w", backupPath, destination, err)
	}
	recordChange(recordPath, destination)

	return nil
}

func cleanBackupStore(store Store, tracked []lock.File, recordPath func(string)) (int, error) {
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
		recordChange(recordPath, path)
		removed++
	}

	return removed, nil
}

func backupObjectPath(store Store, cid string) string {
	return filepath.Join(store.BackupsPath(), cid, "object")
}

func orderManagedFilesForRemoval(files []lock.File) []lock.File {
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

func recordChange(recordPath func(string), path string) {
	if recordPath == nil {
		return
	}
	recordPath(path)
}

func sourceDisplayName(name, loc string) string {
	if trimmedName := strings.TrimSpace(name); trimmedName != "" {
		return trimmedName
	}
	trimmedLoc := strings.TrimSpace(loc)
	if trimmedLoc == "" {
		return ""
	}
	return filepath.Base(filepath.Clean(trimmedLoc))
}

func resolveSourcePath(sourceDir, raw string) (string, error) {
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

func ensureParentDirs(path string) ([]string, error) {
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

func cleanupTrackedAutoDirs(dirs []lock.Dir) error {
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

func snapshotObjectIfExists(path string) (lock.Object, bool, error) {
	obj, err := snapshotObject(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return lock.Object{}, false, nil
		}
		return lock.Object{}, false, err
	}
	return obj, true, nil
}

func snapshotObject(path string) (lock.Object, error) {
	d, err := digest.ForPath(path)
	if err != nil {
		return lock.Object{}, err
	}

	return lock.Object{
		Path:   path,
		Digest: d.String(),
	}, nil
}
