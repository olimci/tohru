package manifest

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveStructuralTree(t *testing.T) {
	m := Manifest{
		Schema: 1,
		Profile: Profile{
			Slug: "test",
			Name: "test",
		},
		Roots: []Root{
			{
				Source: "home",
				Dest:   "~",
				Defaults: &Defaults{
					Type: "link",
				},
				Tree: Tree{
					".zshrc": FileNode("copy"),
					".config": DirectoryNode(nil, Tree{
						"kitty": DirectoryNode(nil, Tree{
							"kitty.conf":    FileNode(),
							"kitty.app.png": FileNode("copy", "untracked"),
						}),
						"nvim": DirectoryNode(nil, Tree{
							"after": DirectoryNode([]string{"untracked"}, nil),
						}),
						"empty": DirectoryNode(nil, nil),
					}),
				},
			},
		},
	}

	if err := m.Resolve(); err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}

	if got, want := len(m.Plan.Links), 1; got != want {
		t.Fatalf("len(Links) = %d, want %d", got, want)
	}
	if got, want := len(m.Plan.Files), 2; got != want {
		t.Fatalf("len(Files) = %d, want %d", got, want)
	}
	if got, want := len(m.Plan.Dirs), 2; got != want {
		t.Fatalf("len(Dirs) = %d, want %d", got, want)
	}

	if m.Plan.Links[0].From != filepath.Join("~", ".config", "kitty", "kitty.conf") {
		t.Fatalf("unexpected link destination: %q", m.Plan.Links[0].From)
	}

	var kittyTracked *bool
	for _, file := range m.Plan.Files {
		if file.Dest == filepath.Join("~", ".config", "kitty", "kitty.app.png") {
			kittyTracked = file.Tracked
			break
		}
	}
	if kittyTracked == nil || *kittyTracked {
		t.Fatalf("kitty.app.png should be untracked, got %#v", kittyTracked)
	}

	var afterTracked *bool
	for _, dir := range m.Plan.Dirs {
		if dir.Path == filepath.Join("~", ".config", "nvim", "after") {
			afterTracked = dir.Tracked
			break
		}
	}
	if afterTracked == nil || *afterTracked {
		t.Fatalf("nvim/after should be untracked, got %#v", afterTracked)
	}
}

func TestResolveTrackedOverridesDefaultFalse(t *testing.T) {
	m := Manifest{
		Schema: 1,
		Profile: Profile{
			Slug: "test",
			Name: "test",
		},
		Roots: []Root{
			{
				Source: "home",
				Dest:   "~",
				Defaults: &Defaults{
					Type:  "copy",
					Track: boolPtr(false),
				},
				Tree: Tree{
					".zshrc": FileNode("tracked"),
				},
			},
		},
	}

	if err := m.Resolve(); err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if len(m.Plan.Files) != 1 {
		t.Fatalf("len(Files) = %d, want 1", len(m.Plan.Files))
	}
	if m.Plan.Files[0].Tracked == nil || !*m.Plan.Files[0].Tracked {
		t.Fatalf("tracked override was not applied: %#v", m.Plan.Files[0].Tracked)
	}
}

func TestDecodeManifestRejectsOldEntriesFormat(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, Name)
	payload := `{
  "schema": 1,
  "profile": { "slug": "test", "name": "test", "description": "" },
  "roots": [
    {
      "source": "home",
      "dest": "~",
      "entries": {
        ".zshrc": { "type": "copy" }
      }
    }
  ]
}`
	if err := os.WriteFile(path, []byte(payload), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	_, err := decodeManifest(path)
	if err == nil || !strings.Contains(err.Error(), `unknown field "entries"`) {
		t.Fatalf("decodeManifest() error = %v, want unknown field entries", err)
	}
}

func TestResolveRejectsInvalidTreeFlags(t *testing.T) {
	tests := []struct {
		name    string
		root    Root
		wantErr string
	}{
		{
			name: "untracked link",
			root: Root{
				Source: "home",
				Dest:   "~",
				Defaults: &Defaults{
					Type: "link",
				},
				Tree: Tree{"file": FileNode("untracked")},
			},
			wantErr: "untracked is not supported for link entries",
		},
		{
			name: "duplicate flag",
			root: Root{
				Source: "home",
				Dest:   "~",
				Tree:   Tree{"file": FileNode("copy", "copy")},
			},
			wantErr: `duplicate flag "copy"`,
		},
		{
			name: "directory type flag",
			root: Root{
				Source: "home",
				Dest:   "~",
				Tree: Tree{
					"dir": DirectoryNode([]string{"copy"}, nil),
				},
			},
			wantErr: `flag "copy" is only valid on files`,
		},
		{
			name: "reserved root dot",
			root: Root{
				Source: "home",
				Dest:   "~",
				Tree:   Tree{".": FileNode()},
			},
			wantErr: "reserved key is not allowed at the root level",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := Manifest{
				Schema: 1,
				Profile: Profile{
					Slug: "test",
					Name: "test",
				},
				Roots: []Root{tt.root},
			}
			err := m.Resolve()
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("Resolve() error = %v, want substring %q", err, tt.wantErr)
			}
		})
	}
}

func TestTidyMergesNestedRoots(t *testing.T) {
	m := Manifest{
		Schema: 1,
		Profile: Profile{
			Slug: "test",
			Name: "test",
		},
		Roots: []Root{
			{
				Source: "home",
				Dest:   "~",
				Defaults: &Defaults{
					Type: "copy",
				},
				Tree: Tree{
					"bin": DirectoryNode(nil, Tree{
						"script": FileNode(),
					}),
				},
			},
			{
				Source: "home/bin",
				Dest:   "~/bin",
				Defaults: &Defaults{
					Type: "copy",
				},
				Tree: Tree{
					"tool": FileNode(),
				},
			},
		},
	}

	merges, err := m.Tidy()
	if err != nil {
		t.Fatalf("Tidy() error = %v", err)
	}
	if merges != 1 {
		t.Fatalf("Tidy() merges = %d, want 1", merges)
	}
	if len(m.Roots) != 1 {
		t.Fatalf("len(Roots) = %d, want 1", len(m.Roots))
	}

	bin := m.Roots[0].Tree["bin"]
	if !bin.IsDir() {
		t.Fatalf("bin node should be a directory")
	}
	if _, ok := bin.Dir.Tree["script"]; !ok {
		t.Fatalf("merged tree is missing script")
	}
	if _, ok := bin.Dir.Tree["tool"]; !ok {
		t.Fatalf("merged tree is missing tool")
	}
}

func TestWriteUsesStructuralTreeEncoding(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, Name)
	m := Manifest{
		Schema: 1,
		Profile: Profile{
			Slug: "test",
			Name: "test",
		},
		Roots: []Root{
			{
				Source: "home",
				Dest:   "~",
				Tree: Tree{
					".zshrc": FileNode("copy"),
					".config": DirectoryNode(nil, Tree{
						"empty": DirectoryNode(nil, nil),
					}),
				},
			},
		},
	}

	if err := Write(path, m); err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	text := string(raw)
	if strings.Contains(text, `"entries"`) {
		t.Fatalf("Write() output should not contain entries: %s", text)
	}
	if !strings.Contains(text, `"tree"`) || !strings.Contains(text, `".zshrc": [`) {
		t.Fatalf("Write() output did not use structural encoding: %s", text)
	}
}

func boolPtr(v bool) *bool {
	return &v
}
