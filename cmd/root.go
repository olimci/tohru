package cmd

import (
	"context"

	"github.com/olimci/tohru/pkg/version"
	"github.com/urfave/cli/v3"
)

// TODO: revise this, potentially move to main.go
// Commands:
// install
//   (inialises ~/.tohru)
//
// load [profile]:
//   loads the given profile (installs if not already installed)
//   - steps:
//   - unload current profile, remove all managed objects,
//   - load new profile, add all managed objects,
//   - for objects with a Prev that haven't been replaced, move them from ~/.tohru/backups back to their original location
//   - for objects with a Prev that have been replaced, update the new object's Prev to the old object's Prev
//   - if clean is enabled, remove all backup objects that are no longer required by lock
//
// reload
//   reloads the currently loaded profile
//
// unload
//   unloads the currently loaded profile and restores backed-up objects where appropriate
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
			// application management
			installCommand(),
			uninstallCommand(),
			tidyCommand(),
			statusCommand(),

			// profile management
			profileCommand(),
			loadCommand(),
			reloadCommand(),
			unloadCommand(),
		},
	}

	return app.Run(ctx, args)
}
