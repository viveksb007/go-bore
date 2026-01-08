package server

import (
	"crypto/subtle"
	"fmt"
	"io"
	"net"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/viveksb007/go-bore/internal/logging"
	"github.com/viveksb007/go-bore/internal/protocol"
)

// Server manages client connections and port tunneling
type Server struct {
	listenPort     int
	secret         string
	clients        map[string]*ClientSession
	clientsMu      sync.RWMutex
	portAllocator  *PortAllocator
	listener       net.Listener
	serverHost     string
	pendingStreams map[string]*PendingStream
	pendingMu      sync.RWMutex
}

// ClientSession represents an active client connection
type ClientSession struct {
	id         string
	conn       net.Conn
	publicPort int
	listener   net.Listener
	streams    map[string]*ServerStream
	streamsMu  sync.RWMutex
}

// ServerStream represents a proxied connection on the server side
type ServerStream struct {
	uuid         string
	dataConn     net.Conn
	externalConn net.Conn
	closeCh      chan struct{}
	createdAt    time.Time
}

// PendingStream represents an external connection waiting for client to open data connection
type PendingStream struct {
	externalConn net.Conn
	sessionID    string
	createdAt    time.Time
	timeout      *time.Timer
}

// NewServer creates a new Server instance with the specified configuration
func NewServer(listenPort int, secret string) *Server {
	// Use default port if not specified
	if listenPort == 0 {
		listenPort = 7835
	}

	return &Server{
		listenPort:     listenPort,
		secret:         secret,
		clients:        make(map[string]*ClientSession),
		portAllocator:  NewPortAllocator(10000, 60000),
		pendingStreams: make(map[string]*PendingStream),
	}
}

// Run starts the server and begins listening for client connections
func (s *Server) Run() error {
	addr := fmt.Sprintf(":%d", s.listenPort)
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		logging.Error("failed to start server", "port", s.listenPort, "error", err)
		// Check if port is already in use
		if IsPortInUse(err) {
			return NewPortInUseError(s.listenPort, err)
		}
		return fmt.Errorf("failed to listen on %s: %w", addr, err)
	}
	s.listener = listener

	// Extract server host once at startup
	if host, _, err := net.SplitHostPort(listener.Addr().String()); err == nil && host != "" && host != "::" && host != "0.0.0.0" {
		s.serverHost = host
	}

	logging.Info("server listening", "port", s.listenPort)

	for {
		conn, err := listener.Accept()
		if err != nil {
			// If listener was closed, return nil (graceful shutdown)
			if opErr, ok := err.(*net.OpError); ok && opErr.Err.Error() == "use of closed network connection" {
				logging.Info("server stopped")
				return nil
			}
			logging.Error("failed to accept connection", "error", err)
			return fmt.Errorf("failed to accept connection: %w", err)
		}

		logging.Debug("new connection", "remoteAddr", conn.RemoteAddr())

		// Handle client connection in a goroutine
		go s.handleClient(conn)
	}
}

// handleClient processes an incoming client connection
// It detects the connection type based on the first message and routes accordingly
func (s *Server) handleClient(conn net.Conn) {
	// Read first message to determine connection type
	msg, err := protocol.ReadMessage(conn)
	if err != nil {
		logging.Error("failed to read first message", "remoteAddr", conn.RemoteAddr(), "error", err)
		conn.Close()
		return
	}

	// Route based on message type
	switch msg.Type {
	case protocol.MessageTypeHandshake:
		logging.Debug("control connection detected", "remoteAddr", conn.RemoteAddr())
		// Control connection - we manage the lifecycle here
		defer conn.Close()
		s.handleControlConnection(conn, msg)
	case protocol.MessageTypeStreamHandshake:
		logging.Debug("data connection detected", "remoteAddr", conn.RemoteAddr())
		// Data connection - proxyStream goroutine owns the connection lifecycle
		// Do NOT close conn here; it will be closed by proxyStream when done
		s.handleDataConnection(conn, msg)
	default:
		logging.Error("unexpected message type", "type", msg.Type, "remoteAddr", conn.RemoteAddr())
		conn.Close()
	}
}

// handleControlConnection processes a control connection (client handshake)
func (s *Server) handleControlConnection(conn net.Conn, handshakeMsg *protocol.Message) {
	remoteAddr := conn.RemoteAddr().String()

	// Decode handshake payload
	handshake, err := protocol.DecodeHandshake(handshakeMsg.Payload)
	if err != nil {
		logging.Error("failed to decode handshake", "remoteAddr", remoteAddr, "error", err)
		logging.Warn("protocol violation: malformed handshake", "remoteAddr", remoteAddr)
		return
	}

	// Verify protocol version
	if handshake.Version != protocol.ProtocolVersion {
		logging.Error("unsupported protocol version", "version", handshake.Version, "remoteAddr", remoteAddr)
		logging.Warn("protocol violation: unsupported version", "remoteAddr", remoteAddr, "version", handshake.Version)
		s.sendReject(conn)
		return
	}

	// Authenticate
	if !s.authenticate(handshake.Secret) {
		logging.Warn("authentication failed", "remoteAddr", remoteAddr)
		s.sendReject(conn)
		return
	}

	logging.Debug("client authenticated", "remoteAddr", remoteAddr)

	// Allocate public port
	publicPort, err := s.portAllocator.Allocate()
	if err != nil {
		logging.Error("failed to allocate port", "remoteAddr", remoteAddr, "error", err)
		logging.Warn("resource exhausted: no available ports", "remoteAddr", remoteAddr)
		s.sendReject(conn)
		return
	}

	// Send accept message
	if err := s.sendAccept(conn, publicPort); err != nil {
		logging.Error("failed to send accept message", "remoteAddr", remoteAddr, "error", err)
		s.portAllocator.Release(publicPort)
		return
	}

	// Create session
	session := s.createSession(conn)
	session.publicPort = publicPort

	// Create listener on public port
	publicListener, err := s.createPublicListener(publicPort)
	if err != nil {
		logging.Error("failed to create public listener", "port", publicPort, "remoteAddr", remoteAddr, "error", err)
		s.portAllocator.Release(publicPort)
		s.removeSession(session.id)
		return
	}
	session.listener = publicListener

	logging.Info("client connected",
		"sessionID", session.id,
		"publicPort", publicPort,
	)

	// Start accepting connections on public port
	go s.acceptPublicConnections(session)

	// TODO: Start message loop in later tasks
	// For now, keep connection alive by blocking until it closes
	// This will be replaced with the actual message loop in task 6
	buf := make([]byte, 1)
	for {
		_, err := conn.Read(buf)
		if err != nil {
			// Connection closed
			logging.Info("client disconnected", "sessionID", session.id)
			s.removeSession(session.id)
			return
		}
	}
}

// handleDataConnection processes a data connection (stream handshake)
// Note: This function takes ownership of conn - it must close it in all error paths
// or pass ownership to proxyStream goroutine
func (s *Server) handleDataConnection(conn net.Conn, streamHandshakeMsg *protocol.Message) {
	// Decode stream handshake payload
	streamHandshake, err := protocol.DecodeStreamHandshake(streamHandshakeMsg.Payload)
	if err != nil {
		logging.Error("failed to decode stream handshake", "error", err)
		logging.Warn("protocol violation: malformed stream handshake", "remoteAddr", conn.RemoteAddr())
		conn.Close()
		return
	}

	uuid := streamHandshake.UUID

	// Validate UUID format (should be 36 characters)
	if len(uuid) != 36 {
		logging.Warn("protocol violation: invalid UUID format", "uuid", uuid, "remoteAddr", conn.RemoteAddr())
		s.sendStreamReject(conn)
		conn.Close()
		return
	}

	// Look up UUID in pending streams
	s.pendingMu.Lock()
	pending, exists := s.pendingStreams[uuid]
	if !exists {
		s.pendingMu.Unlock()
		logging.Warn("stream UUID not found or expired", "uuid", uuid)
		s.sendStreamReject(conn)
		conn.Close()
		return
	}

	// Remove from pending streams and cancel timeout
	delete(s.pendingStreams, uuid)
	s.pendingMu.Unlock()

	// Cancel timeout
	if pending.timeout != nil {
		pending.timeout.Stop()
	}

	// Send stream accept
	if err := s.sendStreamAccept(conn); err != nil {
		logging.Error("failed to send stream accept", "uuid", uuid, "error", err)
		// Close both connections on error
		conn.Close()
		pending.externalConn.Close()
		return
	}

	// Find the client session
	s.clientsMu.RLock()
	var session *ClientSession
	for _, clientSession := range s.clients {
		if clientSession.id == pending.sessionID {
			session = clientSession
			break
		}
	}
	s.clientsMu.RUnlock()

	if session == nil {
		logging.Error("client session not found", "sessionID", pending.sessionID, "uuid", uuid)
		conn.Close()
		pending.externalConn.Close()
		return
	}

	// Create server stream
	stream := &ServerStream{
		uuid:         uuid,
		dataConn:     conn,
		externalConn: pending.externalConn,
		closeCh:      make(chan struct{}),
		createdAt:    time.Now(),
	}

	// Register stream in session
	session.streamsMu.Lock()
	session.streams[uuid] = stream
	session.streamsMu.Unlock()

	logging.Info("stream created",
		"uuid", uuid,
		"sessionID", session.id,
	)

	// Start bidirectional raw TCP proxy
	logging.Debug("starting proxy", "uuid", uuid)
	go s.proxyStream(stream, session)
}

// authenticate checks if the provided secret matches the server's secret
func (s *Server) authenticate(clientSecret string) bool {
	// If server has no secret configured, allow all connections (open mode)
	if s.secret == "" {
		return true
	}

	// Use constant-time comparison to prevent timing attacks
	return subtle.ConstantTimeCompare([]byte(clientSecret), []byte(s.secret)) == 1
}

// sendAccept sends an accept message to the client
func (s *Server) sendAccept(conn net.Conn, publicPort int) error {
	acceptPayload := &protocol.AcceptPayload{
		PublicPort: uint16(publicPort),
		ServerHost: s.serverHost,
	}

	payloadBytes, err := protocol.EncodeAccept(acceptPayload)
	if err != nil {
		return fmt.Errorf("failed to encode accept payload: %w", err)
	}

	msg := &protocol.Message{
		Type:    protocol.MessageTypeAccept,
		Payload: payloadBytes,
	}

	if err := protocol.WriteMessage(conn, msg); err != nil {
		return fmt.Errorf("failed to write accept message: %w", err)
	}

	return nil
}

// sendReject sends a reject message to the client
func (s *Server) sendReject(conn net.Conn) error {
	msg := &protocol.Message{
		Type:    protocol.MessageTypeReject,
		Payload: nil,
	}

	return protocol.WriteMessage(conn, msg)
}

// sendStreamAccept sends a stream accept message to the client
func (s *Server) sendStreamAccept(conn net.Conn) error {
	msg := &protocol.Message{
		Type:    protocol.MessageTypeStreamAccept,
		Payload: nil,
	}

	return protocol.WriteMessage(conn, msg)
}

// sendStreamReject sends a stream reject message to the client
func (s *Server) sendStreamReject(conn net.Conn) error {
	msg := &protocol.Message{
		Type:    protocol.MessageTypeStreamReject,
		Payload: nil,
	}

	return protocol.WriteMessage(conn, msg)
}

// createSession creates a new client session
func (s *Server) createSession(conn net.Conn) *ClientSession {
	s.clientsMu.Lock()
	defer s.clientsMu.Unlock()

	// Generate unique session ID using remote address
	sessionID := conn.RemoteAddr().String()

	session := &ClientSession{
		id:      sessionID,
		conn:    conn,
		streams: make(map[string]*ServerStream),
	}

	s.clients[sessionID] = session
	return session
}

// removeSession removes a client session and cleans up resources
func (s *Server) removeSession(sessionID string) {
	s.clientsMu.Lock()
	session, exists := s.clients[sessionID]
	if !exists {
		s.clientsMu.Unlock()
		return
	}
	delete(s.clients, sessionID)
	s.clientsMu.Unlock()

	// Clean up session resources
	session.cleanup(s.portAllocator)
}

// cleanup cleans up all resources associated with a client session
func (cs *ClientSession) cleanup(portAllocator *PortAllocator) {
	// Close all streams
	cs.streamsMu.Lock()
	for _, stream := range cs.streams {
		stream.close()
	}
	cs.streams = make(map[string]*ServerStream)
	cs.streamsMu.Unlock()

	// Close public port listener
	if cs.listener != nil {
		cs.listener.Close()
	}

	// Release allocated port
	if cs.publicPort != 0 {
		portAllocator.Release(cs.publicPort)
	}

	// Close control connection
	if cs.conn != nil {
		cs.conn.Close()
	}
}

// close closes a stream and cleans up resources
func (s *ServerStream) close() {
	if s.dataConn != nil {
		s.dataConn.Close()
	}
	if s.externalConn != nil {
		s.externalConn.Close()
	}
	// Close channel only once using select to avoid panic
	select {
	case <-s.closeCh:
		// Already closed
	default:
		close(s.closeCh)
	}
}

// createPublicListener creates a TCP listener on the specified public port
func (s *Server) createPublicListener(port int) (net.Listener, error) {
	addr := fmt.Sprintf(":%d", port)
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("failed to listen on port %d: %w", port, err)
	}
	return listener, nil
}

// acceptPublicConnections accepts incoming connections on the public port
// and notifies the client about each new connection
func (s *Server) acceptPublicConnections(session *ClientSession) {
	defer func() {
		// Clean up session when this goroutine exits
		s.removeSession(session.id)
	}()

	for {
		conn, err := session.listener.Accept()
		if err != nil {
			// If listener was closed, exit gracefully
			if opErr, ok := err.(*net.OpError); ok && opErr.Err.Error() == "use of closed network connection" {
				return
			}
			logging.Error("failed to accept connection on public port",
				"port", session.publicPort,
				"error", err,
			)
			return
		}

		// Generate UUID for this connection
		streamUUID := uuid.New().String()

		logging.Debug("new external connection",
			"port", session.publicPort,
			"uuid", streamUUID,
			"remoteAddr", conn.RemoteAddr(),
		)

		// Create pending stream with timeout
		pending := &PendingStream{
			externalConn: conn,
			sessionID:    session.id,
			createdAt:    time.Now(),
		}

		// Set 30-second timeout
		pending.timeout = time.AfterFunc(30*time.Second, func() {
			s.cleanupPendingStream(streamUUID)
		})

		// Store pending stream
		s.pendingMu.Lock()
		s.pendingStreams[streamUUID] = pending
		s.pendingMu.Unlock()

		// Send MessageTypeConnect to client
		if err := s.sendConnect(session, streamUUID); err != nil {
			logging.Error("failed to send connect message",
				"uuid", streamUUID,
				"error", err,
			)
			s.cleanupPendingStream(streamUUID)
			continue
		}

		logging.Debug("connect message sent", "uuid", streamUUID)
	}
}

// sendConnect sends a MessageTypeConnect to the client with UUID
func (s *Server) sendConnect(session *ClientSession, uuid string) error {
	connectPayload := &protocol.ConnectPayload{
		UUID: uuid,
	}

	payloadBytes, err := protocol.EncodeConnect(connectPayload)
	if err != nil {
		return fmt.Errorf("failed to encode connect payload: %w", err)
	}

	msg := &protocol.Message{
		Type:    protocol.MessageTypeConnect,
		Payload: payloadBytes,
	}

	if err := protocol.WriteMessage(session.conn, msg); err != nil {
		return fmt.Errorf("failed to write connect message: %w", err)
	}

	return nil
}

// cleanupPendingStream removes and cleans up a pending stream
func (s *Server) cleanupPendingStream(uuid string) {
	s.pendingMu.Lock()
	pending, exists := s.pendingStreams[uuid]
	if !exists {
		s.pendingMu.Unlock()
		return
	}
	delete(s.pendingStreams, uuid)
	s.pendingMu.Unlock()

	// Cancel timeout if it exists
	if pending.timeout != nil {
		pending.timeout.Stop()
	}

	// Close external connection
	if pending.externalConn != nil {
		pending.externalConn.Close()
	}

	logging.Debug("pending stream cleaned up", "uuid", uuid)
}

// Close shuts down the server and cleans up resources
func (s *Server) Close() error {
	logging.Debug("shutting down server")

	// Close main listener first
	if s.listener != nil {
		s.listener.Close()
	}

	// Clean up all pending streams
	s.pendingMu.Lock()
	for uuid, pending := range s.pendingStreams {
		if pending.timeout != nil {
			pending.timeout.Stop()
		}
		if pending.externalConn != nil {
			pending.externalConn.Close()
		}
		delete(s.pendingStreams, uuid)
	}
	s.pendingMu.Unlock()

	// Clean up all client sessions
	s.clientsMu.Lock()
	sessionIDs := make([]string, 0, len(s.clients))
	for id := range s.clients {
		sessionIDs = append(sessionIDs, id)
	}
	s.clientsMu.Unlock()

	for _, id := range sessionIDs {
		s.removeSession(id)
	}

	logging.Info("server shutdown complete")
	return nil
}

// Addr returns the server's listen address as a string.
// Returns empty string if server is not running.
func (s *Server) Addr() string {
	if s.listener == nil {
		return ""
	}
	return s.listener.Addr().String()
}

// proxyStream handles bidirectional raw TCP proxying for a stream
func (s *Server) proxyStream(stream *ServerStream, session *ClientSession) {
	logging.Debug("proxy started", "uuid", stream.uuid)
	defer func() {
		// Clean up stream when proxy exits
		s.cleanupStream(stream, session)
	}()

	// Use a channel to signal when one direction completes
	done := make(chan struct{})

	// Goroutine 1: external → client (externalConn → dataConn)
	go func() {
		logging.Debug("starting external→client copy", "uuid", stream.uuid)
		_, err := io.Copy(stream.dataConn, stream.externalConn)
		if err != nil {
			logging.Debug("external→client copy error", "uuid", stream.uuid, "error", err)
		} else {
			logging.Debug("external→client copy completed", "uuid", stream.uuid)
		}

		// Signal completion and close connections
		select {
		case <-done:
			// Other goroutine already signaled
		default:
			close(done)
		}
	}()

	// Goroutine 2: client → external (dataConn → externalConn)
	go func() {
		logging.Debug("starting client→external copy", "uuid", stream.uuid)
		_, err := io.Copy(stream.externalConn, stream.dataConn)
		if err != nil {
			logging.Debug("client→external copy error", "uuid", stream.uuid, "error", err)
		} else {
			logging.Debug("client→external copy completed", "uuid", stream.uuid)
		}

		// Signal completion and close connections
		select {
		case <-done:
			// Other goroutine already signaled
		default:
			close(done)
		}
	}()

	// Wait for one direction to complete, then close connections to stop the other
	<-done
	stream.dataConn.Close()
	stream.externalConn.Close()

	logging.Debug("bidirectional proxy completed", "uuid", stream.uuid)
}

// cleanupStream removes a stream from the session and closes resources
func (s *Server) cleanupStream(stream *ServerStream, session *ClientSession) {
	// Remove stream from session
	session.streamsMu.Lock()
	delete(session.streams, stream.uuid)
	session.streamsMu.Unlock()

	// Close stream resources
	stream.close()

	logging.Info("stream closed", "uuid", stream.uuid)
}
