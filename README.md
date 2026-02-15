# tohru

a simple dotfiles manager.

## Install

```bash
go install github.com/olimci/tohru
```

## Usage

```bash
# install application files (optionally load a source immediately)
tohru install [source]
# load some dotfiles
tohru load [path]
# reload current source
tohru reload
# unload current source
tohru unload
# see what files are being tracked by tohru
tohru status
```

tohru will automatically take backups of files that might be clobbered, and will automatically restore them once the conflicting source is unloaded. this behaviour is configurable in config.

## Manifest

dotfiles are defined with a `tohru.toml` file:

```toml
[tohru]
version = "0.0.0"

[source]
name = "my-dotfiles"
description = "personal setup"

# create a symlink
[[link]]
to = "zshrc"
from = "~/.zshrc"

# copy a file
[[file]]
source = "gitconfig"
dest = "~/.gitconfig"

# create a dir
[[dir]]
path = "~/.config/nvim"
tracked = false # disable tracking for dir
```
