package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

func encodeJSON(path string, value any) error {
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

	if err := json.NewEncoder(f).Encode(value); err != nil {
		_ = os.Remove(tp)
		return fmt.Errorf("encode %s: %w", tp, err)
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

func ensureJSONFile(path string, value any) (bool, error) {
	if _, err := os.Stat(path); err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return false, fmt.Errorf("stat %s: %w", path, err)
		}
		if err := encodeJSON(path, value); err != nil {
			return false, err
		}
		return true, nil
	}
	return false, nil
}

func decodeJSON(path string, value any) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	dec := json.NewDecoder(f)
	if err := dec.Decode(value); err != nil {
		return err
	}
	return nil
}
