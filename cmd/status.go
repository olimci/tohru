package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/olimci/tohru/pkg/store"
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
			&cli.BoolFlag{
				Name:  "json",
				Usage: "print full status as JSON",
			},
			&cli.BoolFlag{
				Name:  "flat",
				Usage: "show compact flat status output",
			},
			&cli.StringFlag{
				Name:  "color",
				Usage: "color mode: auto|always|never",
				Value: "auto",
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

	s, err := store.DefaultStore()
	if err != nil {
		return err
	}

	snapshot, err := s.Status()
	if err != nil {
		return err
	}

	if cmd.Bool("json") {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(snapshot)
	}

	backups := cmd.Bool("backups")

	if backups {
		output, err := renderBackups(snapshot, statusRenderOptions{
			ColorMode: cmd.String("color"),
			Stdout:    os.Stdout,
		})
		if err != nil {
			return err
		}
		_, err = fmt.Fprint(os.Stdout, output)
		return err
	}

	output, err := renderStatus(snapshot, statusRenderOptions{
		Flat:      cmd.Bool("flat"),
		ColorMode: cmd.String("color"),
		Stdout:    os.Stdout,
	})
	if err != nil {
		return err
	}
	_, err = fmt.Fprint(os.Stdout, output)
	return err
}
