package cmd

import (
	"context"
	"fmt"

	"github.com/olimci/tohru/pkg/store"
	"github.com/urfave/cli/v3"
)

func installCommand() *cli.Command {
	return &cli.Command{
		Name:      "install",
		Usage:     "initialize tohru",
		ArgsUsage: "[source]",
		Action:    installAction,
		Flags: []cli.Flag{
			&cli.BoolFlag{
				Name:    "force",
				Aliases: []string{"f"},
				Usage:   "force installation even if tohru is already installed",
			},
		},
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
	opts := applyOptionsFromCommand(cmd)

	s, err := store.DefaultStore()
	if err != nil {
		return err
	}

	if s.IsInstalled() {
		return fmt.Errorf("tohru is already installed in %s", s.Root)
	}

	if err := s.Install(); err != nil {
		return err
	}

	fmt.Printf("initialized tohru store in %s\n", s.Root)
	printChangedPaths(cmd, []string{s.BackupsPath(), s.ConfigPath(), s.LockPath()})

	if source == "" {
		return nil
	}

	res, err := s.Load(source, opts)
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
