package cmd

import (
	"context"
	"fmt"

	storepkg "github.com/olimci/tohru/pkg/store"
	"github.com/urfave/cli/v3"
)

func loadCommand() *cli.Command {
	return &cli.Command{
		Name:      "load",
		Aliases:   []string{"switch"},
		Usage:     "load a source",
		ArgsUsage: "<source>",
		Flags: []cli.Flag{
			&cli.BoolFlag{
				Name:    "force",
				Aliases: []string{"f"},
				Usage:   "overwrite existing files or modified managed files",
			},
		},
		Action: loadAction,
	}
}

func loadAction(_ context.Context, cmd *cli.Command) error {
	args := cmd.Args().Slice()
	source := cmd.Args().First()

	if source == "" {
		return fmt.Errorf("load requires a source argument")
	}

	if len(args) > 1 {
		return fmt.Errorf("load accepts exactly one source argument")
	}
	force := cmd.Bool("force")

	store, err := storepkg.DefaultStore()
	if err != nil {
		return err
	}

	res, err := store.Load(source, force)
	if err != nil {
		return err
	}

	if res.UnloadedSourceName != "" || res.UnloadedTrackedCount > 0 {
		name := res.UnloadedSourceName
		if name == "" {
			name = "previous source"
		}
		fmt.Printf("unloaded %s (%d managed object(s))\n", name, res.UnloadedTrackedCount)
	}

	fmt.Printf("loaded %s (%d tracked object(s))\n", res.SourceName, res.TrackedCount)
	if res.RemovedBackupCount > 0 {
		fmt.Printf("cleaned %d unreferenced backup object(s)\n", res.RemovedBackupCount)
	}
	printChangedPaths(cmd, res.ChangedPaths)
	return nil
}
