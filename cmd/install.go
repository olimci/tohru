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
				Usage:   "treat an existing install as success and still process the optional profile",
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

	alreadyInstalled := s.IsInstalled()
	if alreadyInstalled && !opts.Force {
		return fmt.Errorf("tohru is already installed in %s", s.Root)
	}

	var res store.LoadResult
	switch {
	case alreadyInstalled && profile != "":
		res, err = s.Load(profile, opts)
	case alreadyInstalled:
		fmt.Printf("tohru is already installed in %s\n", s.Root)
		return nil
	default:
		res, err = s.InstallAndLoad(profile, opts)
	}
	if err != nil {
		return err
	}

	if !alreadyInstalled {
		fmt.Printf("initialized tohru store in %s\n", s.Root)
		printChanges(cmd, []string{s.BackupsPath(), s.ProfilesPath(), s.ConfigPath(), s.StatePath(), s.ProfilesFilePath()})
	}

	if profile == "" {
		return nil
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
	printWarnings(res.Warnings)
	printChanges(cmd, res.ChangedPaths)
	return nil
}
