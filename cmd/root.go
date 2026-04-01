package cmd

import (
	"context"
	"fmt"

	"github.com/olimci/tohru/pkg/version"
	"github.com/urfave/cli/v3"
)

const repoLink = "github.com/olimci/tohru"

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
			versionCommand(),

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

	if len(args) <= 1 {
		fmt.Println(version.Banner(repoLink) + "\n")
		args = append(args, "help")
	}

	return app.Run(ctx, args)
}
