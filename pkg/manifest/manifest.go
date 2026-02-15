package manifest

import "strings"

// Manifest represents a configuration file for a Tohru dotfiles source
type Manifest struct {
	Tohru  Tohru  `toml:"tohru"`  // application metadata
	Source Source `toml:"source"` // source metadata

	Imports []Import `toml:"import"`
	Links   []Link   `toml:"link"`
	Files   []File   `toml:"file"`
	Dirs    []Dir    `toml:"dir"`
}

type Tohru struct {
	Version string `toml:"version"` // check this if version is compatible probably semver
}

type Source struct {
	Name        string `toml:"name"`
	Description string `toml:"description"`
}

type Import struct {
	Path string   `toml:"path"`
	OS   []string `toml:"os"`
	Arch []string `toml:"arch"`
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

	tracked    bool `toml:"-"`
	trackedSet bool `toml:"-"`
}

type Dir struct {
	// Dirs don't need a source
	Path    string `toml:"path"`
	Tracked *bool  `toml:"tracked,omitempty"` // nil defaults to true

	tracked    bool `toml:"-"`
	trackedSet bool `toml:"-"`
}

func (m *Manifest) ResolveDefaults() {
	for i := range m.Files {
		m.Files[i].resolveTracked()
	}
	for i := range m.Dirs {
		m.Dirs[i].resolveTracked()
	}
}

func (m *Manifest) Merge(other Manifest) {
	if version := strings.TrimSpace(other.Tohru.Version); version != "" {
		m.Tohru.Version = version
	}
	if name := strings.TrimSpace(other.Source.Name); name != "" {
		m.Source.Name = name
	}
	if description := strings.TrimSpace(other.Source.Description); description != "" {
		m.Source.Description = description
	}

	m.Links = append(m.Links, other.Links...)
	m.Files = append(m.Files, other.Files...)
	m.Dirs = append(m.Dirs, other.Dirs...)
}

func (i Import) Applies(goos, goarch string) bool {
	return matchConstraint(i.OS, goos) && matchConstraint(i.Arch, goarch)
}

func matchConstraint(values []string, target string) bool {
	if len(values) == 0 {
		return true
	}
	normalizedTarget := strings.ToLower(strings.TrimSpace(target))
	for _, raw := range values {
		value := strings.ToLower(strings.TrimSpace(raw))
		if value == "" {
			continue
		}
		if value == normalizedTarget {
			return true
		}
	}
	return false
}

func (f File) IsTracked() bool {
	if f.Tracked != nil {
		return *f.Tracked
	}
	if f.trackedSet {
		return f.tracked
	}
	return true
}

func (d Dir) IsTracked() bool {
	if d.Tracked != nil {
		return *d.Tracked
	}
	if d.trackedSet {
		return d.tracked
	}
	return true
}

func (f *File) resolveTracked() {
	f.trackedSet = true
	f.tracked = true
	if f.Tracked != nil {
		f.tracked = *f.Tracked
	}
}

func (d *Dir) resolveTracked() {
	d.trackedSet = true
	d.tracked = true
	if d.Tracked != nil {
		d.tracked = *d.Tracked
	}
}
