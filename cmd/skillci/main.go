package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var version = "dev"

func main() {
	root := &cobra.Command{
		Use:     "skillci",
		Short:   "Lint, eval, and regression-test Claude Skills",
		Version: version,
	}
	root.AddCommand(newCheckCmd())
	root.AddCommand(newEvalCmd())

	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
