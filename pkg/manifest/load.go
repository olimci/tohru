package manifest

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/olimci/tohru/pkg/utils/fileutils"
)

const name = "tohru.toml"

type ImportTree struct {
	Path    string
	Imports []ImportTree
}

// Load resolves a source path and decodes its manifest.
// returns an absolute path to the manifest directory
func Load(source string) (Manifest, string, error) {
	manifest, sourceDir, _, err := load(source)
	return manifest, sourceDir, err
}

// LoadWithTree resolves a source path and decodes its manifest, and returns
// the resolved import tree after platform filtering.
func LoadWithTree(source string) (Manifest, string, ImportTree, error) {
	return load(source)
}

func load(source string) (Manifest, string, ImportTree, error) {
	absSource, err := fileutils.AbsPath(source)
	if err != nil {
		return Manifest{}, "", ImportTree{}, err
	}

	info, err := os.Stat(absSource)
	if err != nil {
		return Manifest{}, "", ImportTree{}, fmt.Errorf("stat source %q: %w", source, err)
	}

	if info.IsDir() {
		manifestPath, err := findManifestFile(absSource)
		if err != nil {
			return Manifest{}, "", ImportTree{}, err
		}

		m, tree, err := loadWithImports(manifestPath, absSource)
		if err != nil {
			return Manifest{}, "", ImportTree{}, err
		}
		return m, absSource, tree, nil
	}

	m, tree, err := loadWithImports(absSource, filepath.Dir(absSource))
	if err != nil {
		return Manifest{}, "", ImportTree{}, err
	}
	return m, filepath.Dir(absSource), tree, nil
}

type loadContext struct {
	rootDir string
	stack   []string
	inStack map[string]struct{}
}

func loadWithImports(path, rootDir string) (Manifest, ImportTree, error) {
	canonicalRoot, err := filepath.EvalSymlinks(filepath.Clean(rootDir))
	if err != nil {
		return Manifest{}, ImportTree{}, fmt.Errorf("resolve source root %s: %w", rootDir, err)
	}

	ctx := loadContext{
		rootDir: filepath.Clean(canonicalRoot),
		stack:   make([]string, 0, 8),
		inStack: make(map[string]struct{}, 8),
	}

	manifest, tree, err := ctx.load(path)
	if err != nil {
		return Manifest{}, ImportTree{}, err
	}
	manifest.ResolveDefaults()
	return manifest, tree, nil
}

func (ctx *loadContext) load(path string) (Manifest, ImportTree, error) {
	manifestPath, err := canonicalPath(path)
	if err != nil {
		return Manifest{}, ImportTree{}, err
	}

	if !pathWithinRoot(ctx.rootDir, manifestPath) {
		return Manifest{}, ImportTree{}, fmt.Errorf("import path escapes source root %s: %s", ctx.rootDir, manifestPath)
	}

	if _, seen := ctx.inStack[manifestPath]; seen {
		cycle := append(append([]string(nil), ctx.stack...), manifestPath)
		return Manifest{}, ImportTree{}, fmt.Errorf("manifest import cycle detected: %s", strings.Join(cycle, " -> "))
	}

	ctx.inStack[manifestPath] = struct{}{}
	ctx.stack = append(ctx.stack, manifestPath)
	defer func() {
		delete(ctx.inStack, manifestPath)
		ctx.stack = ctx.stack[:len(ctx.stack)-1]
	}()

	current, err := decodeTOML(manifestPath)
	if err != nil {
		return Manifest{}, ImportTree{}, err
	}

	merged := Manifest{}
	tree := ImportTree{
		Path:    manifestPath,
		Imports: nil,
	}
	importerDir := filepath.Dir(manifestPath)
	for _, imp := range current.Imports {
		if !imp.Applies(runtime.GOOS, runtime.GOARCH) {
			continue
		}

		importPath, err := resolveImportPath(ctx.rootDir, importerDir, imp.Path)
		if err != nil {
			return Manifest{}, ImportTree{}, err
		}

		imported, importedTree, err := ctx.load(importPath)
		if err != nil {
			return Manifest{}, ImportTree{}, err
		}
		merged.Merge(imported)
		tree.Imports = append(tree.Imports, importedTree)
	}

	current.Imports = nil
	merged.Merge(current)
	return merged, tree, nil
}

func decodeTOML(path string) (Manifest, error) {
	var m Manifest
	if _, err := toml.DecodeFile(path, &m); err != nil {
		return Manifest{}, fmt.Errorf("decode manifest %s: %w", path, err)
	}
	return m, nil
}

func canonicalPath(path string) (string, error) {
	absPath, err := fileutils.AbsPath(path)
	if err != nil {
		return "", err
	}
	canonical, err := filepath.EvalSymlinks(absPath)
	if err != nil {
		return "", fmt.Errorf("resolve manifest path %s: %w", absPath, err)
	}
	return filepath.Clean(canonical), nil
}

func resolveImportPath(rootDir, importerDir, raw string) (string, error) {
	path := fileutils.ExpandHome(strings.TrimSpace(raw))
	if path == "" {
		return "", fmt.Errorf("import path is empty")
	}

	var candidate string
	if filepath.IsAbs(path) {
		candidate = filepath.Clean(path)
	} else {
		candidate = filepath.Clean(filepath.Join(importerDir, path))
	}

	info, err := os.Stat(candidate)
	if err != nil {
		return "", fmt.Errorf("stat import path %s: %w", candidate, err)
	}
	var manifestPath string
	if info.IsDir() {
		manifestPath, err = findManifestFile(candidate)
		if err != nil {
			return "", err
		}
	} else {
		manifestPath = candidate
	}

	canonicalManifestPath, err := canonicalPath(manifestPath)
	if err != nil {
		return "", err
	}
	if !pathWithinRoot(rootDir, canonicalManifestPath) {
		return "", fmt.Errorf("import path escapes source root %s: %s", rootDir, canonicalManifestPath)
	}

	return canonicalManifestPath, nil
}

func pathWithinRoot(rootDir, path string) bool {
	root := filepath.Clean(rootDir)
	candidate := filepath.Clean(path)

	rel, err := filepath.Rel(root, candidate)
	if err != nil {
		return false
	}

	up := ".." + string(filepath.Separator)
	return rel != ".." && !strings.HasPrefix(rel, up)
}

func findManifestFile(sourceDir string) (string, error) {
	candidate := filepath.Join(sourceDir, name)

	info, err := os.Stat(candidate)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("no manifest found in %s (expected %s)", sourceDir, name)
		}
		return "", fmt.Errorf("stat manifest candidate %s: %w", candidate, err)
	}
	if info.IsDir() {
		return "", fmt.Errorf("manifest path is a directory: %s", candidate)
	}

	return candidate, nil
}
