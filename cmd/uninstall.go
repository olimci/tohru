package cmd

import (
	"context"
	"fmt"

	"github.com/olimci/tohru/pkg/store"
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
			&cli.BoolFlag{
				Name:  "discard-changes",
				Usage: "allow uninstall to remove modified managed files without full force behavior",
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
	opts := applyOptionsFromCommand(cmd)

	s, err := store.DefaultStore()
	if err != nil {
		return err
	}

	if !s.IsInstalled() {
		return fmt.Errorf("tohru is not installed")
	}

	unloadRes, err := s.Unload(opts)
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

	if err := s.Uninstall(); err != nil {
		return err
	}
	printChangedPaths(cmd, []string{s.Root})

	fmt.Printf("uninstalled tohru store from %s\n", s.Root)
	return nil
}
