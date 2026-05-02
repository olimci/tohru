package manifest

import (
	"bytes"
	"encoding/json"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
)

const SchemaVersion = 1

const (
	flagCopy      = "copy"
	flagLink      = "link"
	flagTracked   = "tracked"
	flagUntracked = "untracked"
)

var flagOrder = map[string]int{
	flagCopy:      0,
	flagLink:      1,
	flagTracked:   2,
	flagUntracked: 3,
}

// Manifest represents a configuration file for a Tohru dotfiles source.
type Manifest struct {
	Schema   int      `json:"schema"`
	Requires Requires `json:"requires,omitempty"`
	Profile  Profile  `json:"profile"`
	Roots    []Root   `json:"roots,omitempty"`

	Plan Plan `json:"-"`
}

type Requires struct {
	Tohru string `json:"tohru,omitempty"`
}

type Profile struct {
	Slug        string `json:"slug"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

type Root struct {
	Source   string    `json:"source"`
	Dest     string    `json:"dest"`
	Defaults *Defaults `json:"defaults,omitempty"`
	Tree     Tree      `json:"tree,omitempty"`
}

type Defaults struct {
	Type  string `json:"type,omitempty"`
	Track *bool  `json:"track,omitempty"`
}

type Tree map[string]Node

type Node struct {
	File []string
	Dir  *DirNode
}

type DirNode struct {
	Flags []string
	Tree  Tree
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

func FileNode(flags ...string) Node {
	return Node{File: normalizeFlags(flags)}
}

func DirectoryNode(flags []string, tree Tree) Node {
	return Node{
		Dir: &DirNode{
			Flags: normalizeFlags(flags),
			Tree:  cloneTree(tree),
		},
	}
}

func (n Node) IsDir() bool {
	return n.Dir != nil
}

func (n Node) IsFile() bool {
	return n.Dir == nil
}

func (n Node) MarshalJSON() ([]byte, error) {
	if n.Dir == nil {
		return json.Marshal(normalizeFlags(n.File))
	}

	payload := map[string]any{}
	if len(n.Dir.Flags) > 0 {
		payload["."] = normalizeFlags(n.Dir.Flags)
	}

	keys := make([]string, 0, len(n.Dir.Tree))
	for key := range n.Dir.Tree {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	for _, key := range keys {
		payload[key] = n.Dir.Tree[key]
	}

	return json.Marshal(payload)
}

func (n *Node) UnmarshalJSON(data []byte) error {
	data = bytes.TrimSpace(data)
	if len(data) == 0 {
		return fmt.Errorf("node: value is required")
	}

	switch data[0] {
	case '[':
		var flags []string
		if err := json.Unmarshal(data, &flags); err != nil {
			return err
		}
		n.File = normalizeFlags(flags)
		n.Dir = nil
		return nil
	case '{':
		var raw map[string]json.RawMessage
		if err := json.Unmarshal(data, &raw); err != nil {
			return err
		}

		dir := &DirNode{Tree: Tree{}}
		keys := make([]string, 0, len(raw))
		for key := range raw {
			keys = append(keys, key)
		}
		slices.Sort(keys)

		for _, key := range keys {
			if key == "." {
				if err := json.Unmarshal(raw[key], &dir.Flags); err != nil {
					return fmt.Errorf("decode directory metadata: %w", err)
				}
				dir.Flags = normalizeFlags(dir.Flags)
				continue
			}

			var child Node
			if err := json.Unmarshal(raw[key], &child); err != nil {
				return fmt.Errorf("decode child %q: %w", key, err)
			}
			dir.Tree[key] = child
		}

		n.File = nil
		n.Dir = dir
		return nil
	default:
		return fmt.Errorf("node: expected array or object")
	}
}

func (m *Manifest) Resolve() error {
	if m.Schema != SchemaVersion {
		return fmt.Errorf("schema: unsupported value %d (expected %d)", m.Schema, SchemaVersion)
	}

	links := make([]Link, 0, 16)
	files := make([]File, 0, 16)
	dirs := make([]Dir, 0, 8)

	for i, root := range m.Roots {
		rootLinks, rootFiles, rootDirs, err := root.compile()
		if err != nil {
			return fmt.Errorf("roots[%d]: %w", i, err)
		}
		links = append(links, rootLinks...)
		files = append(files, rootFiles...)
		dirs = append(dirs, rootDirs...)
	}

	m.Plan = Plan{
		Links: links,
		Files: files,
		Dirs:  dirs,
	}
	return nil
}

func (r Root) compile() ([]Link, []File, []Dir, error) {
	source := strings.TrimSpace(r.Source)
	if source == "" {
		return nil, nil, nil, fmt.Errorf("source: value is required")
	}

	dest := strings.TrimSpace(r.Dest)
	if dest == "" {
		return nil, nil, nil, fmt.Errorf("dest: value is required")
	}

	var (
		links = make([]Link, 0)
		files = make([]File, 0)
		dirs  = make([]Dir, 0)
	)

	defaults := mergeDefaults(Defaults{}, r.Defaults)
	if _, exists := r.Tree["."]; exists {
		return nil, nil, nil, fmt.Errorf("tree.\".\": reserved key is not allowed at the root level")
	}
	if len(r.Tree) > 0 {
		if err := compileTree(&links, &files, &dirs, source, dest, nil, defaults, r.Tree); err != nil {
			return nil, nil, nil, err
		}
	}

	return links, files, dirs, nil
}

func compileTree(links *[]Link, files *[]File, dirs *[]Dir, sourceRoot, destRoot string, parts []string, defaults Defaults, tree Tree) error {
	keys := make([]string, 0, len(tree))
	for key := range tree {
		keys = append(keys, key)
	}
	slices.Sort(keys)

	for _, key := range keys {
		node := tree[key]
		entryPath := append(append([]string(nil), parts...), key)
		pathLabel := formatTreePath(entryPath)

		if node.IsDir() {
			flags := node.Dir.Flags
			typeFlag, trackOverride, err := flagsForNode(flags, true, pathLabel)
			if err != nil {
				return err
			}
			if typeFlag != "" {
				return fmt.Errorf("tree.%s.\".\": type flags are not supported for directory metadata", pathLabel)
			}

			if len(node.Dir.Tree) == 0 || trackOverride != nil {
				*dirs = append(*dirs, Dir{
					Path:    filepath.Join(append([]string{destRoot}, entryPath...)...),
					Tracked: pickTrack(defaults.Track, trackOverride),
				})
			}

			if err := compileTree(links, files, dirs, sourceRoot, destRoot, entryPath, defaults, node.Dir.Tree); err != nil {
				return err
			}
			continue
		}

		typeFlag, trackOverride, err := flagsForNode(node.File, false, pathLabel)
		if err != nil {
			return err
		}

		effectiveType := typeFlag
		if effectiveType == "" {
			effectiveType = strings.ToLower(strings.TrimSpace(defaults.Type))
		}
		if effectiveType == "" {
			return fmt.Errorf("tree.%s: file type is required", pathLabel)
		}

		tracked := pickTrack(defaults.Track, trackOverride)
		dst := filepath.Join(append([]string{destRoot}, entryPath...)...)

		switch effectiveType {
		case flagCopy:
			*files = append(*files, File{
				Source:  SourcePath(sourceRoot, entryPath),
				Dest:    dst,
				Tracked: tracked,
			})
		case flagLink:
			if tracked != nil && !*tracked {
				return fmt.Errorf("tree.%s: untracked is not supported for link entries", pathLabel)
			}
			*links = append(*links, Link{
				To:   SourcePath(sourceRoot, entryPath),
				From: dst,
			})
		default:
			return fmt.Errorf("tree.%s: unsupported file type %q (expected %q or %q)", pathLabel, effectiveType, flagCopy, flagLink)
		}
	}

	return nil
}

func flagsForNode(flags []string, isDir bool, pathLabel string) (string, *bool, error) {
	var (
		typeFlag      string
		trackOverride *bool
		seen          = map[string]struct{}{}
	)

	for _, raw := range flags {
		flag := strings.ToLower(strings.TrimSpace(raw))
		if flag == "" {
			return "", nil, fmt.Errorf("tree.%s: flags may not be empty", pathLabel)
		}
		if _, exists := seen[flag]; exists {
			return "", nil, fmt.Errorf("tree.%s: duplicate flag %q", pathLabel, flag)
		}
		seen[flag] = struct{}{}

		switch flag {
		case flagCopy, flagLink:
			if isDir {
				return "", nil, fmt.Errorf("tree.%s: flag %q is only valid on files", pathLabel, flag)
			}
			if typeFlag != "" {
				return "", nil, fmt.Errorf("tree.%s: conflicting type flags %q and %q", pathLabel, typeFlag, flag)
			}
			typeFlag = flag
		case flagTracked:
			if trackOverride != nil && !*trackOverride {
				return "", nil, fmt.Errorf("tree.%s: conflicting tracking flags %q and %q", pathLabel, flagTracked, flagUntracked)
			}
			v := true
			trackOverride = &v
		case flagUntracked:
			if trackOverride != nil && *trackOverride {
				return "", nil, fmt.Errorf("tree.%s: conflicting tracking flags %q and %q", pathLabel, flagTracked, flagUntracked)
			}
			v := false
			trackOverride = &v
		default:
			return "", nil, fmt.Errorf("tree.%s: unsupported flag %q", pathLabel, flag)
		}
	}

	return typeFlag, trackOverride, nil
}

func normalizeFlags(flags []string) []string {
	if len(flags) == 0 {
		return nil
	}

	out := append([]string(nil), flags...)
	for i := range out {
		out[i] = strings.ToLower(strings.TrimSpace(out[i]))
	}
	slices.SortFunc(out, func(a, b string) int {
		ai, aok := flagOrder[a]
		bi, bok := flagOrder[b]
		switch {
		case aok && bok:
			return ai - bi
		case aok:
			return -1
		case bok:
			return 1
		default:
			return strings.Compare(a, b)
		}
	})
	return out
}

func cloneTree(tree Tree) Tree {
	if tree == nil {
		return nil
	}
	out := make(Tree, len(tree))
	for key, node := range tree {
		out[key] = cloneNode(node)
	}
	return out
}

func cloneNode(node Node) Node {
	if node.Dir == nil {
		return Node{File: append([]string(nil), node.File...)}
	}
	return Node{
		Dir: &DirNode{
			Flags: append([]string(nil), node.Dir.Flags...),
			Tree:  cloneTree(node.Dir.Tree),
		},
	}
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

func mergeDefaults(base Defaults, override *Defaults) Defaults {
	out := Defaults{
		Type:  base.Type,
		Track: cloneBool(base.Track),
	}
	if override == nil {
		return out
	}
	if strings.TrimSpace(override.Type) != "" {
		out.Type = override.Type
	}
	if override.Track != nil {
		out.Track = cloneBool(override.Track)
	}
	return out
}

func pickTrack(base, override *bool) *bool {
	if override != nil {
		return cloneBool(override)
	}
	return cloneBool(base)
}

func cloneBool(v *bool) *bool {
	if v == nil {
		return nil
	}
	out := *v
	return &out
}
