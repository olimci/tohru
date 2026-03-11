package manifest

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"
)

const SchemaVersion = 1

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
	Source   string           `json:"source"`
	Dest     string           `json:"dest"`
	Defaults *Defaults        `json:"defaults,omitempty"`
	Entries  map[string]Entry `json:"entries,omitempty"`
}

type Defaults struct {
	Type  string `json:"type,omitempty"`
	Track *bool  `json:"track,omitempty"`
}

type Entry struct {
	Type     string           `json:"type,omitempty"`
	Track    *bool            `json:"track,omitempty"`
	Defaults *Defaults        `json:"defaults,omitempty"`
	Entries  map[string]Entry `json:"entries,omitempty"`
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
	if len(r.Entries) > 0 {
		if err := compileEntries(&links, &files, &dirs, source, dest, nil, defaults, r.Entries); err != nil {
			return nil, nil, nil, err
		}
	}

	return links, files, dirs, nil
}

func compileEntries(links *[]Link, files *[]File, dirs *[]Dir, sourceRoot, destRoot string, parts []string, defaults Defaults, entries map[string]Entry) error {
	keys := make([]string, 0, len(entries))
	for key := range entries {
		keys = append(keys, key)
	}
	slices.Sort(keys)

	for _, key := range keys {
		entryPath := append(append([]string(nil), parts...), key)
		entry := entries[key]
		pathLabel := formatTreePath(entryPath)

		if entry.Defaults != nil && len(entry.Entries) == 0 {
			return fmt.Errorf("entries.%s.defaults: defaults require nested entries", pathLabel)
		}

		entryType := strings.ToLower(strings.TrimSpace(entry.Type))
		entryDefaults := mergeDefaults(defaults, entry.Defaults)
		hasChildren := len(entry.Entries) > 0

		if hasChildren {
			switch entryType {
			case "", "dir":
			default:
				return fmt.Errorf("entries.%s.type: unsupported value %q for an entry with children", pathLabel, entry.Type)
			}

			if entry.Track != nil && entryType == "" {
				return fmt.Errorf("entries.%s.track: track requires type \"dir\" when nested entries are present", pathLabel)
			}

			if entryType == "dir" {
				*dirs = append(*dirs, Dir{
					Path:    filepath.Join(append([]string{destRoot}, entryPath...)...),
					Tracked: pickTrack(defaults.Track, entry.Track),
				})
			}

			if err := compileEntries(links, files, dirs, sourceRoot, destRoot, entryPath, entryDefaults, entry.Entries); err != nil {
				return err
			}
			continue
		}

		effectiveType := entryType
		if effectiveType == "" {
			effectiveType = strings.ToLower(strings.TrimSpace(entryDefaults.Type))
		}
		if effectiveType == "" {
			return fmt.Errorf("entries.%s.type: value is required", pathLabel)
		}

		tracked := pickTrack(entryDefaults.Track, entry.Track)
		dst := filepath.Join(append([]string{destRoot}, entryPath...)...)

		switch effectiveType {
		case "copy":
			*files = append(*files, File{
				Source:  SourcePath(sourceRoot, entryPath),
				Dest:    dst,
				Tracked: tracked,
			})
		case "link":
			if tracked != nil && !*tracked {
				return fmt.Errorf("entries.%s.track: track=false is not supported for link entries", pathLabel)
			}
			*links = append(*links, Link{
				To:   SourcePath(sourceRoot, entryPath),
				From: dst,
			})
		case "dir":
			*dirs = append(*dirs, Dir{
				Path:    dst,
				Tracked: tracked,
			})
		default:
			return fmt.Errorf("entries.%s.type: unsupported value %q (expected \"copy\", \"link\", or \"dir\")", pathLabel, entry.Type)
		}
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
