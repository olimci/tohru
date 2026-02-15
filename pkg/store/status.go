package store

import (
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/olimci/tohru/pkg/digest"
	"github.com/olimci/tohru/pkg/store/lock"
)

type StatusSnapshot struct {
	Manifest        lock.Manifest
	Tracked         []TrackedStatus
	BackupRefs      []BackupRefStatus
	OrphanedBackups []string
	BrokenBackups   []string
}

type TrackedStatus struct {
	Path          string
	PrevDigest    string
	BackupPresent bool
	Drifted       bool
	Missing       bool
}

type BackupRefStatus struct {
	Digest  string
	Paths   []string
	Present bool
}

func (s Store) Status() (StatusSnapshot, error) {
	if !s.IsInstalled() {
		return StatusSnapshot{}, ErrNotInstalled
	}

	lck, err := s.LoadLock()
	if err != nil {
		return StatusSnapshot{}, err
	}

	availableBackups, brokenBackups, err := scanBackupStore(s)
	if err != nil {
		return StatusSnapshot{}, err
	}

	tracked := make([]TrackedStatus, 0, len(lck.Files))
	refPaths := make(map[string][]string, len(lck.Files))
	for _, f := range lck.Files {
		path := strings.TrimSpace(f.Path)
		if path == "" {
			continue
		}

		item := TrackedStatus{Path: path}

		current, exists, snapshotErr := snapshotObjectIfExists(path)
		if snapshotErr != nil {
			return StatusSnapshot{}, fmt.Errorf("snapshot tracked path %s: %w", path, snapshotErr)
		}
		if !exists {
			item.Drifted = true
			item.Missing = true
		} else if strings.TrimSpace(f.Curr.Digest) != "" {
			expectedDigest, parseExpectedErr := digest.Parse(f.Curr.Digest)
			if parseExpectedErr != nil {
				return StatusSnapshot{}, fmt.Errorf("parse tracked digest for %s: %w", f.Path, parseExpectedErr)
			}
			actualDigest, parseActualErr := digest.Parse(current.Digest)
			if parseActualErr != nil {
				return StatusSnapshot{}, fmt.Errorf("parse current digest for %s: %w", f.Path, parseActualErr)
			}
			item.Drifted = expectedDigest.String() != actualDigest.String()
		}

		if f.Prev != nil && strings.TrimSpace(f.Prev.Digest) != "" {
			d, parseErr := digest.Parse(f.Prev.Digest)
			if parseErr != nil {
				return StatusSnapshot{}, fmt.Errorf("parse previous digest for %s: %w", f.Path, parseErr)
			}
			if !d.IsZero() {
				cid := d.String()
				item.PrevDigest = cid
				_, item.BackupPresent = availableBackups[cid]
				refPaths[cid] = append(refPaths[cid], path)
			}
		}

		tracked = append(tracked, item)
	}

	sort.Slice(tracked, func(i, j int) bool {
		return tracked[i].Path < tracked[j].Path
	})

	refDigests := make([]string, 0, len(refPaths))
	for cid := range refPaths {
		refDigests = append(refDigests, cid)
	}
	sort.Strings(refDigests)

	refs := make([]BackupRefStatus, 0, len(refDigests))
	for _, cid := range refDigests {
		paths := append([]string(nil), refPaths[cid]...)
		sort.Strings(paths)
		_, present := availableBackups[cid]
		refs = append(refs, BackupRefStatus{
			Digest:  cid,
			Paths:   paths,
			Present: present,
		})
	}

	orphaned := make([]string, 0, len(availableBackups))
	for cid := range availableBackups {
		if _, referenced := refPaths[cid]; referenced {
			continue
		}
		orphaned = append(orphaned, cid)
	}
	sort.Strings(orphaned)

	return StatusSnapshot{
		Manifest:        lck.Manifest,
		Tracked:         tracked,
		BackupRefs:      refs,
		OrphanedBackups: orphaned,
		BrokenBackups:   brokenBackups,
	}, nil
}

func scanBackupStore(store Store) (map[string]struct{}, []string, error) {
	entries, err := os.ReadDir(store.BackupsPath())
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return map[string]struct{}{}, nil, nil
		}
		return nil, nil, fmt.Errorf("read backups directory %s: %w", store.BackupsPath(), err)
	}

	available := make(map[string]struct{}, len(entries))
	broken := make([]string, 0, len(entries))
	for _, entry := range entries {
		cid := entry.Name()
		path := backupObjectPath(store, cid)
		if _, statErr := os.Lstat(path); statErr == nil {
			available[cid] = struct{}{}
			continue
		} else if errors.Is(statErr, os.ErrNotExist) {
			broken = append(broken, cid)
			continue
		} else {
			return nil, nil, fmt.Errorf("stat backup object %s: %w", path, statErr)
		}
	}
	sort.Strings(broken)

	return available, broken, nil
}
