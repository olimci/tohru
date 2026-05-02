package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/olimci/tohru/pkg/manifest"
)

func TestBuildEntriesAndInsertEntryStructuralTree(t *testing.T) {
	tests := []struct {
		name     string
		buildDir func(t *testing.T) (string, os.FileInfo)
		relParts []string
		want     manifest.Tree
	}{
		{
			name: "single file",
			buildDir: func(t *testing.T) (string, os.FileInfo) {
				t.Helper()
				dir := t.TempDir()
				path := filepath.Join(dir, "zshrc")
				if err := os.WriteFile(path, []byte("set -o vi\n"), 0o644); err != nil {
					t.Fatalf("WriteFile() error = %v", err)
				}
				info, err := os.Lstat(path)
				if err != nil {
					t.Fatalf("Lstat() error = %v", err)
				}
				return path, info
			},
			relParts: []string{".zshrc"},
			want: manifest.Tree{
				".zshrc": manifest.FileNode("copy"),
			},
		},
		{
			name: "populated directory",
			buildDir: func(t *testing.T) (string, os.FileInfo) {
				t.Helper()
				dir := t.TempDir()
				root := filepath.Join(dir, "kitty")
				if err := os.MkdirAll(root, 0o755); err != nil {
					t.Fatalf("MkdirAll() error = %v", err)
				}
				path := filepath.Join(root, "kitty.conf")
				if err := os.WriteFile(path, []byte("font_size 12\n"), 0o644); err != nil {
					t.Fatalf("WriteFile() error = %v", err)
				}
				info, err := os.Lstat(root)
				if err != nil {
					t.Fatalf("Lstat() error = %v", err)
				}
				return root, info
			},
			relParts: []string{".config", "kitty"},
			want: manifest.Tree{
				".config": manifest.DirectoryNode(nil, manifest.Tree{
					"kitty": manifest.DirectoryNode(nil, manifest.Tree{
						"kitty.conf": manifest.FileNode("copy"),
					}),
				}),
			},
		},
		{
			name: "empty directory",
			buildDir: func(t *testing.T) (string, os.FileInfo) {
				t.Helper()
				dir := t.TempDir()
				root := filepath.Join(dir, "empty")
				if err := os.MkdirAll(root, 0o755); err != nil {
					t.Fatalf("MkdirAll() error = %v", err)
				}
				info, err := os.Lstat(root)
				if err != nil {
					t.Fatalf("Lstat() error = %v", err)
				}
				return root, info
			},
			relParts: []string{".config", "empty"},
			want: manifest.Tree{
				".config": manifest.DirectoryNode(nil, manifest.Tree{
					"empty": manifest.DirectoryNode(nil, nil),
				}),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path, info := tt.buildDir(t)
			entries, err := buildEntries(path, tt.relParts, info)
			if err != nil {
				t.Fatalf("buildEntries() error = %v", err)
			}

			tree := manifest.Tree{}
			for _, entry := range entries {
				if err := insertEntry(tree, entry.Parts, entry.Value); err != nil {
					t.Fatalf("insertEntry() error = %v", err)
				}
			}

			if len(tree) != len(tt.want) {
				t.Fatalf("len(tree) = %d, want %d", len(tree), len(tt.want))
			}
			for key, want := range tt.want {
				got, ok := tree[key]
				if !ok {
					t.Fatalf("tree missing key %q", key)
				}
				if !nodesEqual(got, want) {
					t.Fatalf("tree[%q] = %#v, want %#v", key, got, want)
				}
			}
		})
	}
}

func nodesEqual(a, b manifest.Node) bool {
	if a.IsFile() != b.IsFile() {
		return false
	}
	if a.IsFile() {
		if len(a.File) != len(b.File) {
			return false
		}
		for i := range a.File {
			if a.File[i] != b.File[i] {
				return false
			}
		}
		return true
	}
	if len(a.Dir.Flags) != len(b.Dir.Flags) {
		return false
	}
	for i := range a.Dir.Flags {
		if a.Dir.Flags[i] != b.Dir.Flags[i] {
			return false
		}
	}
	if len(a.Dir.Tree) != len(b.Dir.Tree) {
		return false
	}
	for key, childA := range a.Dir.Tree {
		childB, ok := b.Dir.Tree[key]
		if !ok || !nodesEqual(childA, childB) {
			return false
		}
	}
	return true
}
