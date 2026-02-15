package cmd

import (
	"context"

	"github.com/olimci/tohru/pkg/version"
	"github.com/urfave/cli/v3"
)

// Commands:
// install
//   (inialises .config/tohru)
//
// load [source]:
//   loads the given source (installs if not already installed)
//   - steps:
//   - unload current source, remove all managed objects,
//   - load new source, add all managed objects,
//   - for objects with a Prev that haven't been replaced, move them from .config/tohru/backups back to their original location
//   - for objects with a Prev that have been replaced, update the new object's Prev to the old object's Prev
//   - if clean is enabled, remove all backup objects that are no longer required by lock
//
// reload
//   reloads the currently loaded source
//
// unload
//   unloads the currently loaded source and restores backed-up objects where appropriate
//
// uninstall
//   uninstalls tohru store from the machine

func Execute(ctx context.Context, args []string) error {
	app := &cli.Command{
		Name:    "tohru",
		Usage:   "a simple dotfiles manager",
		Version: version.Version,
		Flags: []cli.Flag{
			&cli.BoolFlag{
				Name:  "verbose",
				Usage: "show changed filesystem paths",
			},
		},
		Commands: []*cli.Command{
			installCommand(),
			loadCommand(),
			reloadCommand(),
			unloadCommand(),
			tidyCommand(),
			statusCommand(),
			uninstallCommand(),
		},
	}

	return app.Run(ctx, args)
}
