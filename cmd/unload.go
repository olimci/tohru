package cmd

import (
	"context"
	"fmt"
	"strings"

	"github.com/olimci/tohru/pkg/store"
	"github.com/urfave/cli/v3"
)

func unloadCommand() *cli.Command {
	return &cli.Command{
		Name:  "unload",
		Usage: "unload the currently loaded profile",
		Flags: []cli.Flag{
			&cli.BoolFlag{
				Name:    "force",
				Aliases: []string{"f"},
				Usage:   "force unload, even with missing/changed paths or restore conflicts",
			},
			&cli.BoolFlag{
				Name:  "discard-changes",
				Usage: "allow removing modified managed files without enabling full force behavior",
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
	opts := cmdOptions(cmd)

	s, err := store.DefaultStore()
	if err != nil {
		return err
	}

	if !s.IsInstalled() {
		return fmt.Errorf("tohru is not installed")
	}

	lck, err := s.LoadLock()
	if err != nil {
		return err
	}
	if strings.ToLower(lck.Manifest.State) != "loaded" && len(lck.Files) == 0 {
		fmt.Println("nothing to unload")
		return nil
	}

	res, err := s.Unload(opts)
	if err != nil {
		return err
	}

	name := res.ProfileName
	if name == "" {
		name = "profile"
	}
	fmt.Printf("unloaded %s (%d managed object(s))\n", name, res.RemovedCount)
	if res.RemovedBackupCount > 0 {
		fmt.Printf("cleaned %d unreferenced backup object(s)\n", res.RemovedBackupCount)
	}
	printChanges(cmd, res.ChangedPaths)
	return nil
}
