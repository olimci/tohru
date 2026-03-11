package config

const (
	SchemaVersion = 1
	PruneAuto     = "auto"
	PruneManual   = "manual"
)

type Config struct {
	Schema  int     `json:"schema"`
	Options Options `json:"options"`
}

type Options struct {
	Backups       Backups `json:"backups"`
	CacheProfiles bool    `json:"cache_profiles"`
}

type Backups struct {
	Enabled bool   `json:"enabled"`
	Prune   string `json:"prune"`
}
