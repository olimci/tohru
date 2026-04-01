package main

import (
	"context"
	"fmt"
	"os"

	"github.com/olimci/tohru/cmd"
)

func main() {
	if err := cmd.Execute(context.Background(), os.Args); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
