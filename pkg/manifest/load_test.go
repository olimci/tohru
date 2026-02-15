package manifest

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadTrackedDefaultsToTrueWhenOmitted(t *testing.T) {
	t.Parallel()

	manifestPath := writeManifest(t, `
[source]
name = "example"

[[file]]
from = "a"
to = "b"

[[dir]]
to = "c"
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
from = "a"
to = "b"
tracked = false

[[dir]]
to = "c"
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

func writeManifest(t *testing.T, body string) string {
	t.Helper()

	dir := t.TempDir()
	path := filepath.Join(dir, "tohru.toml")
	if err := os.WriteFile(path, []byte(strings.TrimSpace(body)+"\n"), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	return path
}
