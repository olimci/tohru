package cmd

import (
	"context"
	"fmt"

	storepkg "github.com/olimci/tohru/pkg/store"
	"github.com/urfave/cli/v3"
)

func reloadCommand() *cli.Command {
	return &cli.Command{
		Name:  "reload",
		Usage: "reload the currently loaded source",
		Flags: []cli.Flag{
			&cli.BoolFlag{
				Name:    "force",
				Aliases: []string{"f"},
				Usage:   "reload current source",
			},
		},
		Action: reloadAction,
	}
}

func reloadAction(_ context.Context, cmd *cli.Command) error {
	args := cmd.Args().Slice()

	if len(args) > 0 {
		return fmt.Errorf("reload does not accept arguments")
	}
	force := cmd.Bool("force")

	store, err := storepkg.DefaultStore()
	if err != nil {
		return err
	}

	if !store.IsInstalled() {
		return fmt.Errorf("tohru is not installed, run `tohru install` first")
	}

	res, err := store.Reload(force)
	if err != nil {
		return err
	}

	fmt.Printf("reloaded %s (%d tracked object(s))\n", res.SourceDir, res.TrackedCount)
	return nil
}
