package client

import (
	"context"
	"fmt"
	"io"
	"net"
	"sync"
	"time"

	"github.com/viveksb007/go-bore/internal/logging"
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
	ctx         context.Context          // Context for cancellation
	cancel      context.CancelFunc       // Cancel function for shutdown
	wg          sync.WaitGroup           // WaitGroup for goroutines
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

	ctx, cancel := context.WithCancel(context.Background())

	return &Client{
		serverAddr: serverAddr,
		localPort:  localPort,
		localHost:  localHost,
		secret:     secret,
		streams:    make(map[string]*ClientStream),
		ctx:        ctx,
		cancel:     cancel,
	}
}

// Connect establishes the control connection to the server and performs handshake
func (c *Client) Connect() error {
	logging.Debug("establishing control connection", "server", c.serverAddr)

	// Establish TCP connection to server
	conn, err := net.Dial("tcp", c.serverAddr)
	if err != nil {
		logging.Error("failed to connect to server", "server", c.serverAddr, "error", err)
		return fmt.Errorf("failed to connect to server %s: %w", c.serverAddr, err)
	}
	c.controlConn = conn

	logging.Debug("control connection established", "server", c.serverAddr)

	// Send handshake message
	if err := c.sendHandshake(); err != nil {
		c.controlConn.Close()
		c.controlConn = nil
		logging.Error("handshake failed", "error", err)
		return fmt.Errorf("handshake failed: %w", err)
	}

	// Wait for accept/reject response
	if err := c.receiveHandshakeResponse(); err != nil {
		c.controlConn.Close()
		c.controlConn = nil
		return err
	}

	logging.Info("connected to server",
		"server", c.serverAddr,
		"publicPort", c.publicPort,
	)

	return nil
}

// sendHandshake sends the handshake message to the server
func (c *Client) sendHandshake() error {
	logging.Debug("sending handshake",
		logging.SecretAttr("secret", c.secret),
	)

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

	logging.Debug("handshake sent")
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
		logging.Debug("handshake accepted",
			"publicPort", c.publicPort,
			"serverHost", c.serverHost,
		)
		return nil

	case protocol.MessageTypeReject:
		logging.Error("authentication failed")
		return fmt.Errorf("authentication failed")

	default:
		return fmt.Errorf("unexpected message type: %d", msg.Type)
	}
}

// Run starts the message loop on the control connection.
// It continuously reads messages from the server and handles them appropriately.
// This method blocks until the connection is closed or an error occurs.
func (c *Client) Run() error {
	if c.controlConn == nil {
		return fmt.Errorf("client not connected, call Connect() first")
	}

	// Message reading loop
	for {
		select {
		case <-c.ctx.Done():
			return c.ctx.Err()
		default:
		}

		msg, err := protocol.ReadMessage(c.controlConn)
		if err != nil {
			// Check if context was cancelled (graceful shutdown)
			select {
			case <-c.ctx.Done():
				return c.ctx.Err()
			default:
			}
			return fmt.Errorf("failed to read message from server: %w", err)
		}

		if err := c.handleMessage(msg); err != nil {
			return fmt.Errorf("failed to handle message: %w", err)
		}
	}
}

// handleMessage processes a message received from the server
func (c *Client) handleMessage(msg *protocol.Message) error {
	switch msg.Type {
	case protocol.MessageTypeConnect:
		// New external connection - spawn goroutine to handle it
		connectPayload, err := protocol.DecodeConnect(msg.Payload)
		if err != nil {
			return fmt.Errorf("failed to decode connect message: %w", err)
		}

		// Spawn goroutine to handle the new stream
		c.wg.Add(1)
		go func(uuid string) {
			defer c.wg.Done()
			c.handleNewStream(uuid)
		}(connectPayload.UUID)

		return nil

	default:
		return fmt.Errorf("unexpected message type in message loop: %d", msg.Type)
	}
}

// handleNewStream handles a new stream connection request from the server.
// This is called when the server sends a MessageTypeConnect with a UUID.
// It opens a data connection to the server, performs stream handshake,
// connects to the local service, and starts bidirectional proxying.
func (c *Client) handleNewStream(uuid string) {
	logging.Debug("handling new stream", "uuid", uuid)

	// Create stream entry to track this connection
	stream := &ClientStream{
		uuid:      uuid,
		closeCh:   make(chan struct{}),
		createdAt: time.Now(),
	}

	// Register stream
	c.streamsMu.Lock()
	c.streams[uuid] = stream
	c.streamsMu.Unlock()

	// Ensure cleanup on exit
	defer c.removeStream(uuid)

	// Step 1: Open data connection to server
	dataConn, err := net.DialTimeout("tcp", c.serverAddr, 10*time.Second)
	if err != nil {
		logging.Error("failed to open data connection", "uuid", uuid, "error", err)
		return
	}
	stream.dataConn = dataConn

	logging.Debug("data connection established", "uuid", uuid)

	// Step 2: Send MessageTypeStreamHandshake(UUID)
	streamHandshakePayload := &protocol.StreamHandshakePayload{
		UUID: uuid,
	}
	payloadBytes, err := protocol.EncodeStreamHandshake(streamHandshakePayload)
	if err != nil {
		logging.Error("failed to encode stream handshake", "uuid", uuid, "error", err)
		dataConn.Close()
		return
	}

	msg := &protocol.Message{
		Type:    protocol.MessageTypeStreamHandshake,
		Payload: payloadBytes,
	}
	if err := protocol.WriteMessage(dataConn, msg); err != nil {
		logging.Error("failed to send stream handshake", "uuid", uuid, "error", err)
		dataConn.Close()
		return
	}

	// Step 3: Wait for MessageTypeStreamAccept or MessageTypeStreamReject
	response, err := protocol.ReadMessage(dataConn)
	if err != nil {
		logging.Error("failed to read stream response", "uuid", uuid, "error", err)
		dataConn.Close()
		return
	}

	switch response.Type {
	case protocol.MessageTypeStreamAccept:
		logging.Debug("stream accepted", "uuid", uuid)
	case protocol.MessageTypeStreamReject:
		logging.Error("stream rejected by server", "uuid", uuid)
		dataConn.Close()
		return
	default:
		logging.Error("unexpected stream response", "uuid", uuid, "type", response.Type)
		dataConn.Close()
		return
	}

	// Step 4: Dial local service
	localAddr := fmt.Sprintf("%s:%d", c.localHost, c.localPort)
	localConn, err := net.DialTimeout("tcp", localAddr, 10*time.Second)
	if err != nil {
		logging.Error("failed to connect to local service", "uuid", uuid, "localAddr", localAddr, "error", err)
		dataConn.Close()
		return
	}
	stream.localConn = localConn

	logging.Info("stream created",
		"uuid", uuid,
		"localAddr", localAddr,
	)

	// Step 5: Start bidirectional proxy
	c.proxyStream(stream)

	logging.Debug("stream closed", "uuid", uuid)
}

// proxyStream starts bidirectional proxying between the data connection and local connection.
// It spawns two goroutines for each direction and waits for both to complete.
func (c *Client) proxyStream(stream *ClientStream) {
	var wg sync.WaitGroup
	wg.Add(2)

	// Goroutine 1: local → server (copy from local to data connection)
	go func() {
		defer wg.Done()
		io.Copy(stream.dataConn, stream.localConn)
		// When local connection closes or errors, close write side of data connection
		if tcpConn, ok := stream.dataConn.(*net.TCPConn); ok {
			tcpConn.CloseWrite()
		}
	}()

	// Goroutine 2: server → local (copy from data connection to local)
	go func() {
		defer wg.Done()
		io.Copy(stream.localConn, stream.dataConn)
		// When data connection closes or errors, close write side of local connection
		if tcpConn, ok := stream.localConn.(*net.TCPConn); ok {
			tcpConn.CloseWrite()
		}
	}()

	// Wait for both directions to complete
	wg.Wait()

	// Cleanup is handled by defer in handleNewStream
}

// PublicPort returns the public port allocated by the server
func (c *Client) PublicPort() uint16 {
	return c.publicPort
}

// ServerHost returns the server hostname from the accept message
func (c *Client) ServerHost() string {
	return c.serverHost
}

// PublicURL returns the public URL where the tunneled service is accessible
func (c *Client) PublicURL() string {
	if c.serverHost != "" {
		return fmt.Sprintf("%s:%d", c.serverHost, c.publicPort)
	}
	// Extract host from server address if serverHost is empty
	host, _, err := net.SplitHostPort(c.serverAddr)
	if err != nil {
		return fmt.Sprintf("localhost:%d", c.publicPort)
	}
	return fmt.Sprintf("%s:%d", host, c.publicPort)
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
	logging.Debug("closing client")

	// Cancel context to signal all goroutines to stop
	if c.cancel != nil {
		c.cancel()
	}

	// Close control connection first to unblock any pending reads
	if c.controlConn != nil {
		c.controlConn.Close()
		c.controlConn = nil
	}

	// Wait for all stream goroutines to finish
	c.wg.Wait()

	// Close all streams
	c.streamsMu.Lock()
	for _, stream := range c.streams {
		stream.close()
	}
	c.streams = make(map[string]*ClientStream)
	c.streamsMu.Unlock()

	logging.Info("client disconnected")
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

// StreamCount returns the number of active streams
func (c *Client) StreamCount() int {
	c.streamsMu.RLock()
	defer c.streamsMu.RUnlock()
	return len(c.streams)
}

// GetStream returns a stream by UUID (for testing purposes)
func (c *Client) GetStream(uuid string) *ClientStream {
	c.streamsMu.RLock()
	defer c.streamsMu.RUnlock()
	return c.streams[uuid]
}

// removeStream removes a stream from the client's stream map
func (c *Client) removeStream(uuid string) {
	c.streamsMu.Lock()
	defer c.streamsMu.Unlock()
	if stream, exists := c.streams[uuid]; exists {
		stream.close()
		delete(c.streams, uuid)
	}
}
