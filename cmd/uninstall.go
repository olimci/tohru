package cmd

import (
	"context"
	"fmt"

	storepkg "github.com/olimci/tohru/pkg/store"
	"github.com/urfave/cli/v3"
)

func uninstallCommand() *cli.Command {
	return &cli.Command{
		Name:  "uninstall",
		Usage: "uninstall tohru",
		Flags: []cli.Flag{
			&cli.BoolFlag{
				Name:    "force",
				Aliases: []string{"f"},
				Usage:   "force unload of modified managed files before uninstalling",
			},
		},
		Action: uninstallAction,
	}
}

func uninstallAction(_ context.Context, cmd *cli.Command) error {
	args := cmd.Args().Slice()

	if len(args) > 0 {
		return fmt.Errorf("uninstall does not accept arguments")
	}
	force := cmd.Bool("force")

	store, err := storepkg.DefaultStore()
	if err != nil {
		return err
	}

	if !store.IsInstalled() {
		return fmt.Errorf("tohru is not installed")
	}

	unloadRes, err := store.Unload(force)
	if err != nil {
		return err
	}
	if unloadRes.SourceName != "" || unloadRes.RemovedCount > 0 {
		name := unloadRes.SourceName
		if name == "" {
			name = "source"
		}
		fmt.Printf("unloaded %s (%d managed object(s))\n", name, unloadRes.RemovedCount)
	}
	if unloadRes.RemovedBackupCount > 0 {
		fmt.Printf("cleaned %d unreferenced backup object(s)\n", unloadRes.RemovedBackupCount)
	}
	printChangedPaths(cmd, unloadRes.ChangedPaths)

	if err := store.Uninstall(); err != nil {
		return err
	}
	printChangedPaths(cmd, []string{store.Root})

	fmt.Printf("uninstalled tohru store from %s\n", store.Root)
	return nil
}
