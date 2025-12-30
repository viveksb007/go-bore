package main

import (
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"
	"github.com/viveksb007/go-bore/internal/logging"
	"github.com/viveksb007/go-bore/internal/server"
)

var (
	serverPort      int
	serverSecret    string
	healthCheckPort int
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
	serverCmd.Flags().IntVar(&healthCheckPort, "health-port", 0, "Port for health check HTTP server (0 to disable)")
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

	// Start health check server if port is specified
	var healthServer *server.HealthServer
	if healthCheckPort > 0 {
		healthServer = server.NewHealthServer(healthCheckPort)
		if err := healthServer.Start(); err != nil {
			logging.Error("failed to start health check server", "error", err)
			return err
		}
	}

	srv := server.NewServer(serverPort, serverSecret)

	// Set up signal handling for graceful shutdown
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	// Run the server in a goroutine
	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.Run()
	}()

	// Wait for either signal or server error
	select {
	case sig := <-sigCh:
		logging.Info("received signal, shutting down", "signal", sig)
		if healthServer != nil {
			healthServer.Stop()
		}
		srv.Close()
		return nil
	case err := <-errCh:
		if err != nil {
			logging.Error("server error", "error", err)
		}
		if healthServer != nil {
			healthServer.Stop()
		}
		return err
	}
}
