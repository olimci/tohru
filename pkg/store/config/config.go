package config

type Config struct {
	Tohru   Tohru   `toml:"tohru"`   // Application metadata
	Options Options `toml:"options"` // Application options
}

type Tohru struct {
	Version string `toml:"version"` // Application version
}

type Options struct {
	Backup bool `toml:"backup"` // do we store backups? if no error if an action results in clobbering, unless --force
	Clean  bool `toml:"clean"`  // if enabled, remove all backup objects that are no longer required by lock automatically (perhaps this can be overridden by --clean/--dirty)
}
