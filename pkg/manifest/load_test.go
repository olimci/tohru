package manifest

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestLoadTrackedDefaultsToTrueWhenOmitted(t *testing.T) {
	t.Parallel()

	manifestPath := writeManifest(t, `
[source]
name = "example"

[[file]]
source = "a"
dest = "b"

[[dir]]
path = "c"
`)

	m, _, err := Load(manifestPath)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	if len(m.Files) != 1 || len(m.Dirs) != 1 {
		t.Fatalf("unexpected manifest lengths: files=%d dirs=%d", len(m.Files), len(m.Dirs))
	}
	if !m.Files[0].IsTracked() {
		t.Fatal("file should default to tracked=true when tracked is omitted")
	}
	if !m.Dirs[0].IsTracked() {
		t.Fatal("dir should default to tracked=true when tracked is omitted")
	}
}

func TestLoadHonorsTrackedValue(t *testing.T) {
	t.Parallel()

	manifestPath := writeManifest(t, `
[source]
name = "example"

[[file]]
source = "a"
dest = "b"
tracked = false

[[dir]]
path = "c"
tracked = false
`)

	m, _, err := Load(manifestPath)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	if len(m.Files) != 1 || len(m.Dirs) != 1 {
		t.Fatalf("unexpected manifest lengths: files=%d dirs=%d", len(m.Files), len(m.Dirs))
	}
	if m.Files[0].IsTracked() {
		t.Fatal("file should resolve tracked=false")
	}
	if m.Dirs[0].IsTracked() {
		t.Fatal("dir should resolve tracked=false")
	}
}

func TestLoadResolvesNestedImportsWithConstraints(t *testing.T) {
	t.Parallel()

	rootDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(rootDir, "manifests", "platform"), 0o755); err != nil {
		t.Fatalf("mkdir manifests: %v", err)
	}

	writeManifestFile(t, filepath.Join(rootDir, "tohru.toml"), `
[source]
name = "root"

[[import]]
path = "manifests/base.toml"

[[import]]
path = "manifests/platform/current.toml"
os = ["`+runtime.GOOS+`"]
arch = ["`+runtime.GOARCH+`"]

[[import]]
path = "manifests/platform/skip.toml"
os = ["definitely-not-this-os"]

[[dir]]
path = "root-dir"
`)

	writeManifestFile(t, filepath.Join(rootDir, "manifests", "base.toml"), `
[tohru]
version = "0.1.0"

[source]
description = "from import"

[[import]]
path = "nested.toml"

[[file]]
source = "from-base"
dest = "to-base"
`)

	writeManifestFile(t, filepath.Join(rootDir, "manifests", "nested.toml"), `
[[dir]]
path = "nested-dir"
tracked = false
`)

	writeManifestFile(t, filepath.Join(rootDir, "manifests", "platform", "current.toml"), `
[[link]]
to = "current-link"
from = "current-dest"
`)

	writeManifestFile(t, filepath.Join(rootDir, "manifests", "platform", "skip.toml"), `
[[file]]
source = "from-skip"
dest = "to-skip"
`)

	m, sourceDir, err := Load(rootDir)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	if sourceDir != rootDir {
		t.Fatalf("sourceDir = %q, want %q", sourceDir, rootDir)
	}
	if m.Source.Name != "root" {
		t.Fatalf("source name = %q, want root", m.Source.Name)
	}
	if m.Source.Description != "from import" {
		t.Fatalf("source description = %q, want from import", m.Source.Description)
	}
	if m.Tohru.Version != "0.1.0" {
		t.Fatalf("tohru version = %q, want 0.1.0", m.Tohru.Version)
	}

	if len(m.Links) != 1 {
		t.Fatalf("links length = %d, want 1", len(m.Links))
	}
	if len(m.Files) != 1 {
		t.Fatalf("files length = %d, want 1", len(m.Files))
	}
	if len(m.Dirs) != 2 {
		t.Fatalf("dirs length = %d, want 2", len(m.Dirs))
	}

	if m.Dirs[0].Path != "nested-dir" || m.Dirs[0].IsTracked() {
		t.Fatalf("nested imported dir unexpected: %#v", m.Dirs[0])
	}
	if m.Dirs[1].Path != "root-dir" || !m.Dirs[1].IsTracked() {
		t.Fatalf("root dir unexpected: %#v", m.Dirs[1])
	}
}

func TestLoadImportSupportsDirectoryPath(t *testing.T) {
	t.Parallel()

	rootDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(rootDir, "manifests", "group"), 0o755); err != nil {
		t.Fatalf("mkdir manifests: %v", err)
	}

	writeManifestFile(t, filepath.Join(rootDir, "tohru.toml"), `
[[import]]
path = "manifests/group"
`)
	writeManifestFile(t, filepath.Join(rootDir, "manifests", "group", "tohru.toml"), `
[[dir]]
path = "group-dir"
`)

	m, _, err := Load(rootDir)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if len(m.Dirs) != 1 || m.Dirs[0].Path != "group-dir" {
		t.Fatalf("unexpected loaded dirs: %#v", m.Dirs)
	}
}

func TestLoadDetectsImportCycles(t *testing.T) {
	t.Parallel()

	rootDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(rootDir, "manifests"), 0o755); err != nil {
		t.Fatalf("mkdir manifests: %v", err)
	}

	writeManifestFile(t, filepath.Join(rootDir, "tohru.toml"), `
[[import]]
path = "manifests/a.toml"
`)
	writeManifestFile(t, filepath.Join(rootDir, "manifests", "a.toml"), `
[[import]]
path = "b.toml"
`)
	writeManifestFile(t, filepath.Join(rootDir, "manifests", "b.toml"), `
[[import]]
path = "a.toml"
`)

	_, _, err := Load(rootDir)
	if err == nil {
		t.Fatal("expected cycle detection error")
	}
	if !strings.Contains(err.Error(), "manifest import cycle detected") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadRejectsImportOutsideSourceRoot(t *testing.T) {
	t.Parallel()

	rootDir := t.TempDir()
	outsidePath := filepath.Join(filepath.Dir(rootDir), "outside.toml")
	writeManifestFile(t, filepath.Join(rootDir, "tohru.toml"), `
[[import]]
path = "../outside.toml"
`)
	writeManifestFile(t, outsidePath, `
[[dir]]
path = "outside"
`)

	_, _, err := Load(rootDir)
	if err == nil {
		t.Fatal("expected import root-escape error")
	}
	if !strings.Contains(err.Error(), "import path escapes source root") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadWithTreeReturnsResolvedImportTree(t *testing.T) {
	t.Parallel()

	rootDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(rootDir, "manifests"), 0o755); err != nil {
		t.Fatalf("mkdir manifests: %v", err)
	}

	writeManifestFile(t, filepath.Join(rootDir, "tohru.toml"), `
[[import]]
path = "manifests/base.toml"

[[import]]
path = "manifests/skipped.toml"
os = ["not-this-os"]
`)
	writeManifestFile(t, filepath.Join(rootDir, "manifests", "base.toml"), `
[[import]]
path = "nested.toml"
`)
	writeManifestFile(t, filepath.Join(rootDir, "manifests", "nested.toml"), `
[[dir]]
path = "nested"
`)
	writeManifestFile(t, filepath.Join(rootDir, "manifests", "skipped.toml"), `
[[dir]]
path = "skipped"
`)

	_, sourceDir, tree, err := LoadWithTree(rootDir)
	if err != nil {
		t.Fatalf("LoadWithTree returned error: %v", err)
	}
	if sourceDir != rootDir {
		t.Fatalf("sourceDir = %q, want %q", sourceDir, rootDir)
	}

	paths := flattenTreePaths(t, sourceDir, tree)
	expected := []string{"tohru.toml", "manifests/base.toml", "manifests/nested.toml"}
	if strings.Join(paths, "|") != strings.Join(expected, "|") {
		t.Fatalf("tree paths = %#v, want %#v", paths, expected)
	}
}

func writeManifest(t *testing.T, body string) string {
	t.Helper()

	dir := t.TempDir()
	path := filepath.Join(dir, "tohru.toml")
	if err := os.WriteFile(path, []byte(strings.TrimSpace(body)+"\n"), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	return path
}

func writeManifestFile(t *testing.T, path, body string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir for manifest: %v", err)
	}
	if err := os.WriteFile(path, []byte(strings.TrimSpace(body)+"\n"), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
}

func flattenTreePaths(t *testing.T, sourceDir string, tree ImportTree) []string {
	t.Helper()

	if strings.TrimSpace(tree.Path) == "" {
		return nil
	}
	canonicalSourceDir, err := filepath.EvalSymlinks(filepath.Clean(sourceDir))
	if err != nil {
		t.Fatalf("canonical source dir %s: %v", sourceDir, err)
	}

	out := make([]string, 0, 8)

	var walk func(ImportTree)
	walk = func(node ImportTree) {
		rel, err := filepath.Rel(canonicalSourceDir, node.Path)
		if err != nil {
			t.Fatalf("rel path for %s: %v", node.Path, err)
		}
		out = append(out, filepath.ToSlash(rel))
		for _, child := range node.Imports {
			walk(child)
		}
	}

	walk(tree)
	return out
}
