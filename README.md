# tohru

a simple dotfiles manager.

## Install

```bash
go install github.com/olimci/tohru
```

## Usage

```bash
# install application files (optionally load a profile immediately)
tohru install [profile]
# list cached profile slugs and paths
tohru profile list
# create a new empty profile in ~/.tohru/profiles/<slug>
tohru profile new <slug>
# copy a local path into a profile and add manifest entries
tohru profile add <slug> <path>
# merge nested tree roots in a profile manifest
tohru profile tidy <slug>
# load some dotfiles (path, or a cached profile slug)
tohru load [profile]
# reload current profile
tohru reload
# unload current profile
tohru unload
# see what files are being tracked by tohru
tohru status
```

tohru will automatically take backups of files that might be clobbered, and will automatically restore them once the conflicting profile is unloaded. this behaviour is configurable in config.

## Manifest

dotfiles are defined with a `tohru.json` file:

```json
{
  "tohru": {
    "version": "0.2.0"
  },
  "profile": {
    "slug": "my-dotfiles",
    "name": "my-dotfiles",
    "description": "personal setup"
  },

  "trees": {
    "home": {
      "dest": "~",
      "files": {
        ".zshrc": { "mode": "copy" },
        ".config": {
          "kitty": {
            "kitty.conf": { "mode": "link" },
            "theme.conf": { "mode": "link" },
            "kitty.app.png": { "mode": "copy", "tracked": false }
          },
          "nvim": {
            "after": { "kind": "dir", "tracked": false }
          }
        }
      }
    }
  }
}
```

In profile source trees, hidden path segments are encoded with a `dot_` prefix (for example, `.config/nvim` is stored as `dot_config/nvim`).

When a loaded profile has `profile.slug`, tohru caches `slug -> profile path` in state, so future `tohru load <slug>` works without the full path.
