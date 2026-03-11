package manifest

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/olimci/tohru/pkg/utils/fileutils"
)

const Name = "tohru.json"

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

	manifest, err := decodeManifest(manifestPath)
	if err != nil {
		return Manifest{}, "", err
	}
	if err := manifest.Resolve(); err != nil {
		return Manifest{}, "", err
	}

	return manifest, sourceDir, nil
}

func decodeManifest(path string) (Manifest, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Manifest{}, fmt.Errorf("read manifest %s: %w", path, err)
	}

	var m Manifest
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&m); err != nil {
		return Manifest{}, fmt.Errorf("decode manifest %s: %w", path, err)
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		return Manifest{}, fmt.Errorf("decode manifest %s: trailing content after top-level object", path)
	}

	return m, nil
}

func Write(path string, m Manifest) error {
	payload, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("encode %s: %w", path, err)
	}
	payload = append(payload, '\n')

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create directory for %s: %w", path, err)
	}

	f, err := os.CreateTemp(dir, filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("create temporary file for %s: %w", path, err)
	}
	tp := f.Name()
	defer f.Close()

	if err := f.Chmod(0o644); err != nil {
		_ = os.Remove(tp)
		return fmt.Errorf("chmod %s: %w", tp, err)
	}

	if _, err := f.Write(payload); err != nil {
		_ = os.Remove(tp)
		return fmt.Errorf("write %s: %w", tp, err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tp)
		return fmt.Errorf("close %s: %w", tp, err)
	}
	if err := os.Rename(tp, path); err != nil {
		_ = os.Remove(tp)
		return fmt.Errorf("replace %s: %w", path, err)
	}
	return nil
}

func findManifestFile(sourceDir string) (string, error) {
	candidate := filepath.Join(sourceDir, Name)

	info, err := os.Stat(candidate)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("no manifest found in %s (expected %s)", sourceDir, Name)
		}
		return "", fmt.Errorf("stat manifest candidate %s: %w", candidate, err)
	}
	if info.IsDir() {
		return "", fmt.Errorf("manifest path is a directory: %s", candidate)
	}

	return candidate, nil
}
