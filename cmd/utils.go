package cmd

import (
	"fmt"

	"github.com/olimci/tohru/pkg/store"
	"github.com/urfave/cli/v3"
)

func cmdOptions(cmd *cli.Command) store.Options {
	if cmd == nil {
		return store.Options{}
	}
	return store.Options{
		Force:          cmd.Bool("force"),
		DiscardChanges: cmd.Bool("discard-changes"),
	}
}

func verbose(cmd *cli.Command) bool {
	if cmd == nil {
		return false
	}
	if cmd.Bool("verbose") {
		return true
	}
	root := cmd.Root()
	return root != nil && root.Bool("verbose")
}

func printChanges(cmd *cli.Command, paths []string) {
	if !verbose(cmd) || len(paths) == 0 {
		return
	}
	fmt.Println("changed paths:")
	for _, path := range paths {
		fmt.Printf("  %s\n", path)
	}
}
