package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

func ensureDefaultConfig(path string) (bool, error) {
	if _, err := os.Stat(path); err == nil {
		return false, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return false, fmt.Errorf("stat %s: %w", path, err)
	}

	if err := writeTOML(path, DefaultConfig()); err != nil {
		return false, err
	}
	return true, nil
}

func ensureDefaultLock(s Store) (bool, error) {
	if _, err := os.Stat(s.LockPath()); err == nil {
		return false, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return false, fmt.Errorf("stat %s: %w", s.LockPath(), err)
	}

	if err := writeJSON(s.LockPath(), DefaultLock()); err != nil {
		return false, err
	}
	return true, nil
}

func writeTOML(path string, value any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create directory for %s: %w", path, err)
	}

	tp := path + ".tmp"

	f, err := os.OpenFile(tp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("create %s: %w", tp, err)
	}
	defer f.Close()

	if err := toml.NewEncoder(f).Encode(value); err != nil {
		_ = os.Remove(tp)
		return fmt.Errorf("encode: %s: %w", path, err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tp)
		return fmt.Errorf("close %s: %w", path, err)
	}

	if err := os.Rename(tp, path); err != nil {
		_ = os.Remove(tp)
		return fmt.Errorf("replace %s: %w", path, err)
	}

	return nil
}

func writeJSON(path string, value any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create directory for %s: %w", path, err)
	}

	tp := path + ".tmp"

	f, err := os.OpenFile(tp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("create %s: %w", tp, err)
	}
	defer f.Close()

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

func decodeJSONFile(path string, value any) error {
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
