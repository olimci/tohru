package cmd

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	storepkg "github.com/olimci/tohru/pkg/store"
	"github.com/urfave/cli/v3"
)

func statusCommand() *cli.Command {
	return &cli.Command{
		Name:  "status",
		Usage: "show tracked objects",
		Flags: []cli.Flag{
			&cli.BoolFlag{
				Name:  "backups",
				Usage: "show backups-only status details",
			},
		},
		Action: statusAction,
	}
}

func statusAction(_ context.Context, cmd *cli.Command) error {
	args := cmd.Args().Slice()
	if len(args) > 0 {
		return fmt.Errorf("status does not accept arguments")
	}

	store, err := storepkg.DefaultStore()
	if err != nil {
		return err
	}
	if !store.IsInstalled() {
		return fmt.Errorf("tohru is not installed")
	}

	snapshot, err := store.Status()
	if err != nil {
		return err
	}
	backupsOnly := cmd.Bool("backups")

	if backupsOnly {
		renderBackupStatus(snapshot)
		return nil
	}

	manifestState := strings.ToLower(snapshot.Manifest.State)
	if manifestState == "loaded" && strings.TrimSpace(snapshot.Manifest.Loc) != "" {
		fmt.Printf("On source %s\n", sourceLabel(snapshot.Manifest.Name, snapshot.Manifest.Loc))
	} else {
		fmt.Println("No source loaded")
	}
	fmt.Printf("Manifest state: %s\n", snapshot.Manifest.State)

	fmt.Println()
	fmt.Println("Tracked objects:")
	if len(snapshot.Tracked) == 0 {
		fmt.Println("  (none)")
	} else {
		for _, tracked := range snapshot.Tracked {
			switch {
			case tracked.PrevDigest == "":
				fmt.Printf("  T  %s\n", tracked.Path)
			case tracked.BackupPresent:
				fmt.Printf("  B  %s\n", tracked.Path)
			default:
				fmt.Printf("  !  %s\n", tracked.Path)
			}
		}
	}

	fmt.Println()
	renderBackupStatus(snapshot)

	return nil
}

func renderBackupStatus(snapshot storepkg.StatusSnapshot) {
	fmt.Println("Backups referenced by lock:")
	if len(snapshot.BackupRefs) == 0 {
		fmt.Println("  (none)")
	} else {
		for _, ref := range snapshot.BackupRefs {
			stateLabel := "missing"
			if ref.Present {
				stateLabel = "present"
			}
			fmt.Printf("  %s  %s\n", stateLabel, ref.Digest)
			for _, path := range ref.Paths {
				fmt.Printf("       %s\n", path)
			}
		}
	}

	fmt.Println()
	fmt.Println("Unreferenced backup objects:")
	if len(snapshot.OrphanedBackups) == 0 {
		fmt.Println("  (none)")
	} else {
		for _, cid := range snapshot.OrphanedBackups {
			fmt.Printf("  orphan  %s\n", cid)
		}
	}

	if len(snapshot.BrokenBackups) > 0 {
		fmt.Println()
		fmt.Println("Broken backup entries:")
		for _, cid := range snapshot.BrokenBackups {
			fmt.Printf("  broken  %s\n", cid)
		}
	}
}

func sourceLabel(name, loc string) string {
	trimmedName := strings.TrimSpace(name)
	if trimmedName != "" {
		return trimmedName
	}
	trimmedLoc := strings.TrimSpace(loc)
	if trimmedLoc == "" {
		return ""
	}
	return filepath.Base(filepath.Clean(trimmedLoc))
}
