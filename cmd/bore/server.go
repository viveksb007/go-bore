package main

import (
	"github.com/spf13/cobra"
	"github.com/viveksb007/go-bore/internal/logging"
	"github.com/viveksb007/go-bore/internal/server"
)

var (
	serverPort   int
	serverSecret string
)

// serverCmd represents the server command
var serverCmd = &cobra.Command{
	Use:   "server",
	Short: "Start a bore server",
	Long: `Start a bore server that accepts tunnel connections from clients.
The server listens for client connections and allocates public ports for tunneling.

Example:
  gobore server --port 7835
  gobore server --port 7835 --secret mytoken123`,
	RunE: runServer,
}

func init() {
	rootCmd.AddCommand(serverCmd)

	serverCmd.Flags().IntVarP(&serverPort, "port", "p", 7835, "Port to listen for client connections")
	serverCmd.Flags().StringVarP(&serverSecret, "secret", "s", "", "Secret for client authentication")
}

func runServer(cmd *cobra.Command, args []string) error {
	authMode := "open mode (no authentication)"
	if serverSecret != "" {
		authMode = "authentication enabled"
	}

	logging.Info("starting server",
		"port", serverPort,
		"authMode", authMode,
	)

	logging.Debug("server configuration",
		"port", serverPort,
		logging.SecretAttr("secret", serverSecret),
	)

	srv := server.NewServer(serverPort, serverSecret)

	if err := srv.Run(); err != nil {
		logging.Error("server error", "error", err)
		return err
	}

	return nil
}
