# tohru

a simple dotfiles manager.

## Install

```bash
go install github.com/olimci/tohru
```

## Usage

```bash
# show the banner and top-level help
tohru
# print the current version
tohru version
# install application files (optionally load a profile immediately)
tohru install [profile]
# list cached profile slugs and paths
tohru profile list
# create a new empty profile in ~/.tohru/profiles/<slug>
tohru profile new <slug>
# copy a local path into a profile and add manifest entries
tohru profile add <slug> <path>
# merge nested roots in a profile manifest
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
  "schema": 1,
  "requires": {
    "tohru": "0.2.0"
  },
  "profile": {
    "slug": "my-dotfiles",
    "name": "my-dotfiles",
    "description": "personal setup"
  },
  "roots": [
    {
      "source": "home",
      "dest": "~",
      "defaults": {
        "type": "link"
      },
      "tree": {
        ".zshrc": ["copy"],
        ".config": {
          "kitty": {
            "kitty.conf": [],
            "theme.conf": [],
            "kitty.app.png": ["copy", "untracked"]
          },
          "nvim": {
            "after": {
              ".": ["untracked"]
            }
          }
        }
      }
    }
  ]
}
```

In the structural tree format, arrays represent files and objects represent directories. Directory metadata uses the reserved `"."` key, and an empty array `[]` means “inherit defaults with no overrides”.

In profile source trees, hidden path segments are encoded with a `dot_` prefix, so `.config/nvim` is stored as `dot_config/nvim`.

When a loaded profile has `profile.slug`, tohru caches `slug -> profile path` in state, so future `tohru load <slug>` works without the full path.
