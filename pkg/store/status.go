package store

import (
	"errors"
	"fmt"
	"maps"
	"os"
	"slices"
	"strings"

	"github.com/olimci/tohru/pkg/digest"
	"github.com/olimci/tohru/pkg/store/state"
)

type StatusSnapshot struct {
	Profile         state.Profile
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
	ManagedKind   digest.Kind `json:"-"`
	Operation     string      `json:"-"`
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

	lck, err := s.LoadState()
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
		kind, operation, presentationErr := trackedPresentation(f.Current.Digest)
		if presentationErr != nil {
			return StatusSnapshot{}, fmt.Errorf("parse tracked object metadata for %s: %w", f.Path, presentationErr)
		}
		item.ManagedKind = kind
		item.Operation = operation

		current, exists, snapshotErr := maybeSnapshot(path)
		if snapshotErr != nil {
			return StatusSnapshot{}, fmt.Errorf("snapshot tracked path %s: %w", path, snapshotErr)
		}
		if !exists {
			item.Drifted = true
			item.Missing = true
		} else if strings.TrimSpace(f.Current.Digest) != "" {
			expectedDigest, parseExpectedErr := digest.Parse(f.Current.Digest)
			if parseExpectedErr != nil {
				return StatusSnapshot{}, fmt.Errorf("parse tracked digest for %s: %w", f.Path, parseExpectedErr)
			}
			actualDigest, parseActualErr := digest.Parse(current.Digest)
			if parseActualErr != nil {
				return StatusSnapshot{}, fmt.Errorf("parse current digest for %s: %w", f.Path, parseActualErr)
			}
			item.Drifted = expectedDigest.String() != actualDigest.String()
		}

		if f.Previous != nil && strings.TrimSpace(f.Previous.Digest) != "" {
			d, parseErr := digest.Parse(f.Previous.Digest)
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

	slices.SortFunc(tracked, func(a, b TrackedStatus) int {
		return strings.Compare(a.Path, b.Path)
	})

	refs := make([]BackupRefStatus, 0, len(refPaths))
	for _, cid := range slices.Sorted(maps.Keys(refPaths)) {
		paths := slices.Clone(refPaths[cid])
		slices.Sort(paths)
		_, present := availableBackups[cid]
		refs = append(refs, BackupRefStatus{
			Digest:  cid,
			Paths:   paths,
			Present: present,
		})
	}

	orphaned := make([]string, 0, len(availableBackups))
	for _, cid := range slices.Sorted(maps.Keys(availableBackups)) {
		if _, referenced := refPaths[cid]; referenced {
			continue
		}
		orphaned = append(orphaned, cid)
	}

	return StatusSnapshot{
		Profile:         lck.Profile,
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
		path := backupPath(store, cid)
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
	slices.Sort(broken)

	return available, broken, nil
}

func trackedPresentation(rawDigest string) (digest.Kind, string, error) {
	d, err := digest.Parse(rawDigest)
	if err != nil {
		return "", "", err
	}

	switch d.Kind {
	case digest.KindSymlink:
		return d.Kind, "link", nil
	case digest.KindFile, digest.KindDir:
		return d.Kind, "copy", nil
	default:
		return d.Kind, "", nil
	}
}
