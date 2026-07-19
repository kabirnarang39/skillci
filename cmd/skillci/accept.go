package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
)

func newAcceptCmd() *cobra.Command {
	var path string
	cmd := &cobra.Command{
		Use:   "accept <case-name>",
		Short: "Promote a generated eval case from evals/_generated/ into evals/",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			src := filepath.Join(path, "evals", "_generated", name+".yaml")
			dst := filepath.Join(path, "evals", name+".yaml")

			data, err := os.ReadFile(src)
			if err != nil {
				return fmt.Errorf("reading generated case %s: %w", src, err)
			}
			if err := os.WriteFile(dst, data, 0o644); err != nil {
				return err
			}
			if err := os.Remove(src); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "accepted %s -> %s\n", src, dst)
			return nil
		},
	}
	cmd.Flags().StringVar(&path, "path", ".", "skill directory")
	return cmd
}
