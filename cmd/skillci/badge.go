package main

import (
	"fmt"
	"os"
	"path/filepath"

	skillbadge "github.com/kabirnarang/skillci/internal/badge"
	"github.com/kabirnarang/skillci/internal/history"
	"github.com/spf13/cobra"
)

func newBadgeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "badge [path]",
		Short: "Regenerate the SVG badge from the latest recorded history",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir := "."
			if len(args) == 1 {
				dir = args[0]
			}
			hist, err := history.Load(filepath.Join(dir, ".skillci", "history.json"))
			if err != nil {
				return err
			}
			last, ok := hist.LastRun()
			if !ok {
				return fmt.Errorf("no history found — run `skillci regress` first")
			}
			state := skillbadge.StateFromRun(last)
			return os.WriteFile(filepath.Join(dir, ".skillci", "badge.svg"), []byte(skillbadge.Render(state)), 0o644)
		},
	}
}
