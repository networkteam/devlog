// Command devlog is a CLI that exposes a local devlog instance's Agent JSON API
// to AI agents over the Model Context Protocol.
package main

import (
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"
)

func main() {
	if err := rootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func rootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:           "devlog",
		Short:         "devlog agent relay",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.AddCommand(relayCmd())
	return root
}

func relayCmd() *cobra.Command {
	var direct string
	var session string
	var mcpPort int

	cmd := &cobra.Command{
		Use:   "relay",
		Short: "Serve a devlog instance to an AI agent over MCP",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if direct == "" {
				return errors.New("--direct <base-url> is required (browser relay mode is Phase 2)")
			}

			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()

			return runRelayDirect(ctx, direct, session, mcpPort)
		},
	}

	cmd.Flags().StringVar(&direct, "direct", "", "base URL of a local devlog dashboard, e.g. http://localhost:1095/_devlog")
	cmd.Flags().StringVar(&session, "session", "", "capture session id to attach to (default: auto-select or prompt)")
	cmd.Flags().IntVar(&mcpPort, "mcp-port", 0, "MCP server port on 127.0.0.1 (0 = random)")
	return cmd
}
