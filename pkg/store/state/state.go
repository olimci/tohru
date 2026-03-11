package state

// State stores the current state of the application.
type State struct {
	Profile Profile `json:"profile"`        // current profile state
	Files   []File  `json:"files"`          // tohru managed files
	Dirs    []Dir   `json:"dirs,omitempty"` // auto-created parent dirs (cleanup if empty)
}

// Profile references the currently loaded profile.
type Profile struct {
	State string `json:"state"` // unloaded|loaded
	Kind  string `json:"kind"`  // local (remote might be added later)
	Path  string `json:"path"`  // path to profile directory
	Slug  string `json:"slug,omitempty"`
	Name  string `json:"name,omitempty"`
}

// CachedProfile is a cached profile entry used in profiles.json.
type CachedProfile struct {
	Slug string `json:"slug"`
	Name string `json:"name,omitempty"`
	Path string `json:"path"`
}

// File represents a managed object, ie one that the application created and is tracking
type File struct {
	Path string `json:"path"` // path to managed object

	// Current exists so we can check if a managed file has been modified externally and fail if it has.
	Current Object `json:"curr"` // existing object state
	// Previous exists so we know where the backup object is stored, and what it is.
	Previous *Object `json:"prev,omitempty"` // state of previous object there
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
