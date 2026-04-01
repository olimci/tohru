package cmd

import (
	"context"
	"fmt"

	"github.com/olimci/tohru/pkg/version"
	"github.com/urfave/cli/v3"
)

func versionCommand() *cli.Command {
	return &cli.Command{
		Name:  "version",
		Usage: "print the version of tohru",
		Action: func(_ context.Context, _ *cli.Command) error {
			fmt.Printf("tohru version %s\n", version.Version)
			return nil
		},
	}
}
