package cmd

import (
	"context"
	"errors"
	"fmt"

	"github.com/olimci/tohru/pkg/store"
	"github.com/urfave/cli/v3"
)

func reloadCommand() *cli.Command {
	return &cli.Command{
		Name:  "reload",
		Usage: "reload the currently loaded profile",
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
	opts := cmdOptions(cmd)

	s, err := store.DefaultStore()
	if err != nil {
		return err
	}

	res, err := s.Reload(opts)
	if err != nil {
		if errors.Is(err, store.ErrNotInstalled) {
			return fmt.Errorf("tohru is not installed, run `tohru install` first")
		}
		return err
	}

	if res.UnloadedProfileName != "" || res.UnloadedTrackedCount > 0 {
		name := res.UnloadedProfileName
		if name == "" {
			name = "current profile"
		}
		fmt.Printf("unloaded %s (%d managed object(s))\n", name, res.UnloadedTrackedCount)
	}

	fmt.Printf("reloaded %s (%d tracked object(s))\n", res.ProfileName, res.TrackedCount)
	if res.RemovedBackupCount > 0 {
		fmt.Printf("cleaned %d unreferenced backup object(s)\n", res.RemovedBackupCount)
	}
	printWarnings(res.Warnings)
	printChanges(cmd, res.ChangedPaths)
	return nil
}
