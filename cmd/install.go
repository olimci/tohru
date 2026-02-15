package cmd

import (
	"context"
	"fmt"

	storepkg "github.com/olimci/tohru/pkg/store"
	"github.com/urfave/cli/v3"
)

func installCommand() *cli.Command {
	return &cli.Command{
		Name:      "install",
		Usage:     "initialize tohru",
		ArgsUsage: "[source]",
		Action:    installAction,
	}
}

func installAction(_ context.Context, cmd *cli.Command) error {
	args := cmd.Args().Slice()
	source := ""

	if len(args) > 1 {
		return fmt.Errorf("install accepts at most one optional source argument")
	}
	if len(args) == 1 {
		source = args[0]
	}

	store, err := storepkg.DefaultStore()
	if err != nil {
		return err
	}

	if store.IsInstalled() {
		return fmt.Errorf("tohru is already installed in %s", store.Root)
	}

	if err := store.Install(); err != nil {
		return err
	}

	fmt.Printf("initialized tohru store in %s\n", store.Root)
	printChangedPaths(cmd, []string{store.BackupsPath(), store.ConfigPath(), store.LockPath()})

	if source == "" {
		return nil
	}

	res, err := store.Load(source, false)
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
