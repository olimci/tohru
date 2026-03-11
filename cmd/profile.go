package cmd

import (
	"context"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"

	"github.com/olimci/tohru/pkg/manifest"
	"github.com/olimci/tohru/pkg/store"
	"github.com/olimci/tohru/pkg/store/state"
	"github.com/olimci/tohru/pkg/utils/fileutils"
	"github.com/olimci/tohru/pkg/utils/profileutils"
	"github.com/olimci/tohru/pkg/version"
	"github.com/urfave/cli/v3"
)

var writeManifest = manifest.Write

func profileCommand() *cli.Command {
	return &cli.Command{
		Name:  "profile",
		Usage: "profile management",
		Commands: []*cli.Command{
			{
				Name:    "list",
				Aliases: []string{"ls"},
				Usage:   "list cached profiles",
				Action:  profileListAction,
			},
			{
				Name:      "new",
				Usage:     "create a new profile directory with an empty manifest",
				ArgsUsage: "<slug>",
				Action:    profileNewAction,
			},
			{
				Name:      "add",
				Usage:     "copy a local path into a profile and add manifest entries",
				ArgsUsage: "<slug> <path>",
				Action:    profileAddAction,
			},
			{
				Name:      "tidy",
				Usage:     "merge nested roots in a profile manifest",
				ArgsUsage: "<slug>",
				Action:    profileTidyAction,
			},
		},
		Action: profileAction,
	}
}

func profileAction(_ context.Context, cmd *cli.Command) error {
	if len(cmd.Args().Slice()) > 0 {
		return fmt.Errorf("unknown profile subcommand")
	}
	return fmt.Errorf("profile requires a subcommand (try: profile list|new|add|tidy)")
}

func profileListAction(_ context.Context, cmd *cli.Command) error {
	args := cmd.Args().Slice()
	if len(args) > 0 {
		return fmt.Errorf("profile list does not accept arguments")
	}

	s, err := store.DefaultStore()
	if err != nil {
		return err
	}

	profiles, err := s.LoadProfiles()
	if err != nil {
		return err
	}
	if len(profiles) == 0 {
		fmt.Println("No cached profiles")
		return nil
	}

	fmt.Println("Cached profiles:")
	for _, slug := range slices.Sorted(maps.Keys(profiles)) {
		p := profiles[slug]
		name := strings.TrimSpace(p.Name)
		loc := strings.TrimSpace(p.Path)

		if name != "" {
			fmt.Printf("  %s (%s) -> %s\n", slug, name, loc)
			continue
		}
		fmt.Printf("  %s -> %s\n", slug, loc)
	}

	return nil
}

func profileNewAction(_ context.Context, cmd *cli.Command) error {
	args := cmd.Args().Slice()
	if len(args) != 1 {
		return fmt.Errorf("profile new requires exactly one slug argument")
	}

	slug, err := profileutils.ValidateSlug(args[0], "profile slug", false)
	if err != nil {
		return err
	}

	s, err := store.DefaultStore()
	if err != nil {
		return err
	}

	guard, err := s.Lock()
	if err != nil {
		return err
	}
	defer guard.Unlock()

	profiles, err := s.LoadProfiles()
	if err != nil {
		return err
	}
	if _, exists := profiles[slug]; exists {
		return fmt.Errorf("profile %q already exists in cache", slug)
	}

	profileDir := filepath.Join(s.ProfilesPath(), slug)
	if _, err := os.Stat(profileDir); err == nil {
		return fmt.Errorf("profile directory already exists: %s", profileDir)
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("stat profile directory %s: %w", profileDir, err)
	}

	if err := os.MkdirAll(profileDir, 0o755); err != nil {
		return fmt.Errorf("create profile directory %s: %w", profileDir, err)
	}

	manifestPath := filepath.Join(profileDir, manifest.Name)
	if err := manifest.Write(manifestPath, defaultManifest(slug)); err != nil {
		return fmt.Errorf("write profile manifest %s: %w", manifestPath, err)
	}

	profiles[slug] = state.CachedProfile{
		Slug: slug,
		Name: slug,
		Path: profileDir,
	}
	if err := s.SaveProfiles(profiles); err != nil {
		return fmt.Errorf("save profiles cache: %w", err)
	}

	fmt.Printf("created profile %s at %s\n", slug, profileDir)
	printChanges(cmd, []string{profileDir, manifestPath, s.ProfilesFilePath()})
	return nil
}

func profileAddAction(_ context.Context, cmd *cli.Command) error {
	args := cmd.Args().Slice()
	if len(args) != 2 {
		return fmt.Errorf("profile add requires exactly two arguments: <slug> <path>")
	}

	slug, err := profileutils.ValidateSlug(args[0], "profile slug", false)
	if err != nil {
		return err
	}
	localPath, err := fileutils.AbsPath(args[1])
	if err != nil {
		return err
	}

	info, err := os.Lstat(localPath)
	if err != nil {
		return fmt.Errorf("stat source path %s: %w", localPath, err)
	}

	s, err := store.DefaultStore()
	if err != nil {
		return err
	}

	guard, err := s.Lock()
	if err != nil {
		return err
	}
	defer guard.Unlock()

	changed, err := addPath(s, slug, localPath, info)
	if err != nil {
		return err
	}

	fmt.Printf("added %s to profile %s\n", localPath, slug)
	printChanges(cmd, changed)
	return nil
}

func profileTidyAction(_ context.Context, cmd *cli.Command) error {
	args := cmd.Args().Slice()
	if len(args) != 1 {
		return fmt.Errorf("profile tidy requires exactly one slug argument")
	}

	slug, err := profileutils.ValidateSlug(args[0], "profile slug", false)
	if err != nil {
		return err
	}

	s, err := store.DefaultStore()
	if err != nil {
		return err
	}

	guard, err := s.Lock()
	if err != nil {
		return err
	}
	defer guard.Unlock()

	profiles, err := s.LoadProfiles()
	if err != nil {
		return err
	}
	profile, ok := profiles[slug]
	if !ok {
		return fmt.Errorf("profile %q not found in cache", slug)
	}
	profileDir := strings.TrimSpace(profile.Path)
	if profileDir == "" {
		return fmt.Errorf("profile %q has an empty location", slug)
	}

	m, _, err := manifest.Load(profileDir)
	if err != nil {
		return err
	}
	merges, err := m.Tidy()
	if err != nil {
		return err
	}
	if merges == 0 {
		fmt.Printf("profile %s is already tidy\n", slug)
		return nil
	}

	manifestPath := filepath.Join(profileDir, manifest.Name)
	if err := manifest.Write(manifestPath, m); err != nil {
		return err
	}

	fmt.Printf("tidied profile %s (%d merge(s))\n", slug, merges)
	printChanges(cmd, []string{manifestPath})
	return nil
}

func defaultManifest(slug string) manifest.Manifest {
	return manifest.Manifest{
		Schema: manifest.SchemaVersion,
		Requires: manifest.Requires{
			Tohru: version.Version,
		},
		Profile: manifest.Profile{
			Slug:        slug,
			Name:        slug,
			Description: "",
		},
		Roots: []manifest.Root{},
	}
}

type addEntry struct {
	Parts []string
	Value manifest.Entry
}

type copyJob struct {
	Source string
	Dest   string
}

func pickRoot(m *manifest.Manifest, profileDir, targetPath string) (int, string, []string, error) {
	bestIndex := -1
	bestDepth := -1
	bestSourceRoot := ""
	var bestRel []string

	for i, root := range m.Roots {
		sourceRoot, ok, err := rootSourceRoot(profileDir, root.Source)
		if err != nil {
			return -1, "", nil, err
		}
		if !ok {
			continue
		}

		destRoot, err := fileutils.AbsPath(root.Dest)
		if err != nil {
			return -1, "", nil, fmt.Errorf("resolve roots[%d].dest: %w", i, err)
		}
		rel, err := filepath.Rel(destRoot, targetPath)
		if err != nil || fileutils.Escapes(rel) {
			continue
		}
		relParts := fileutils.SplitPathParts(rel)
		if len(relParts) == 0 {
			return -1, "", nil, fmt.Errorf("path %s is equal to roots[%d].dest %s; add a child path instead", targetPath, i, root.Dest)
		}

		depth := fileutils.PathDepth(destRoot)
		if bestIndex == -1 || depth > bestDepth || (depth == bestDepth && root.Source < m.Roots[bestIndex].Source) {
			bestIndex = i
			bestDepth = depth
			bestSourceRoot = sourceRoot
			bestRel = relParts
		}
	}

	if bestIndex != -1 {
		return bestIndex, bestSourceRoot, bestRel, nil
	}

	var source string
	var dest string
	var rel string
	home, err := os.UserHomeDir()
	if err == nil {
		if relHome, relErr := filepath.Rel(home, targetPath); relErr == nil && !fileutils.Escapes(relHome) && relHome != "." {
			source = "home"
			dest = "~"
			rel = relHome
		}
	}
	if rel == "" {
		relRoot, relErr := filepath.Rel(string(filepath.Separator), targetPath)
		if relErr != nil || fileutils.Escapes(relRoot) || relRoot == "." {
			return -1, "", nil, fmt.Errorf("cannot derive root for %s", targetPath)
		}
		source = "root"
		dest = string(filepath.Separator)
		rel = relRoot
	}

	for i, root := range m.Roots {
		if root.Source == source {
			return -1, "", nil, fmt.Errorf("cannot auto-create root for %s: roots[%d] already uses source %q with dest %q", targetPath, i, source, root.Dest)
		}
	}

	m.Roots = append(m.Roots, manifest.Root{
		Source:  source,
		Dest:    dest,
		Entries: map[string]manifest.Entry{},
	})
	return len(m.Roots) - 1, filepath.Join(profileDir, source), fileutils.SplitPathParts(rel), nil
}

func rootSourceRoot(profileDir, source string) (string, bool, error) {
	source = fileutils.ExpandHome(strings.TrimSpace(source))
	if source == "" {
		return "", false, nil
	}

	var root string
	if filepath.IsAbs(source) {
		root = filepath.Clean(source)
	} else {
		root = filepath.Clean(filepath.Join(profileDir, source))
	}

	profileRoot := filepath.Clean(profileDir)
	rel, err := filepath.Rel(profileRoot, root)
	if err != nil {
		return "", false, fmt.Errorf("resolve profile tree source: %w", err)
	}
	if fileutils.Escapes(rel) {
		return "", false, nil
	}
	return root, true, nil
}

func buildEntries(localPath string, relParts []string, info os.FileInfo) ([]addEntry, error) {
	switch {
	case info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0:
		return []addEntry{{Parts: append([]string(nil), relParts...), Value: manifest.Entry{Type: "copy"}}}, nil
	case info.IsDir():
		entries := make([]addEntry, 0, 8)
		err := filepath.WalkDir(localPath, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			rel, err := filepath.Rel(localPath, path)
			if err != nil {
				return err
			}
			if rel == "." {
				return nil
			}

			parts := append(append([]string(nil), relParts...), fileutils.SplitPathParts(rel)...)
			if d.IsDir() {
				empty, err := isEmptyDir(path)
				if err != nil {
					return err
				}
				if empty {
					entries = append(entries, addEntry{Parts: parts, Value: manifest.Entry{Type: "dir"}})
				}
				return nil
			}

			t := d.Type()
			if t.IsRegular() || t&os.ModeSymlink != 0 {
				entries = append(entries, addEntry{Parts: parts, Value: manifest.Entry{Type: "copy"}})
				return nil
			}
			return fmt.Errorf("unsupported file type at %s", path)
		})
		if err != nil {
			return nil, fmt.Errorf("walk source directory %s: %w", localPath, err)
		}
		if len(entries) == 0 {
			entries = append(entries, addEntry{Parts: append([]string(nil), relParts...), Value: manifest.Entry{Type: "dir"}})
		}
		return entries, nil
	default:
		return nil, fmt.Errorf("unsupported source type at %s", localPath)
	}
}

func addPath(s store.Store, slug, localPath string, info os.FileInfo) ([]string, error) {
	profiles, err := s.LoadProfiles()
	if err != nil {
		return nil, err
	}
	profile, ok := profiles[slug]
	if !ok {
		return nil, fmt.Errorf("profile %q not found in cache", slug)
	}
	profileDir := strings.TrimSpace(profile.Path)
	if profileDir == "" {
		return nil, fmt.Errorf("profile %q has an empty location", slug)
	}

	m, _, err := manifest.Load(profileDir)
	if err != nil {
		return nil, err
	}

	rootIndex, sourceRoot, relParts, err := pickRoot(&m, profileDir, localPath)
	if err != nil {
		return nil, err
	}

	entries, err := buildEntries(localPath, relParts, info)
	if err != nil {
		return nil, err
	}
	root := m.Roots[rootIndex]
	if root.Entries == nil {
		root.Entries = map[string]manifest.Entry{}
	}
	for _, entry := range entries {
		if err := insertEntry(root.Entries, entry.Parts, entry.Value); err != nil {
			return nil, err
		}
	}
	m.Roots[rootIndex] = root
	if err := m.Resolve(); err != nil {
		return nil, fmt.Errorf("validate updated manifest: %w", err)
	}

	sourcePaths, rollbackSources, err := copySources(localPath, sourceRoot, relParts, entries, info)
	if err != nil {
		return nil, err
	}

	manifestPath := filepath.Join(profileDir, manifest.Name)
	if err := writeManifest(manifestPath, m); err != nil {
		if rollbackErr := rollbackSources(); rollbackErr != nil {
			return nil, fmt.Errorf("write manifest %s: %w (rollback failed: %v)", manifestPath, err, rollbackErr)
		}
		return nil, fmt.Errorf("write manifest %s: %w (rolled back copied sources)", manifestPath, err)
	}

	return append(sourcePaths, manifestPath), nil
}

func copySources(localPath, sourceRoot string, relParts []string, entries []addEntry, info os.FileInfo) ([]string, func() error, error) {
	jobs, err := copyJobs(localPath, sourceRoot, relParts, entries, info)
	if err != nil {
		return nil, nil, err
	}
	for _, job := range jobs {
		if err := checkMissing(job.Dest); err != nil {
			return nil, nil, err
		}
	}

	changed := make([]string, 0, len(jobs))
	seen := make(map[string]struct{}, len(jobs))
	created := make([]string, 0, len(jobs))
	rollback := func() error {
		return rollbackCopiedProfileSources(sourceRoot, created)
	}
	for _, job := range jobs {
		if err := fileutils.CopyPath(job.Source, job.Dest); err != nil {
			if rollbackErr := rollback(); rollbackErr != nil {
				return nil, nil, fmt.Errorf("copy %s to %s: %w (rollback failed: %v)", job.Source, job.Dest, err, rollbackErr)
			}
			return nil, nil, fmt.Errorf("copy %s to %s: %w (rolled back copied sources)", job.Source, job.Dest, err)
		}
		created = append(created, job.Dest)
		if _, ok := seen[job.Dest]; ok {
			continue
		}
		seen[job.Dest] = struct{}{}
		changed = append(changed, job.Dest)
	}
	slices.Sort(changed)
	return changed, rollback, nil
}

func copyJobs(localPath, sourceRoot string, relParts []string, entries []addEntry, info os.FileInfo) ([]copyJob, error) {
	switch {
	case info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0:
		return []copyJob{{Source: localPath, Dest: manifest.SourcePath(sourceRoot, relParts)}}, nil
	case info.IsDir():
		jobs := make([]copyJob, 0, len(entries))
		for _, entry := range entries {
			if strings.EqualFold(strings.TrimSpace(entry.Value.Type), "dir") {
				continue
			}
			suffix, err := suffixParts(entry.Parts, relParts)
			if err != nil {
				return nil, err
			}
			srcParts := append([]string{localPath}, suffix...)
			jobs = append(jobs, copyJob{
				Source: filepath.Join(srcParts...),
				Dest:   manifest.SourcePath(sourceRoot, entry.Parts),
			})
		}
		return jobs, nil
	default:
		return nil, fmt.Errorf("unsupported source type at %s", localPath)
	}
}

func suffixParts(parts, prefix []string) ([]string, error) {
	if len(parts) < len(prefix) {
		return nil, fmt.Errorf("internal error: cannot derive source suffix for %q", strings.Join(parts, "/"))
	}
	if !slices.Equal(parts[:len(prefix)], prefix) {
		return nil, fmt.Errorf("internal error: path %q does not match expected prefix %q", strings.Join(parts, "/"), strings.Join(prefix, "/"))
	}
	return slices.Clone(parts[len(prefix):]), nil
}

func checkMissing(path string) error {
	if _, err := os.Lstat(path); err == nil {
		return fmt.Errorf("profile source path already exists: %s", path)
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("stat profile source path %s: %w", path, err)
	}
	return nil
}

func rollbackCopiedProfileSources(sourceRoot string, paths []string) error {
	for _, path := range fileutils.SortByDepth(paths, true) {
		if err := fileutils.RemovePath(path); err != nil {
			return fmt.Errorf("remove copied profile source %s: %w", path, err)
		}
		if err := pruneEmptyParents(sourceRoot, filepath.Dir(path)); err != nil {
			return err
		}
	}
	return nil
}

func pruneEmptyParents(sourceRoot, start string) error {
	root := filepath.Clean(sourceRoot)
	cur := filepath.Clean(start)

	for {
		if cur == root || cur == "." || cur == string(filepath.Separator) {
			return nil
		}

		rel, err := filepath.Rel(root, cur)
		if err != nil || fileutils.Escapes(rel) {
			return nil
		}

		if err := os.Remove(cur); err != nil {
			if os.IsNotExist(err) {
				cur = filepath.Dir(cur)
				continue
			}
			return nil
		}

		cur = filepath.Dir(cur)
	}
}

func isEmptyDir(path string) (bool, error) {
	entries, err := os.ReadDir(path)
	if err != nil {
		return false, err
	}
	return len(entries) == 0, nil
}

func insertEntry(root map[string]manifest.Entry, parts []string, value manifest.Entry) error {
	if len(parts) == 0 {
		return fmt.Errorf("cannot add empty path")
	}

	node := root
	for i := 0; i < len(parts)-1; i++ {
		part := parts[i]
		entry, exists := node[part]
		if !exists {
			next := manifest.Entry{Entries: map[string]manifest.Entry{}}
			node[part] = next
			node = next.Entries
			continue
		}
		entryType := strings.ToLower(strings.TrimSpace(entry.Type))
		if entryType == "copy" || entryType == "link" {
			return fmt.Errorf("cannot add %q: %q is already a leaf entry", strings.Join(parts, "."), strings.Join(parts[:i+1], "."))
		}
		if entry.Entries == nil {
			entry.Entries = map[string]manifest.Entry{}
			node[part] = entry
		}
		node = entry.Entries
	}

	leaf := parts[len(parts)-1]
	if existing, exists := node[leaf]; exists {
		if reflect.DeepEqual(existing, value) {
			return nil
		}
		return fmt.Errorf("cannot add %q: path already exists with a different definition", strings.Join(parts, "."))
	}
	node[leaf] = value
	return nil
}
