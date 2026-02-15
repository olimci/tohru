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

	if info.IsDir() {
		manifestPath, err := findManifestFile(absSource)
		if err != nil {
			return Manifest{}, "", err
		}

		m, err := loadTOML(manifestPath)
		if err != nil {
			return Manifest{}, "", err
		}
		return m, absSource, nil
	}

	m, err := loadTOML(absSource)
	if err != nil {
		return Manifest{}, "", err
	}
	return m, filepath.Dir(absSource), nil
}

func loadTOML(path string) (Manifest, error) {
	var m Manifest

	if _, err := toml.DecodeFile(path, &m); err != nil {
		return Manifest{}, fmt.Errorf("decode manifest %s: %w", path, err)
	}
	m.ResolveDefaults()

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
