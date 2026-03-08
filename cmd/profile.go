package cmd

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"unicode"

	"github.com/BurntSushi/toml"
	"github.com/olimci/tohru/pkg/manifest"
	"github.com/olimci/tohru/pkg/store"
	"github.com/olimci/tohru/pkg/store/lock"
	"github.com/olimci/tohru/pkg/utils/fileutils"
	"github.com/olimci/tohru/pkg/version"
	"github.com/urfave/cli/v3"
)

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
				Usage:     "merge nested tree roots in a profile manifest",
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

	slugs := make([]string, 0, len(profiles))
	for slug := range profiles {
		slugs = append(slugs, slug)
	}
	sort.Strings(slugs)

	fmt.Println("Cached profiles:")
	for _, slug := range slugs {
		p := profiles[slug]
		name := strings.TrimSpace(p.Name)
		loc := strings.TrimSpace(p.Loc)

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

	slug, err := profileSlug(args[0])
	if err != nil {
		return err
	}

	s, err := store.DefaultStore()
	if err != nil {
		return err
	}

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

	manifestPath := filepath.Join(profileDir, "tohru.toml")
	if err := os.WriteFile(manifestPath, []byte(newProfileManifest(slug)), 0o644); err != nil {
		return fmt.Errorf("write profile manifest %s: %w", manifestPath, err)
	}

	profiles[slug] = lock.Profile{
		Slug: slug,
		Name: slug,
		Loc:  profileDir,
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

	slug, err := profileSlug(args[0])
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

	profiles, err := s.LoadProfiles()
	if err != nil {
		return err
	}
	profile, ok := profiles[slug]
	if !ok {
		return fmt.Errorf("profile %q not found in cache", slug)
	}
	profileDir := strings.TrimSpace(profile.Loc)
	if profileDir == "" {
		return fmt.Errorf("profile %q has an empty location", slug)
	}

	m, _, err := manifest.Load(profileDir)
	if err != nil {
		return err
	}

	treeIdx, sourceRoot, relParts, err := pickTreeForAdd(&m, profileDir, localPath)
	if err != nil {
		return err
	}

	entries, err := buildAddEntries(localPath, relParts, info)
	if err != nil {
		return err
	}
	if m.Trees[treeIdx].Files == nil {
		m.Trees[treeIdx].Files = map[string]any{}
	}
	for _, entry := range entries {
		if err := insertEntry(m.Trees[treeIdx].Files, entry.Parts, entry.Value); err != nil {
			return err
		}
	}
	if err := m.ResolveDefaults(); err != nil {
		return fmt.Errorf("validate updated manifest: %w", err)
	}

	sourcePaths, err := copyProfileSources(localPath, sourceRoot, relParts, entries, info)
	if err != nil {
		return err
	}

	manifestPath := filepath.Join(profileDir, "tohru.toml")
	if err := writeManifest(manifestPath, m); err != nil {
		return err
	}

	fmt.Printf("added %s to profile %s\n", localPath, slug)
	changed := append(sourcePaths, manifestPath)
	printChanges(cmd, changed)
	return nil
}

func profileTidyAction(_ context.Context, cmd *cli.Command) error {
	args := cmd.Args().Slice()
	if len(args) != 1 {
		return fmt.Errorf("profile tidy requires exactly one slug argument")
	}

	slug, err := profileSlug(args[0])
	if err != nil {
		return err
	}

	s, err := store.DefaultStore()
	if err != nil {
		return err
	}

	profiles, err := s.LoadProfiles()
	if err != nil {
		return err
	}
	profile, ok := profiles[slug]
	if !ok {
		return fmt.Errorf("profile %q not found in cache", slug)
	}
	profileDir := strings.TrimSpace(profile.Loc)
	if profileDir == "" {
		return fmt.Errorf("profile %q has an empty location", slug)
	}

	m, _, err := manifest.Load(profileDir)
	if err != nil {
		return err
	}
	merges, err := m.TidyTrees()
	if err != nil {
		return err
	}
	if merges == 0 {
		fmt.Printf("profile %s is already tidy\n", slug)
		return nil
	}

	manifestPath := filepath.Join(profileDir, "tohru.toml")
	if err := writeManifest(manifestPath, m); err != nil {
		return err
	}

	fmt.Printf("tidied profile %s (%d merge(s))\n", slug, merges)
	printChanges(cmd, []string{manifestPath})
	return nil
}

func profileSlug(raw string) (string, error) {
	slug := strings.ToLower(strings.TrimSpace(raw))
	if slug == "" {
		return "", fmt.Errorf("profile slug is empty")
	}

	for _, r := range slug {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-' || r == '_' {
			continue
		}
		return "", fmt.Errorf("profile slug %q contains invalid character %q (allowed: letters, digits, '-', '_')", raw, r)
	}

	return slug, nil
}

func newProfileManifest(slug string) string {
	return fmt.Sprintf(
		"[tohru]\nversion = %q\n\n[profile]\nslug = %q\nname = %q\ndescription = %q\n",
		version.Version,
		slug,
		slug,
		"",
	)
}

type addEntry struct {
	Parts []string
	Value any
}

type copyJob struct {
	Source string
	Dest   string
}

func pickTreeForAdd(m *manifest.Manifest, profileDir, targetPath string) (int, string, []string, error) {
	bestIdx := -1
	bestDepth := -1
	var bestSourceRoot string
	var bestRel []string

	for i, tree := range m.Trees {
		sourceRoot, ok, err := profileTreeSourceRoot(profileDir, tree.Source)
		if err != nil {
			return 0, "", nil, err
		}
		if !ok {
			continue
		}

		destRoot, err := fileutils.AbsPath(tree.Dest)
		if err != nil {
			return 0, "", nil, fmt.Errorf("resolve tree[%d].dest: %w", i, err)
		}
		rel, err := filepath.Rel(destRoot, targetPath)
		if err != nil || relEscapes(rel) {
			continue
		}
		relParts := splitParts(rel)
		if len(relParts) == 0 {
			return 0, "", nil, fmt.Errorf("path %s is equal to tree[%d].dest %s; add a child path instead", targetPath, i, tree.Dest)
		}

		depth := pathDepth(destRoot)
		if bestIdx == -1 || depth > bestDepth {
			bestIdx = i
			bestDepth = depth
			bestSourceRoot = sourceRoot
			bestRel = relParts
		}
	}

	if bestIdx != -1 {
		return bestIdx, bestSourceRoot, bestRel, nil
	}

	var source string
	var dest string
	var rel string
	home, err := os.UserHomeDir()
	if err == nil {
		if relHome, relErr := filepath.Rel(home, targetPath); relErr == nil && !relEscapes(relHome) && relHome != "." {
			source = "home"
			dest = "~"
			rel = relHome
		}
	}
	if rel == "" {
		relRoot, relErr := filepath.Rel(string(filepath.Separator), targetPath)
		if relErr != nil || relEscapes(relRoot) || relRoot == "." {
			return 0, "", nil, fmt.Errorf("cannot derive tree root for %s", targetPath)
		}
		source = "root"
		dest = string(filepath.Separator)
		rel = relRoot
	}

	m.Trees = append(m.Trees, manifest.Tree{
		Source: source,
		Dest:   dest,
		Files:  map[string]any{},
	})
	return len(m.Trees) - 1, filepath.Join(profileDir, source), splitParts(rel), nil
}

func profileTreeSourceRoot(profileDir, source string) (string, bool, error) {
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
	if relEscapes(rel) {
		return "", false, nil
	}
	return root, true, nil
}

func buildAddEntries(localPath string, relParts []string, info os.FileInfo) ([]addEntry, error) {
	switch {
	case info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0:
		return []addEntry{{Parts: cloneParts(relParts), Value: map[string]any{"mode": "copy"}}}, nil
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

			parts := append(cloneParts(relParts), splitParts(rel)...)
			if d.IsDir() {
				empty, err := isEmptyDir(path)
				if err != nil {
					return err
				}
				if empty {
					entries = append(entries, addEntry{Parts: parts, Value: map[string]any{"kind": "dir"}})
				}
				return nil
			}

			t := d.Type()
			if t.IsRegular() || t&os.ModeSymlink != 0 {
				entries = append(entries, addEntry{Parts: parts, Value: map[string]any{"mode": "copy"}})
				return nil
			}
			return fmt.Errorf("unsupported file type at %s", path)
		})
		if err != nil {
			return nil, fmt.Errorf("walk source directory %s: %w", localPath, err)
		}
		if len(entries) == 0 {
			entries = append(entries, addEntry{Parts: cloneParts(relParts), Value: map[string]any{"kind": "dir"}})
		}
		return entries, nil
	default:
		return nil, fmt.Errorf("unsupported source type at %s", localPath)
	}
}

func copyProfileSources(localPath, sourceRoot string, relParts []string, entries []addEntry, info os.FileInfo) ([]string, error) {
	jobs, err := buildCopyJobs(localPath, sourceRoot, relParts, entries, info)
	if err != nil {
		return nil, err
	}
	for _, job := range jobs {
		if err := ensureMissingPath(job.Dest); err != nil {
			return nil, err
		}
	}

	changed := make([]string, 0, len(jobs))
	seen := make(map[string]struct{}, len(jobs))
	for _, job := range jobs {
		if err := fileutils.CopyPath(job.Source, job.Dest); err != nil {
			return nil, fmt.Errorf("copy %s to %s: %w", job.Source, job.Dest, err)
		}
		if _, ok := seen[job.Dest]; ok {
			continue
		}
		seen[job.Dest] = struct{}{}
		changed = append(changed, job.Dest)
	}
	sort.Strings(changed)
	return changed, nil
}

func buildCopyJobs(localPath, sourceRoot string, relParts []string, entries []addEntry, info os.FileInfo) ([]copyJob, error) {
	switch {
	case info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0:
		return []copyJob{{Source: localPath, Dest: manifest.SourcePath(sourceRoot, relParts)}}, nil
	case info.IsDir():
		jobs := make([]copyJob, 0, len(entries))
		for _, entry := range entries {
			if isDirEntry(entry.Value) {
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
	for i, part := range prefix {
		if parts[i] != part {
			return nil, fmt.Errorf("internal error: path %q does not match expected prefix %q", strings.Join(parts, "/"), strings.Join(prefix, "/"))
		}
	}
	return cloneParts(parts[len(prefix):]), nil
}

func ensureMissingPath(path string) error {
	if _, err := os.Lstat(path); err == nil {
		return fmt.Errorf("profile source path already exists: %s", path)
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("stat profile source path %s: %w", path, err)
	}
	return nil
}

func isDirEntry(raw any) bool {
	m, ok := raw.(map[string]any)
	if !ok {
		return false
	}
	for key, value := range m {
		if strings.EqualFold(strings.TrimSpace(key), "kind") {
			kind, ok := value.(string)
			return ok && strings.EqualFold(strings.TrimSpace(kind), "dir")
		}
	}
	return false
}

func isEmptyDir(path string) (bool, error) {
	entries, err := os.ReadDir(path)
	if err != nil {
		return false, err
	}
	return len(entries) == 0, nil
}

func insertEntry(root map[string]any, parts []string, value any) error {
	if len(parts) == 0 {
		return fmt.Errorf("cannot add empty path")
	}

	node := root
	for i := 0; i < len(parts)-1; i++ {
		part := parts[i]
		raw, exists := node[part]
		if !exists {
			next := map[string]any{}
			node[part] = next
			node = next
			continue
		}
		if isLeafSpec(raw) {
			return fmt.Errorf("cannot add %q: %q is already a leaf entry", strings.Join(parts, "."), strings.Join(parts[:i+1], "."))
		}
		next, ok := raw.(map[string]any)
		if !ok {
			return fmt.Errorf("cannot add %q: %q is not a nested table", strings.Join(parts, "."), strings.Join(parts[:i+1], "."))
		}
		node = next
	}

	leaf := parts[len(parts)-1]
	if existing, exists := node[leaf]; exists {
		if reflect.DeepEqual(existing, value) {
			return nil
		}
		return fmt.Errorf("cannot add %q: path already exists with a different definition", strings.Join(parts, "."))
	}
	node[leaf] = cloneAny(value)
	return nil
}

func isLeafSpec(raw any) bool {
	switch value := raw.(type) {
	case string:
		return true
	case map[string]any:
		if len(value) == 0 {
			return false
		}
		hasSpec := false
		hasOther := false
		for key := range value {
			switch strings.ToLower(strings.TrimSpace(key)) {
			case "mode", "kind", "tracked":
				hasSpec = true
			default:
				hasOther = true
			}
		}
		return hasSpec && !hasOther
	default:
		return true
	}
}

func cloneAny(v any) any {
	switch value := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(value))
		for k, v := range value {
			out[k] = cloneAny(v)
		}
		return out
	case []any:
		out := make([]any, len(value))
		for i := range value {
			out[i] = cloneAny(value[i])
		}
		return out
	default:
		return value
	}
}

func cloneParts(parts []string) []string {
	return append([]string(nil), parts...)
}

func splitParts(path string) []string {
	clean := filepath.Clean(path)
	if clean == "." {
		return nil
	}
	raw := strings.Split(clean, string(filepath.Separator))
	parts := make([]string, 0, len(raw))
	for _, part := range raw {
		if part == "" || part == "." {
			continue
		}
		parts = append(parts, part)
	}
	return parts
}

func pathDepth(path string) int {
	return len(splitParts(path))
}

func relEscapes(rel string) bool {
	up := ".." + string(filepath.Separator)
	return rel == ".." || strings.HasPrefix(rel, up)
}

func writeManifest(path string, m manifest.Manifest) error {
	tmp := path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("create %s: %w", tmp, err)
	}
	defer f.Close()

	if err := toml.NewEncoder(f).Encode(m); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("encode %s: %w", path, err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("close %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("replace %s: %w", path, err)
	}
	return nil
}
