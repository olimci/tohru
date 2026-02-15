package cmd

import (
	"fmt"

	"github.com/urfave/cli/v3"
)

func isVerbose(cmd *cli.Command) bool {
	if cmd == nil {
		return false
	}
	if cmd.Bool("verbose") {
		return true
	}
	root := cmd.Root()
	return root != nil && root.Bool("verbose")
}

func printChangedPaths(cmd *cli.Command, paths []string) {
	if !isVerbose(cmd) || len(paths) == 0 {
		return
	}
	fmt.Println("changed paths:")
	for _, path := range paths {
		fmt.Printf("  %s\n", path)
	}
}
