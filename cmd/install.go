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
		ArgsUsage: "[profile]",
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
	profile := ""

	if len(args) > 1 {
		return fmt.Errorf("install accepts at most one optional profile argument")
	}
	if len(args) == 1 {
		profile = args[0]
	}
	opts := cmdOptions(cmd)

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
	printChanges(cmd, []string{s.BackupsPath(), s.ProfilesPath(), s.ConfigPath(), s.LockPath(), s.ProfilesFilePath()})

	if profile == "" {
		return nil
	}

	res, err := s.Load(profile, opts)
	if err != nil {
		return err
	}

	if res.UnloadedProfileName != "" || res.UnloadedTrackedCount > 0 {
		name := res.UnloadedProfileName
		if name == "" {
			name = "previous profile"
		}
		fmt.Printf("unloaded %s (%d managed object(s))\n", name, res.UnloadedTrackedCount)
	}
	fmt.Printf("loaded %s (%d tracked object(s))\n", res.ProfileName, res.TrackedCount)
	if res.RemovedBackupCount > 0 {
		fmt.Printf("cleaned %d unreferenced backup object(s)\n", res.RemovedBackupCount)
	}
	printChanges(cmd, res.ChangedPaths)
	return nil
}
