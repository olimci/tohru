package store

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/olimci/tohru/pkg/store/config"
	"github.com/olimci/tohru/pkg/store/state"
	"github.com/olimci/tohru/pkg/version"
)

const (
	dirName      = ".tohru"
	configFile   = "config.json"
	stateFile    = "state.json"
	backupsDir   = "backups"
	profilesDir  = "profiles"
	profilesFile = "profiles.json"
	defaultKind  = "local"
	envStoreDir  = "TOHRU_STORE_DIR"
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

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return Store{}, fmt.Errorf("resolve user home directory: %w", err)
	}

	return Store{Root: filepath.Join(homeDir, dirName)}, nil
}

func (s Store) ConfigPath() string {
	return filepath.Join(s.Root, configFile)
}

func (s Store) StatePath() string {
	return filepath.Join(s.Root, stateFile)
}

func (s Store) BackupsPath() string {
	return filepath.Join(s.Root, backupsDir)
}

func (s Store) ProfilesPath() string {
	return filepath.Join(s.Root, profilesDir)
}

func (s Store) ProfilesFilePath() string {
	return filepath.Join(s.Root, profilesFile)
}

func (s Store) IsInstalled() bool {
	if _, err := os.Stat(s.ConfigPath()); err != nil {
		return false
	}
	if _, err := os.Stat(s.StatePath()); err != nil {
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

func DefaultState() state.State {
	return state.State{
		Manifest: state.Manifest{
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
	lock, err := s.Lock()
	if err != nil {
		return err
	}
	defer lock.Unlock()

	if s.IsInstalled() {
		return ErrAlreadyInstalled
	}

	_, err = s.installMissing()
	return err
}

// installMissing creates store directories and any missing store files.
func (s Store) installMissing() (bool, error) {
	if err := os.MkdirAll(s.BackupsPath(), 0o755); err != nil {
		return false, fmt.Errorf("create store directories: %w", err)
	}
	if err := os.MkdirAll(s.ProfilesPath(), 0o755); err != nil {
		return false, fmt.Errorf("create store directories: %w", err)
	}

	var changed bool

	if wrote, err := ensureJSONFile(s.ConfigPath(), DefaultConfig()); err != nil {
		return false, err
	} else if wrote {
		changed = true
	}

	if wrote, err := ensureJSONFile(s.StatePath(), DefaultState()); err != nil {
		return false, err
	} else if wrote {
		changed = true
	}

	if wrote, err := ensureJSONFile(s.ProfilesFilePath(), map[string]any{}); err != nil {
		return false, err
	} else if wrote {
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

	if err := decodeJSON(s.ConfigPath(), &cfg); err != nil {
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

func (s Store) LoadState() (state.State, error) {
	lck := DefaultState()
	if _, err := os.Stat(s.StatePath()); err == nil {
		if err := decodeJSON(s.StatePath(), &lck); err != nil {
			return state.State{}, fmt.Errorf("decode %s: %w", s.StatePath(), err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return state.State{}, fmt.Errorf("stat %s: %w", s.StatePath(), err)
	}

	if lck.Manifest.Kind == "" {
		lck.Manifest.Kind = defaultKind
	}
	if lck.Manifest.State == "" {
		lck.Manifest.State = "unloaded"
	}

	return lck, nil
}

func (s Store) SaveState(lck state.State) error {
	if lck.Manifest.Kind == "" {
		lck.Manifest.Kind = defaultKind
	}
	if lck.Manifest.State == "" {
		lck.Manifest.State = "unloaded"
	}

	return encodeJSON(s.StatePath(), lck)
}

func (s Store) LoadProfiles() (map[string]state.Profile, error) {
	profiles := map[string]state.Profile{}
	if _, err := os.Stat(s.ProfilesFilePath()); err == nil {
		if err := decodeJSON(s.ProfilesFilePath(), &profiles); err != nil {
			return nil, fmt.Errorf("decode %s: %w", s.ProfilesFilePath(), err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("stat %s: %w", s.ProfilesFilePath(), err)
	}
	if profiles == nil {
		profiles = map[string]state.Profile{}
	}
	return profiles, nil
}

func (s Store) SaveProfiles(profiles map[string]state.Profile) error {
	if profiles == nil {
		profiles = map[string]state.Profile{}
	}
	return encodeJSON(s.ProfilesFilePath(), profiles)
}
