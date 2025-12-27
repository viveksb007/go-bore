package client

import (
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/viveksb007/go-bore/internal/protocol"
)

// Client manages the tunnel connection to a bore server
type Client struct {
	serverAddr  string                   // Server address (host:port)
	localPort   int                      // Local port to expose
	localHost   string                   // Local host (default: localhost)
	secret      string                   // Authentication secret
	controlConn net.Conn                 // Control connection to server (persistent)
	publicPort  uint16                   // Public port allocated by server
	serverHost  string                   // Server hostname from accept message
	streams     map[string]*ClientStream // Active proxied connections (UUID -> ClientStream)
	streamsMu   sync.RWMutex             // Protects streams map
}

// ClientStream represents a proxied connection on the client side
type ClientStream struct {
	uuid      string
	dataConn  net.Conn      // Data connection to server
	localConn net.Conn      // Connection to local service
	closeCh   chan struct{} // Signal for cleanup
	createdAt time.Time     // For monitoring/debugging
}

// NewClient creates a new Client instance with the specified configuration
func NewClient(serverAddr string, localPort int, localHost string, secret string) *Client {
	// Use default local host if not specified
	if localHost == "" {
		localHost = "localhost"
	}

	return &Client{
		serverAddr: serverAddr,
		localPort:  localPort,
		localHost:  localHost,
		secret:     secret,
		streams:    make(map[string]*ClientStream),
	}
}

// Connect establishes the control connection to the server and performs handshake
func (c *Client) Connect() error {
	// Establish TCP connection to server
	conn, err := net.Dial("tcp", c.serverAddr)
	if err != nil {
		return fmt.Errorf("failed to connect to server %s: %w", c.serverAddr, err)
	}
	c.controlConn = conn

	// Send handshake message
	if err := c.sendHandshake(); err != nil {
		c.controlConn.Close()
		c.controlConn = nil
		return fmt.Errorf("handshake failed: %w", err)
	}

	// Wait for accept/reject response
	if err := c.receiveHandshakeResponse(); err != nil {
		c.controlConn.Close()
		c.controlConn = nil
		return err
	}

	return nil
}

// sendHandshake sends the handshake message to the server
func (c *Client) sendHandshake() error {
	handshakePayload := &protocol.HandshakePayload{
		Version: protocol.ProtocolVersion,
		Secret:  c.secret,
	}

	payloadBytes, err := protocol.EncodeHandshake(handshakePayload)
	if err != nil {
		return fmt.Errorf("failed to encode handshake: %w", err)
	}

	msg := &protocol.Message{
		Type:    protocol.MessageTypeHandshake,
		Payload: payloadBytes,
	}

	if err := protocol.WriteMessage(c.controlConn, msg); err != nil {
		return fmt.Errorf("failed to send handshake: %w", err)
	}

	return nil
}

// receiveHandshakeResponse waits for and processes the accept/reject response from server
func (c *Client) receiveHandshakeResponse() error {
	msg, err := protocol.ReadMessage(c.controlConn)
	if err != nil {
		return fmt.Errorf("failed to read server response: %w", err)
	}

	switch msg.Type {
	case protocol.MessageTypeAccept:
		acceptPayload, err := protocol.DecodeAccept(msg.Payload)
		if err != nil {
			return fmt.Errorf("failed to decode accept message: %w", err)
		}
		c.publicPort = acceptPayload.PublicPort
		c.serverHost = acceptPayload.ServerHost
		return nil

	case protocol.MessageTypeReject:
		return fmt.Errorf("authentication failed")

	default:
		return fmt.Errorf("unexpected message type: %d", msg.Type)
	}
}

// PublicPort returns the public port allocated by the server
func (c *Client) PublicPort() uint16 {
	return c.publicPort
}

// ServerHost returns the server hostname from the accept message
func (c *Client) ServerHost() string {
	return c.serverHost
}

// LocalPort returns the local port being exposed
func (c *Client) LocalPort() int {
	return c.localPort
}

// LocalHost returns the local host being used
func (c *Client) LocalHost() string {
	return c.localHost
}

// ServerAddr returns the server address
func (c *Client) ServerAddr() string {
	return c.serverAddr
}

// Close closes the client connection and cleans up resources
func (c *Client) Close() error {
	// Close all streams
	c.streamsMu.Lock()
	for _, stream := range c.streams {
		stream.close()
	}
	c.streams = make(map[string]*ClientStream)
	c.streamsMu.Unlock()

	// Close control connection
	if c.controlConn != nil {
		c.controlConn.Close()
		c.controlConn = nil
	}

	return nil
}

// close closes a client stream and cleans up resources
func (s *ClientStream) close() {
	if s.dataConn != nil {
		s.dataConn.Close()
	}
	if s.localConn != nil {
		s.localConn.Close()
	}
	// Close channel only once using select to avoid panic
	select {
	case <-s.closeCh:
		// Already closed
	default:
		close(s.closeCh)
	}
}
