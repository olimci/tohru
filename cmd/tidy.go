package cmd

import (
	"context"
	"fmt"

	storepkg "github.com/olimci/tohru/pkg/store"
	"github.com/urfave/cli/v3"
)

func tidyCommand() *cli.Command {
	return &cli.Command{
		Name:   "tidy",
		Usage:  "remove untracked backups",
		Action: tidyAction,
	}
}

func tidyAction(_ context.Context, cmd *cli.Command) error {
	args := cmd.Args().Slice()
	if len(args) > 0 {
		return fmt.Errorf("tidy does not accept arguments")
	}

	store, err := storepkg.DefaultStore()
	if err != nil {
		return err
	}

	if !store.IsInstalled() {
		return fmt.Errorf("tohru is not installed")
	}

	res, err := store.Tidy()
	if err != nil {
		return err
	}

	fmt.Printf("tidied backups (%d object(s) removed)\n", res.RemovedCount)
	printChangedPaths(cmd, res.ChangedPaths)
	return nil
}
