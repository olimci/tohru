package cmd

import (
	"context"
	"fmt"

	storepkg "github.com/olimci/tohru/pkg/store"
	"github.com/urfave/cli/v3"
)

func installCommand() *cli.Command {
	return &cli.Command{
		Name:   "install",
		Usage:  "initialize tohru",
		Action: installAction,
	}
}

func installAction(_ context.Context, cmd *cli.Command) error {
	args := cmd.Args().Slice()

	if len(args) > 0 {
		return fmt.Errorf("install does not accept arguments")
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
	return nil
}
