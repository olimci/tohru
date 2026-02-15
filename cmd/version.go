package cmd

import (
	"context"
	"fmt"

	"github.com/olimci/tohru/pkg/version"
	"github.com/urfave/cli/v3"
)

func versionCommand() *cli.Command {
	return &cli.Command{
		Name:    "version",
		Aliases: []string{"v"},
		Usage:   "show version",
		Action:  versionAction,
	}
}

func versionAction(_ context.Context, cmd *cli.Command) error {
	fmt.Printf("tohru version %s\n", version.Version)
	return nil
}
