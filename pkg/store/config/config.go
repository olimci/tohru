package config

type Config struct {
	Tohru   Tohru   `json:"tohru"`   // Application metadata
	Options Options `json:"options"` // Application options
}

type Tohru struct {
	Version string `json:"version"` // Application version
}

type Options struct {
	Backup bool `json:"backup"` // do we store backups? if no error if an action results in clobbering, unless --force
	Clean  bool `json:"clean"`  // if enabled, remove all backup objects that are no longer required by lock automatically (perhaps this can be overridden by --clean/--dirty)
}
