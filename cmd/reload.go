package cmd

import (
	"context"
	"fmt"

	"github.com/olimci/tohru/pkg/store"
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
				Usage:   "allow clobbering existing paths when reloading",
			},
			&cli.BoolFlag{
				Name:  "discard-changes",
				Usage: "allow replacing modified managed files without enabling full force behavior",
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
	opts := applyOptionsFromCommand(cmd)

	s, err := store.DefaultStore()
	if err != nil {
		return err
	}

	if !s.IsInstalled() {
		return fmt.Errorf("tohru is not installed, run `tohru install` first")
	}

	res, err := s.Reload(opts)
	if err != nil {
		return err
	}

	if res.UnloadedSourceName != "" || res.UnloadedTrackedCount > 0 {
		name := res.UnloadedSourceName
		if name == "" {
			name = "current source"
		}
		fmt.Printf("unloaded %s (%d managed object(s))\n", name, res.UnloadedTrackedCount)
	}

	fmt.Printf("reloaded %s (%d tracked object(s))\n", res.SourceName, res.TrackedCount)
	if res.RemovedBackupCount > 0 {
		fmt.Printf("cleaned %d unreferenced backup object(s)\n", res.RemovedBackupCount)
	}
	printChangedPaths(cmd, res.ChangedPaths)
	return nil
}
