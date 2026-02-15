package cmd

import (
	"context"
	"fmt"
	"strings"

	storepkg "github.com/olimci/tohru/pkg/store"
	"github.com/urfave/cli/v3"
)

func unloadCommand() *cli.Command {
	return &cli.Command{
		Name:  "unload",
		Usage: "unload the currently loaded source",
		Flags: []cli.Flag{
			&cli.BoolFlag{
				Name:    "force",
				Aliases: []string{"f"},
				Usage:   "remove modified managed files",
			},
		},
		Action: unloadAction,
	}
}

func unloadAction(_ context.Context, cmd *cli.Command) error {
	args := cmd.Args().Slice()
	if len(args) > 0 {
		return fmt.Errorf("unload does not accept arguments")
	}
	force := cmd.Bool("force")

	store, err := storepkg.DefaultStore()
	if err != nil {
		return err
	}

	if !store.IsInstalled() {
		return fmt.Errorf("tohru is not installed")
	}

	lck, err := store.LoadLock()
	if err != nil {
		return err
	}
	if strings.ToLower(lck.Manifest.State) != "loaded" && len(lck.Files) == 0 {
		fmt.Println("nothing to unload")
		return nil
	}

	removed, err := store.Unload(force)
	if err != nil {
		return err
	}

	fmt.Printf("unloaded source (%d managed object(s))\n", removed)
	return nil
}
