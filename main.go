package main

import (
	"context"
	_ "embed"
	"fmt"
	"os"

	"github.com/olimci/tohru/cmd"
)

//go:embed title.txt
var titleArt string

func main() {
	if err := cmd.Execute(context.Background(), os.Args, titleArt); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
