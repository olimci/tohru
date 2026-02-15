package cmd

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/olimci/tohru/pkg/manifest"
	"github.com/olimci/tohru/pkg/store"
	"github.com/urfave/cli/v3"
)

func validateCommand() *cli.Command {
	return &cli.Command{
		Name:      "validate",
		Usage:     "validate a source manifest without applying changes",
		ArgsUsage: "[source]",
		Flags: []cli.Flag{
			&cli.BoolFlag{
				Name:  "tree",
				Usage: "print the resolved import tree",
			},
		},
		Action: validateAction,
	}
}

func validateAction(_ context.Context, cmd *cli.Command) error {
	args := cmd.Args().Slice()
	source := ""

	if len(args) > 1 {
		return fmt.Errorf("validate accepts at most one optional source argument")
	}
	if len(args) == 1 {
		source = args[0]
	}

	s, err := store.DefaultStore()
	if err != nil {
		return err
	}

	res, err := s.Validate(source)
	if err != nil {
		return err
	}

	fmt.Printf(
		"validated %s (%d operation(s): %d link(s), %d file(s), %d dir(s))\n",
		res.SourceName,
		res.OpCount,
		res.LinkCount,
		res.FileCount,
		res.DirCount,
	)
	if cmd.Bool("tree") {
		fmt.Println("resolved import tree:")
		printImportTree(res.ImportTree, res.SourceDir, "", true)
	}
	return nil
}

func printImportTree(tree manifest.ImportTree, sourceDir, prefix string, isLast bool) {
	label := formatTreeLabel(tree.Path, sourceDir)
	if prefix == "" {
		fmt.Printf("- %s\n", label)
	} else {
		branch := "|- "
		if isLast {
			branch = "`- "
		}
		fmt.Printf("%s%s%s\n", prefix, branch, label)
	}

	nextPrefix := prefix
	if prefix != "" {
		if isLast {
			nextPrefix += "   "
		} else {
			nextPrefix += "|  "
		}
	} else {
		nextPrefix = "   "
	}

	for i, child := range tree.Imports {
		printImportTree(child, sourceDir, nextPrefix, i == len(tree.Imports)-1)
	}
}

func formatTreeLabel(path, sourceDir string) string {
	rel, err := filepath.Rel(sourceDir, path)
	if err == nil {
		rel = filepath.ToSlash(rel)
		if rel == "." {
			return "tohru.toml"
		}
		if rel != ".." && !strings.HasPrefix(rel, "../") {
			return rel
		}
	}
	return path
}
