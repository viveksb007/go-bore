package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/viveksb007/go-bore/internal/logging"
)

// Version information (can be set at build time)
var (
	version = "0.1.0"
	verbose bool
)

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use:   "gobore",
	Short: "A TCP port tunneling tool",
	Long: `gobore is a CLI tool that enables users to expose local ports to remote servers
through secure tunneling. It consists of a client that runs on the local machine
and a server that receives external traffic and tunnels it back to the client.`,
	Version: version,
	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		// Initialize logging based on verbose flag
		logging.Init(verbose)
	},
}

// Execute adds all child commands to the root command and sets flags appropriately.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func init() {
	// Global flags
	rootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "Enable verbose output (DEBUG level logging)")
}
