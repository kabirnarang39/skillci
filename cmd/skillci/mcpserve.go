package main

import (
	"os"

	"github.com/kabirnarang39/skillci/internal/mcpserver"
	"github.com/spf13/cobra"
)

func newMCPServeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "mcp-serve",
		Short: "Run skillci as an MCP server over stdio",
		Long: `Run skillci as a Model Context Protocol (MCP) server over stdio, so an
agent calls skillci commands as native tool calls instead of shelling out
to the CLI.

Exposes every skillci command except mcp-serve itself — init, check,
eval, regress, fuzz, bisect, accept, diff, badge, report — each invoked
as the real cobra command in-process, so a tool call behaves identically
to running the equivalent CLI command directly and can never silently
drift from it as commands gain new flags over time.

Add to an MCP client's config (e.g. Claude Code) pointing at the compiled
skillci binary with this subcommand — see the main README for an example.`,
		SilenceUsage: true,
		Args:         cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return mcpserver.Serve(os.Stdin, os.Stdout, version, mcpTools())
		},
	}
}
