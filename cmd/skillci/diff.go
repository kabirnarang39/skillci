package main

import (
	"fmt"

	"github.com/kabirnarang39/skillci/internal/snapshot"
	"github.com/spf13/cobra"
)

func newDiffCmd() *cobra.Command {
	var path, model string
	cmd := &cobra.Command{
		Use:   "diff <case-name>",
		Short: "Show a case's pending snapshot change against its accepted golden baseline",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]

			golden, ok, err := snapshot.Load(path, name, model)
			if err != nil {
				return err
			}
			if !ok {
				return fmt.Errorf("no golden baseline for %s (%s) — run `skillci regress` first", name, model)
			}

			pending, ok, err := snapshot.LoadPending(path, name, model)
			if err != nil {
				return err
			}
			if !ok {
				fmt.Fprintf(cmd.OutOrStdout(), "no pending snapshot change for %s (%s)\n", name, model)
				return nil
			}

			diff := snapshot.Compute(golden, pending)
			if !diff.Changed {
				fmt.Fprintf(cmd.OutOrStdout(), "no pending snapshot change for %s (%s)\n", name, model)
				return nil
			}
			fmt.Fprintln(cmd.OutOrStdout(), diff.Render)
			return nil
		},
	}
	cmd.Flags().StringVar(&path, "path", ".", "skill directory")
	cmd.Flags().StringVar(&model, "model", "claude-sonnet-5", "model to inspect")
	return cmd
}
