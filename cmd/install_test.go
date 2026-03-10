package cmd

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/olimci/tohru/pkg/manifest"
	"github.com/olimci/tohru/pkg/store"
	"github.com/olimci/tohru/pkg/version"
)

func TestInstallForceSucceedsWhenAlreadyInstalled(t *testing.T) {
	root := t.TempDir()
	storeRoot := filepath.Join(root, "store")
	t.Setenv("TOHRU_STORE_DIR", storeRoot)

	s := store.Store{Root: storeRoot}
	if err := s.Install(); err != nil {
		t.Fatalf("install store: %v", err)
	}

	if err := Execute(context.Background(), []string{"tohru", "install", "--force"}); err != nil {
		t.Fatalf("install --force: %v", err)
	}
}

func TestInstallForceLoadsProfileWhenAlreadyInstalled(t *testing.T) {
	root := t.TempDir()
	storeRoot := filepath.Join(root, "store")
	t.Setenv("TOHRU_STORE_DIR", storeRoot)

	s := store.Store{Root: storeRoot}
	if err := s.Install(); err != nil {
		t.Fatalf("install store: %v", err)
	}

	profileDir := filepath.Join(root, "profile")
	targetDir := filepath.Join(root, "target")
	srcDir := filepath.Join(profileDir, "src")
	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		t.Fatalf("mkdir source dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "dot_zshrc"), []byte("export TEST=1\n"), 0o644); err != nil {
		t.Fatalf("write source file: %v", err)
	}

	m := manifest.Manifest{
		Tohru: manifest.Tohru{Version: version.Version},
		Profile: manifest.Profile{
			Slug: "demo",
			Name: "Demo",
		},
		Trees: map[string]manifest.Tree{
			"src": {
				Dest: targetDir,
				Files: map[string]any{
					".zshrc": map[string]any{"mode": "copy"},
				},
			},
		},
	}
	if err := manifest.Write(filepath.Join(profileDir, manifest.Name), m); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	if err := Execute(context.Background(), []string{"tohru", "install", "--force", profileDir}); err != nil {
		t.Fatalf("install --force profile: %v", err)
	}

	st, err := s.LoadState()
	if err != nil {
		t.Fatalf("load state: %v", err)
	}
	if st.Manifest.State != "loaded" || st.Manifest.Loc != profileDir {
		t.Fatalf("unexpected state after install --force: %+v", st.Manifest)
	}
}
