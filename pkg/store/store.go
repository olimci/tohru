package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/olimci/tohru/pkg/store/config"
	"github.com/olimci/tohru/pkg/store/lock"
	"github.com/olimci/tohru/pkg/version"
)

const (
	dirName     = "tohru"
	configFile  = "config.toml"
	lockFile    = "lock.json"
	backupsDir  = "backups"
	defaultKind = "local"
	envStoreDir = "TOHRU_STORE_DIR"
)

var (
	ErrAlreadyInstalled = errors.New("tohru is already installed")
	ErrNotInstalled     = errors.New("tohru is not installed")
)

// Store points to local store files.
type Store struct {
	Root string
}

func DefaultStore() (Store, error) {
	if customRoot := strings.TrimSpace(os.Getenv(envStoreDir)); customRoot != "" {
		absRoot, err := filepath.Abs(customRoot)
		if err != nil {
			return Store{}, fmt.Errorf("resolve %s: %w", envStoreDir, err)
		}
		return Store{Root: absRoot}, nil
	}

	cfgDir, err := os.UserConfigDir()
	if err != nil {
		return Store{}, fmt.Errorf("resolve user config directory: %w", err)
	}

	return Store{Root: filepath.Join(cfgDir, dirName)}, nil
}

func (s Store) ConfigPath() string {
	return filepath.Join(s.Root, configFile)
}

func (s Store) LockPath() string {
	return filepath.Join(s.Root, lockFile)
}

func (s Store) BackupsPath() string {
	return filepath.Join(s.Root, backupsDir)
}

func (s Store) IsInstalled() bool {
	if _, err := os.Stat(s.ConfigPath()); err != nil {
		return false
	}
	if _, err := os.Stat(s.LockPath()); err != nil {
		return false
	}
	return true
}

func DefaultConfig() config.Config {
	return config.Config{
		Tohru: config.Tohru{
			Version: version.Version,
		},
		Options: config.Options{
			Backup: true,
			Clean:  true,
		},
	}
}

func DefaultLock() lock.Lock {
	return lock.Lock{
		Manifest: lock.Manifest{
			State: "unloaded",
			Kind:  defaultKind,
			Loc:   "",
		},
		Files: nil,
		Dirs:  nil,
	}
}

// Install initializes store and fails if store already exists.
func (s Store) Install() error {
	if s.IsInstalled() {
		return ErrAlreadyInstalled
	}

	_, err := s.installMissing()
	return err
}

// EnsureInstalled initializes store if missing.
func (s Store) EnsureInstalled() error {
	_, err := s.installMissing()
	return err
}

// installMissing creates store directories and any missing store files.
func (s Store) installMissing() (bool, error) {
	if err := os.MkdirAll(s.BackupsPath(), 0o755); err != nil {
		return false, fmt.Errorf("create store directories: %w", err)
	}

	var changed bool

	createdCfg, err := ensureDefaultConfig(s.ConfigPath())
	if err != nil {
		return false, err
	}
	if createdCfg {
		changed = true
	}

	createdLock, err := ensureDefaultLock(s)
	if err != nil {
		return false, err
	}
	if createdLock {
		changed = true
	}

	return changed, nil
}

func (s Store) LoadConfig() (config.Config, error) {
	cfg := DefaultConfig()
	if _, err := os.Stat(s.ConfigPath()); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return cfg, nil
		}
		return config.Config{}, fmt.Errorf("stat %s: %w", s.ConfigPath(), err)
	}

	if _, err := toml.DecodeFile(s.ConfigPath(), &cfg); err != nil {
		return config.Config{}, fmt.Errorf("decode %s: %w", s.ConfigPath(), err)
	}

	if cfg.Tohru.Version == "" {
		cfg.Tohru.Version = version.Version
	}
	if err := version.EnsureCompatible(cfg.Tohru.Version); err != nil {
		return config.Config{}, fmt.Errorf("unsupported config version %q: %w", cfg.Tohru.Version, err)
	}

	return cfg, nil
}

func (s Store) SaveConfig(cfg config.Config) error {
	if cfg.Tohru.Version == "" {
		cfg.Tohru.Version = version.Version
	}
	return writeTOML(s.ConfigPath(), cfg)
}

func (s Store) LoadLock() (lock.Lock, error) {
	lck := DefaultLock()
	if _, err := os.Stat(s.LockPath()); err == nil {
		if err := decodeJSONFile(s.LockPath(), &lck); err != nil {
			return lock.Lock{}, fmt.Errorf("decode %s: %w", s.LockPath(), err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return lock.Lock{}, fmt.Errorf("stat %s: %w", s.LockPath(), err)
	}

	if lck.Manifest.Kind == "" {
		lck.Manifest.Kind = defaultKind
	}
	if lck.Manifest.State == "" {
		lck.Manifest.State = "unloaded"
	}

	return lck, nil
}

func (s Store) SaveLock(lck lock.Lock) error {
	if lck.Manifest.Kind == "" {
		lck.Manifest.Kind = defaultKind
	}
	if lck.Manifest.State == "" {
		lck.Manifest.State = "unloaded"
	}

	return writeJSON(s.LockPath(), lck)
}

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

	tmpPath := path + ".tmp"

	f, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("create %s: %w", tmpPath, err)
	}

	encErr := toml.NewEncoder(f).Encode(value)
	closeErr := f.Close()
	if encErr != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("encode %s: %w", path, encErr)
	}
	if closeErr != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("close %s: %w", tmpPath, closeErr)
	}

	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("replace %s: %w", path, err)
	}

	return nil
}

func writeJSON(path string, value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("encode %s: %w", path, err)
	}
	return writeData(path, data)
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

func writeData(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create directory for %s: %w", path, err)
	}

	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", tmpPath, err)
	}

	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("replace %s: %w", path, err)
	}

	return nil
}
