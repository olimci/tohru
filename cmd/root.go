package cmd

import (
	"context"

	"github.com/olimci/tohru/pkg/version"
	"github.com/urfave/cli/v3"
)

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
