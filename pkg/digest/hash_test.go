package digest

import (
	"os"
	"path/filepath"
	"testing"
)

func TestForPathDirectoryStableAcrossCreationOrder(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	dirA := filepath.Join(root, "a")
	dirB := filepath.Join(root, "b")

	if err := os.MkdirAll(filepath.Join(dirA, "nested"), 0o755); err != nil {
		t.Fatalf("mkdir dirA: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(dirB, "nested"), 0o755); err != nil {
		t.Fatalf("mkdir dirB: %v", err)
	}

	// Same content written in different order.
	if err := os.WriteFile(filepath.Join(dirA, "z.txt"), []byte("z"), 0o644); err != nil {
		t.Fatalf("write dirA z.txt: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dirA, "a.txt"), []byte("a"), 0o644); err != nil {
		t.Fatalf("write dirA a.txt: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dirA, "nested", "n.txt"), []byte("n"), 0o644); err != nil {
		t.Fatalf("write dirA nested/n.txt: %v", err)
	}

	if err := os.WriteFile(filepath.Join(dirB, "a.txt"), []byte("a"), 0o644); err != nil {
		t.Fatalf("write dirB a.txt: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dirB, "nested", "n.txt"), []byte("n"), 0o644); err != nil {
		t.Fatalf("write dirB nested/n.txt: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dirB, "z.txt"), []byte("z"), 0o644); err != nil {
		t.Fatalf("write dirB z.txt: %v", err)
	}

	dA, err := ForPath(dirA)
	if err != nil {
		t.Fatalf("ForPath(dirA): %v", err)
	}
	dB, err := ForPath(dirB)
	if err != nil {
		t.Fatalf("ForPath(dirB): %v", err)
	}

	if dA.String() != dB.String() {
		t.Fatalf("directory digests differ: %q != %q", dA.String(), dB.String())
	}
}
