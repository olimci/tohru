package cmd

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/olimci/tohru/pkg/manifest"
	"github.com/olimci/tohru/pkg/store"
	"github.com/olimci/tohru/pkg/store/state"
	"github.com/olimci/tohru/pkg/utils/fileutils"
)

func TestAddPathToProfileRollsBackCopiedSourcesOnManifestFailure(t *testing.T) {
	t.Cleanup(func() {
		writeManifest = manifest.Write
	})

	writeManifest = func(string, manifest.Manifest) error {
		return errors.New("disk full")
	}

	root := t.TempDir()
	s := store.Store{Root: filepath.Join(root, "store")}
	if err := s.Install(); err != nil {
		t.Fatalf("install store: %v", err)
	}

	const slug = "demo"
	profileDir := filepath.Join(s.ProfilesPath(), slug)
	if err := os.MkdirAll(profileDir, 0o755); err != nil {
		t.Fatalf("mkdir profile dir: %v", err)
	}
	if err := manifest.Write(filepath.Join(profileDir, manifest.Name), defaultManifest(slug)); err != nil {
		t.Fatalf("write initial manifest: %v", err)
	}
	if err := s.SaveProfiles(map[string]state.Profile{
		slug: {
			Slug: slug,
			Name: slug,
			Loc:  profileDir,
		},
	}); err != nil {
		t.Fatalf("save profiles: %v", err)
	}

	localPath := filepath.Join(root, "captured", "dot_zshrc")
	if err := os.MkdirAll(filepath.Dir(localPath), 0o755); err != nil {
		t.Fatalf("mkdir source dir: %v", err)
	}
	if err := os.WriteFile(localPath, []byte("export TEST=1\n"), 0o644); err != nil {
		t.Fatalf("write source file: %v", err)
	}

	info, err := os.Lstat(localPath)
	if err != nil {
		t.Fatalf("lstat source file: %v", err)
	}

	_, err = addPath(s, slug, localPath, info)
	if err == nil {
		t.Fatalf("expected addPath to fail")
	}
	if !strings.Contains(err.Error(), "rolled back copied sources") {
		t.Fatalf("expected rollback error, got %v", err)
	}

	relRoot, err := filepath.Rel(string(filepath.Separator), localPath)
	if err != nil {
		t.Fatalf("compute expected profile path: %v", err)
	}
	copiedPath := manifest.SourcePath(filepath.Join(profileDir, "root"), fileutils.SplitPathParts(relRoot))
	if _, err := os.Lstat(copiedPath); !os.IsNotExist(err) {
		t.Fatalf("expected copied path to be rolled back, stat err=%v", err)
	}

	m, _, err := manifest.Load(profileDir)
	if err != nil {
		t.Fatalf("reload manifest: %v", err)
	}
	if len(m.Trees) != 0 {
		t.Fatalf("expected manifest to remain unchanged, got %+v", m.Trees)
	}
}
