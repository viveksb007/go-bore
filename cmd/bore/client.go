package main

import (
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"syscall"

	"github.com/spf13/cobra"
	"github.com/viveksb007/go-bore/internal/client"
	"github.com/viveksb007/go-bore/internal/logging"
)

var (
	serverAddr   string
	clientSecret string
	localHost    string
)

// clientCmd represents the client command
var clientCmd = &cobra.Command{
	Use:   "client <port>",
	Short: "Expose a local port through a tunnel",
	Long: `Expose a local port to a remote bore server through a secure tunnel.
This allows external clients to access your local service via the server's public port.

Example:
  gobore client 8080 --to example.com:7835
  gobore client 3000 --to example.com:7835 --secret mytoken123
  gobore client 5000 --to example.com:7835 --local-host 192.168.1.100`,
	Args: cobra.ExactArgs(1),
	RunE: runClient,
}

func init() {
	rootCmd.AddCommand(clientCmd)

	clientCmd.Flags().StringVarP(&serverAddr, "to", "t", "", "Server address (host:port)")
	clientCmd.Flags().StringVarP(&clientSecret, "secret", "s", "", "Secret for authentication")
	clientCmd.Flags().StringVarP(&localHost, "local-host", "l", "localhost", "Local host to forward to")

	clientCmd.MarkFlagRequired("to")
}

func runClient(cmd *cobra.Command, args []string) error {
	// Parse port argument
	portStr := args[0]
	port, err := strconv.Atoi(portStr)
	if err != nil {
		logging.Error("invalid port number", "port", portStr, "error", err)
		return fmt.Errorf("invalid port number: %s", portStr)
	}

	// Validate port range
	if port < 1 || port > 65535 {
		logging.Error("port out of range", "port", port)
		return fmt.Errorf("port must be between 1 and 65535, got %d", port)
	}

	logging.Debug("connecting to server",
		"server", serverAddr,
		"localHost", localHost,
		"localPort", port,
		logging.SecretAttr("secret", clientSecret),
	)

	// Create and connect client
	c := client.NewClient(serverAddr, port, localHost, clientSecret)

	if err := c.Connect(); err != nil {
		logging.Error("failed to connect", "error", err)
		return err
	}

	logging.Info("tunnel established",
		"publicURL", c.PublicURL(),
		"localHost", localHost,
		"localPort", port,
	)

	// Set up signal handling for graceful shutdown
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	// Run the client message loop in a goroutine
	errCh := make(chan error, 1)
	go func() {
		errCh <- c.Run()
	}()

	// Wait for either signal or client error
	select {
	case sig := <-sigCh:
		logging.Info("received signal, shutting down", "signal", sig)
		c.Close()
		return nil
	case err := <-errCh:
		if err != nil {
			logging.Error("client error", "error", err)
		}
		c.Close()
		return err
	}
}
