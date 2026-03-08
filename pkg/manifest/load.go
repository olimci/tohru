package manifest

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
	"github.com/olimci/tohru/pkg/utils/fileutils"
)

const name = "tohru.toml"

// Load resolves a source path and decodes its manifest.
// returns an absolute path to the manifest directory
func Load(source string) (Manifest, string, error) {
	absSource, err := fileutils.AbsPath(source)
	if err != nil {
		return Manifest{}, "", err
	}

	info, err := os.Stat(absSource)
	if err != nil {
		return Manifest{}, "", fmt.Errorf("stat source %q: %w", source, err)
	}

	manifestPath := absSource
	sourceDir := filepath.Dir(absSource)
	if info.IsDir() {
		manifestPath, err = findManifestFile(absSource)
		if err != nil {
			return Manifest{}, "", err
		}
		sourceDir = absSource
	}

	manifest, err := decodeTOML(manifestPath)
	if err != nil {
		return Manifest{}, "", err
	}
	if err := manifest.ResolveDefaults(); err != nil {
		return Manifest{}, "", err
	}

	return manifest, sourceDir, nil
}

func decodeTOML(path string) (Manifest, error) {
	var m Manifest
	md, err := toml.DecodeFile(path, &m)
	if err != nil {
		return Manifest{}, fmt.Errorf("decode manifest %s: %w", path, err)
	}

	for _, key := range md.Undecoded() {
		if len(key) == 0 {
			continue
		}
		switch key[0] {
		case "import":
			return Manifest{}, fmt.Errorf("manifest imports ([[import]]) are no longer supported")
		}
	}
	return m, nil
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
