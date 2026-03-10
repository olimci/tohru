package cmd

import (
	"fmt"

	"github.com/olimci/tohru/pkg/store"
	"github.com/urfave/cli/v3"
)

func cmdOptions(cmd *cli.Command) store.Options {
	return store.Options{
		Force:          cmd.Bool("force"),
		DiscardChanges: cmd.Bool("discard-changes"),
	}
}

func printChanges(cmd *cli.Command, paths []string) {
	if len(paths) == 0 {
		return
	}
	isVerbose := cmd.Bool("verbose")
	if !isVerbose {
		isVerbose = cmd.Root().Bool("verbose")
	}
	if !isVerbose {
		return
	}
	fmt.Println("changed paths:")
	for _, path := range paths {
		fmt.Printf("  %s\n", path)
	}
}

func printWarnings(warnings []string) {
	for _, warning := range warnings {
		if warning == "" {
			continue
		}
		fmt.Printf("warning: %s\n", warning)
	}
}
