package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
)

const defaultConfigYAML = `models: [claude-sonnet-5, claude-opus-4-8, claude-haiku-4-5]
fail_on: regression
`

const exampleEvalYAML = `name: "example-case"
prompt: "Write a prompt that should trigger this skill."
skill_under_test: "REPLACE_WITH_YOUR_SKILL_NAME"
assert:
  triggered: true
  contains: ["REPLACE_WITH_EXPECTED_SUBSTRING"]
`

func newInitCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "init [path]",
		Short: "Scaffold .skillci.yaml and an example eval case for a skill",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir := "."
			if len(args) == 1 {
				dir = args[0]
			}

			cfgPath := filepath.Join(dir, ".skillci.yaml")
			if _, err := os.Stat(cfgPath); err == nil {
				return fmt.Errorf("%s already exists, refusing to overwrite", cfgPath)
			}
			if err := os.WriteFile(cfgPath, []byte(defaultConfigYAML), 0o644); err != nil {
				return err
			}

			evalsDir := filepath.Join(dir, "evals")
			if err := os.MkdirAll(evalsDir, 0o755); err != nil {
				return err
			}
			examplePath := filepath.Join(evalsDir, "example.yaml")
			if _, err := os.Stat(examplePath); os.IsNotExist(err) {
				if err := os.WriteFile(examplePath, []byte(exampleEvalYAML), 0o644); err != nil {
					return err
				}
			}

			fmt.Fprintf(cmd.OutOrStdout(), "scaffolded %s and %s\n", cfgPath, examplePath)
			return nil
		},
	}
}
