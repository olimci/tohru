package manifest

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
)

// Manifest represents a configuration file for a Tohru dotfiles source
type Manifest struct {
	Tohru   Tohru   `toml:"tohru"`   // application metadata
	Profile Profile `toml:"profile"` // profile metadata

	Trees []Tree `toml:"tree"`

	Resolved Resolved `toml:"-"`
}

type Tohru struct {
	Version string `toml:"version"` // check this if version is compatible probably semver
}

type Profile struct {
	Slug        string `toml:"slug"`
	Name        string `toml:"name"`
	Description string `toml:"description"`
}

type Tree struct {
	Source string         `toml:"source"`
	Dest   string         `toml:"dest"`
	Files  map[string]any `toml:"files"`
}

type Resolved struct {
	Links []Link
	Files []File
	Dirs  []Dir
}

type Link struct {
	// Link is a symbolic link from somewhere else to something here
	To   string `toml:"to"`
	From string `toml:"from"`
}

type File struct {
	// File is a copy of a file from somewhere here to somewhere else
	Source  string `toml:"source"`
	Dest    string `toml:"dest"`
	Tracked *bool  `toml:"tracked,omitempty"` // nil defaults to true
}

type Dir struct {
	// Dirs don't need a source
	Path    string `toml:"path"`
	Tracked *bool  `toml:"tracked,omitempty"` // nil defaults to true
}

func (m *Manifest) ResolveDefaults() error {
	links := make([]Link, 0, 16)
	files := make([]File, 0, 16)
	dirs := make([]Dir, 0, 8)

	for i, tree := range m.Trees {
		treeLinks, treeFiles, treeDirs, err := tree.compile(i)
		if err != nil {
			return err
		}
		links = append(links, treeLinks...)
		files = append(files, treeFiles...)
		dirs = append(dirs, treeDirs...)
	}

	m.Resolved = Resolved{
		Links: links,
		Files: files,
		Dirs:  dirs,
	}
	return nil
}

type Leaf struct {
	Mode    string
	Kind    string
	Tracked *bool
}

func (t Tree) compile(index int) ([]Link, []File, []Dir, error) {
	source := strings.TrimSpace(t.Source)
	if source == "" {
		return nil, nil, nil, fmt.Errorf("tree[%d].source: value is required", index)
	}
	dest := strings.TrimSpace(t.Dest)
	if dest == "" {
		return nil, nil, nil, fmt.Errorf("tree[%d].dest: value is required", index)
	}

	var (
		links = make([]Link, 0)
		files = make([]File, 0)
		dirs  = make([]Dir, 0)
	)

	var walk func(node map[string]any, parts []string) error
	walk = func(node map[string]any, parts []string) error {
		keys := make([]string, 0, len(node))
		for key := range node {
			keys = append(keys, key)
		}
		sort.Strings(keys)

		for _, key := range keys {
			entryPath := append(append([]string(nil), parts...), key)
			raw := node[key]

			spec, isLeaf, err := decodeLeafSpec(raw)
			if err != nil {
				return fmt.Errorf("tree[%d].files.%s: %w", index, formatTreePath(entryPath), err)
			}
			if isLeaf {
				if err := appendLeaf(&links, &files, &dirs, source, dest, entryPath, spec); err != nil {
					return fmt.Errorf("tree[%d].files.%s: %w", index, formatTreePath(entryPath), err)
				}
				continue
			}

			child, ok := raw.(map[string]any)
			if !ok {
				return fmt.Errorf("tree[%d].files.%s: expected file spec or nested table", index, formatTreePath(entryPath))
			}

			// Empty nested tables represent explicit empty directory entries.
			if len(child) == 0 {
				destParts := append([]string{dest}, entryPath...)
				dirs = append(dirs, Dir{
					Path: filepath.Join(destParts...),
				})
				continue
			}

			if err := walk(child, entryPath); err != nil {
				return err
			}
		}
		return nil
	}

	if len(t.Files) > 0 {
		if err := walk(t.Files, nil); err != nil {
			return nil, nil, nil, err
		}
	}

	return links, files, dirs, nil
}

func decodeLeafSpec(raw any) (Leaf, bool, error) {
	switch value := raw.(type) {
	case string:
		return Leaf{Mode: value}, true, nil
	case map[string]any:
		if len(value) == 0 {
			return Leaf{}, false, nil
		}

		hasSpecKey := false
		hasNonSpecKey := false
		for key := range value {
			switch strings.ToLower(strings.TrimSpace(key)) {
			case "mode", "kind", "tracked":
				hasSpecKey = true
			default:
				hasNonSpecKey = true
			}
		}
		if !hasSpecKey {
			return Leaf{}, false, nil
		}
		if hasNonSpecKey {
			return Leaf{}, false, fmt.Errorf("cannot mix spec keys (mode/kind/tracked) with nested keys")
		}

		spec := Leaf{}
		for key, rawField := range value {
			switch strings.ToLower(strings.TrimSpace(key)) {
			case "mode":
				mode, ok := rawField.(string)
				if !ok {
					return Leaf{}, false, fmt.Errorf("mode must be a string")
				}
				spec.Mode = mode
			case "kind":
				kind, ok := rawField.(string)
				if !ok {
					return Leaf{}, false, fmt.Errorf("kind must be a string")
				}
				spec.Kind = kind
			case "tracked":
				tracked, ok := rawField.(bool)
				if !ok {
					return Leaf{}, false, fmt.Errorf("tracked must be a boolean")
				}
				trackedCopy := tracked
				spec.Tracked = &trackedCopy
			}
		}
		return spec, true, nil
	default:
		return Leaf{}, false, fmt.Errorf("unsupported value type %T", raw)
	}
}

func appendLeaf(links *[]Link, files *[]File, dirs *[]Dir, sourceRoot, destRoot string, parts []string, spec Leaf) error {
	kind := strings.ToLower(strings.TrimSpace(spec.Kind))
	if kind == "" {
		kind = "file"
	}

	switch kind {
	case "file":
		mode := strings.ToLower(strings.TrimSpace(spec.Mode))
		if mode == "" {
			return fmt.Errorf("mode is required for file entries")
		}

		dstParts := append([]string{destRoot}, parts...)
		dst := filepath.Join(dstParts...)

		switch mode {
		case "link":
			if spec.Tracked != nil {
				return fmt.Errorf("tracked is not supported for link entries")
			}
			*links = append(*links, Link{
				To:   SourcePath(sourceRoot, parts),
				From: dst,
			})
		case "copy":
			*files = append(*files, File{
				Source:  SourcePath(sourceRoot, parts),
				Dest:    dst,
				Tracked: spec.Tracked,
			})
		default:
			return fmt.Errorf("unsupported mode %q (expected \"link\" or \"copy\")", spec.Mode)
		}
	case "dir":
		if strings.TrimSpace(spec.Mode) != "" {
			return fmt.Errorf("mode is not supported for dir entries")
		}
		*dirs = append(*dirs, Dir{
			Path:    filepath.Join(append([]string{destRoot}, parts...)...),
			Tracked: spec.Tracked,
		})
	default:
		return fmt.Errorf("unsupported kind %q (expected \"file\" or \"dir\")", spec.Kind)
	}
	return nil
}

func formatTreePath(parts []string) string {
	if len(parts) == 0 {
		return ""
	}
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if part == "" {
			out = append(out, `""`)
			continue
		}
		out = append(out, part)
	}
	return strings.Join(out, ".")
}
