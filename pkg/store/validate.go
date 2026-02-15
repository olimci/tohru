package store

import (
	"fmt"
	"os"
	"strings"

	"github.com/olimci/tohru/pkg/manifest"
	"github.com/olimci/tohru/pkg/version"
)

type ValidateResult struct {
	SourceDir  string
	SourceName string
	OpCount    int
	LinkCount  int
	FileCount  int
	DirCount   int
	ImportTree manifest.ImportTree
}

func (s Store) Validate(source string) (ValidateResult, error) {
	target, err := s.resolveValidateSource(source)
	if err != nil {
		return ValidateResult{}, err
	}

	m, sourceDir, tree, err := manifest.LoadWithTree(target)
	if err != nil {
		return ValidateResult{}, err
	}
	if err := version.EnsureCompatible(m.Tohru.Version); err != nil {
		return ValidateResult{}, fmt.Errorf("unsupported source version %q: %w", m.Tohru.Version, err)
	}

	ops, err := buildManifestOps(m, sourceDir)
	if err != nil {
		return ValidateResult{}, err
	}

	for _, op := range ops {
		switch op.Kind {
		case opLink:
			// Symlink targets may intentionally not exist yet.
		case opFile:
			info, err := os.Stat(op.Source)
			if err != nil {
				return ValidateResult{}, fmt.Errorf("validate file source %s: %w", op.Source, err)
			}
			if !info.Mode().IsRegular() {
				return ValidateResult{}, fmt.Errorf("validate file source %s: source is not a regular file", op.Source)
			}
		case opDir:
			// No source path for dir operations.
		default:
			return ValidateResult{}, fmt.Errorf("validate operation %s: unsupported operation kind", op.Kind)
		}
	}

	return ValidateResult{
		SourceDir:  sourceDir,
		SourceName: sourceDisplayName(m.Source.Name, sourceDir),
		OpCount:    len(ops),
		LinkCount:  len(m.Links),
		FileCount:  len(m.Files),
		DirCount:   len(m.Dirs),
		ImportTree: tree,
	}, nil
}

func (s Store) resolveValidateSource(source string) (string, error) {
	trimmed := strings.TrimSpace(source)
	if trimmed != "" {
		return trimmed, nil
	}

	if !s.IsInstalled() {
		return "", fmt.Errorf("validate requires a source argument when tohru is not installed")
	}

	lck, err := s.LoadLock()
	if err != nil {
		return "", err
	}
	if strings.ToLower(lck.Manifest.State) != "loaded" {
		return "", fmt.Errorf("validate requires a source argument when no source is loaded")
	}
	if lck.Manifest.Kind != "local" {
		return "", fmt.Errorf("unsupported source kind %q", lck.Manifest.Kind)
	}
	if strings.TrimSpace(lck.Manifest.Loc) == "" {
		return "", fmt.Errorf("loaded source location is empty")
	}

	return lck.Manifest.Loc, nil
}
