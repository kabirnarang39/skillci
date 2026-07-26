package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var version = "dev"

// newRootCmd wires every subcommand onto the root — split out from main
// so the wiring itself (every subcommand actually registered, under the
// name it's supposed to have) is testable without going through
// os.Exit.
func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:     "skillci",
		Short:   "Lint, eval, and regression-test Claude Skills",
		Version: version,
		// Cobra's default is to print "Error: <err>" itself on any
		// RunE/parse error, in addition to main's own os.Exit(1) path
		// below printing the same error again — every failing command
		// was double-printing its error message.
		SilenceErrors: true,
	}
	root.AddCommand(newInitCmd())
	root.AddCommand(newScaffoldCmd())
	root.AddCommand(newCheckCmd())
	root.AddCommand(newEvalCmd())
	root.AddCommand(newRegressCmd())
	root.AddCommand(newAcceptCmd())
	root.AddCommand(newBadgeCmd())
	root.AddCommand(newDiffCmd())
	root.AddCommand(newFuzzCmd())
	root.AddCommand(newBisectCmd())
	root.AddCommand(newReportCmd())
	root.AddCommand(newMCPServeCmd())
	return root
}

func main() {
	if err := newRootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
