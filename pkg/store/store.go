package store

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/olimci/tohru/pkg/store/config"
	"github.com/olimci/tohru/pkg/store/state"
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
		Schema: config.SchemaVersion,
		Options: config.Options{
			Backups: config.Backups{
				Enabled: true,
				Prune:   config.PruneAuto,
			},
			CacheProfiles: true,
		},
	}
}

func DefaultState() state.State {
	return state.State{
		Profile: state.Profile{
			State: "unloaded",
			Kind:  defaultKind,
			Path:  "",
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

	if cfg.Schema != config.SchemaVersion {
		return config.Config{}, fmt.Errorf("unsupported config schema %d", cfg.Schema)
	}

	cfg.Options.Backups.Prune = strings.ToLower(strings.TrimSpace(cfg.Options.Backups.Prune))
	if cfg.Options.Backups.Prune == "" {
		cfg.Options.Backups.Prune = config.PruneAuto
	}
	switch cfg.Options.Backups.Prune {
	case config.PruneAuto, config.PruneManual:
	default:
		return config.Config{}, fmt.Errorf("unsupported options.backups.prune value %q", cfg.Options.Backups.Prune)
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

	if lck.Profile.Kind == "" {
		lck.Profile.Kind = defaultKind
	}
	if lck.Profile.State == "" {
		lck.Profile.State = "unloaded"
	}

	return lck, nil
}

func (s Store) SaveState(lck state.State) error {
	if lck.Profile.Kind == "" {
		lck.Profile.Kind = defaultKind
	}
	if lck.Profile.State == "" {
		lck.Profile.State = "unloaded"
	}

	return encodeJSON(s.StatePath(), lck)
}

func (s Store) LoadProfiles() (map[string]state.CachedProfile, error) {
	profiles := map[string]state.CachedProfile{}
	if _, err := os.Stat(s.ProfilesFilePath()); err == nil {
		if err := decodeJSON(s.ProfilesFilePath(), &profiles); err != nil {
			return nil, fmt.Errorf("decode %s: %w", s.ProfilesFilePath(), err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("stat %s: %w", s.ProfilesFilePath(), err)
	}
	if profiles == nil {
		profiles = map[string]state.CachedProfile{}
	}
	return profiles, nil
}

func (s Store) SaveProfiles(profiles map[string]state.CachedProfile) error {
	if profiles == nil {
		profiles = map[string]state.CachedProfile{}
	}
	return encodeJSON(s.ProfilesFilePath(), profiles)
}
