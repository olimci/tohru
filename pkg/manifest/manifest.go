package manifest

import (
	"fmt"
	"maps"
	"path/filepath"
	"slices"
	"strings"
)

// Manifest represents a configuration file for a Tohru dotfiles source
type Manifest struct {
	Tohru   Tohru   `json:"tohru"`   // application metadata
	Profile Profile `json:"profile"` // profile metadata

	Trees map[string]Tree `json:"trees"` // source -> tree definition

	Plan Plan `json:"-"`
}

type Tohru struct {
	Version string `json:"version"` // check this if version is compatible probably semver
}

type Profile struct {
	Slug        string `json:"slug"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

type Tree struct {
	Dest  string         `json:"dest"`
	Files map[string]any `json:"files,omitempty"`
}

type Plan struct {
	Links []Link
	Files []File
	Dirs  []Dir
}

type Link struct {
	// Link is a symbolic link from somewhere else to something here
	To   string `json:"to"`
	From string `json:"from"`
}

type File struct {
	// File is a copy of a file from somewhere here to somewhere else
	Source  string `json:"source"`
	Dest    string `json:"dest"`
	Tracked *bool  `json:"tracked,omitempty"` // nil defaults to true
}

type Dir struct {
	// Dirs don't need a source
	Path    string `json:"path"`
	Tracked *bool  `json:"tracked,omitempty"` // nil defaults to true
}

func (m *Manifest) Resolve() error {
	links := make([]Link, 0, 16)
	files := make([]File, 0, 16)
	dirs := make([]Dir, 0, 8)

	for _, source := range slices.Sorted(maps.Keys(m.Trees)) {
		tree := m.Trees[source]

		treeLinks, treeFiles, treeDirs, err := tree.compile(source)
		if err != nil {
			return err
		}
		links = append(links, treeLinks...)
		files = append(files, treeFiles...)
		dirs = append(dirs, treeDirs...)
	}

	m.Plan = Plan{
		Links: links,
		Files: files,
		Dirs:  dirs,
	}
	return nil
}

func (t Tree) compile(source string) ([]Link, []File, []Dir, error) {
	source = strings.TrimSpace(source)
	if source == "" {
		return nil, nil, nil, fmt.Errorf("trees: source key is empty")
	}

	dest := strings.TrimSpace(t.Dest)
	if dest == "" {
		return nil, nil, nil, fmt.Errorf("trees.%q.dest: value is required", source)
	}

	var (
		links = make([]Link, 0)
		files = make([]File, 0)
		dirs  = make([]Dir, 0)
	)

	var walk func(node map[string]any, parts []string) error
	walk = func(node map[string]any, parts []string) error {
		for _, key := range slices.Sorted(maps.Keys(node)) {
			entryPath := append(append([]string(nil), parts...), key)
			raw := node[key]

			spec, isLeaf, err := DecodeLeaf(raw)
			if err != nil {
				return fmt.Errorf("trees.%q.files.%s: %w", source, formatTreePath(entryPath), err)
			}
			if isLeaf {
				if err := addLeaf(&links, &files, &dirs, source, dest, entryPath, spec); err != nil {
					return fmt.Errorf("trees.%q.files.%s: %w", source, formatTreePath(entryPath), err)
				}
				continue
			}

			child := raw.(map[string]any)

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

func addLeaf(links *[]Link, files *[]File, dirs *[]Dir, sourceRoot, destRoot string, parts []string, spec Leaf) error {
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
