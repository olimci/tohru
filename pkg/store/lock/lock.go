package lock

// Lock stores the current state of the application.
type Lock struct {
	Manifest Manifest `json:"manifest"`      // current manifest state
	Files    []File   `json:"file"`          // tohru managed files
	Dirs     []Dir    `json:"dir,omitempty"` // auto-created parent dirs (cleanup if empty)
}

// Manifest references the currently loaded manifest. path points to the directory containing the manifest, state indicates the application's state
type Manifest struct {
	State string `json:"state"` // unloaded|loaded
	Kind  string `json:"kind"`  // local (remote might be added later)
	Loc   string `json:"loc"`   // path to manifest directory
}

// File represents a managed object, ie one that the application created and is tracking
type File struct {
	Path string `json:"path"` // path to managed object

	// Curr exists so we can check if a managed file has been modified externally and fail if it has
	Curr Object `json:"curr"` // existing object state
	// Prev exists so we know where the backup object is stored, and what it is.
	Prev *Object `json:"prev,omitempty"` // state of previous object there
}

// Dir is an auto-created directory that can be removed if empty.
type Dir struct {
	Path string `json:"path"`
}

// Object is a generic filesystem object for backups or checking current files
type Object struct {
	Path string `json:"path"` // basically not useful, just there for metadata
	// digest kind is null when there is no object.
	Digest string `json:"hash"` // something like "[null|file|dir|symlink]:sha(whatever):{CID}"
}
