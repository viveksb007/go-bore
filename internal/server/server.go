package server

import (
	"fmt"
	"io"
	"net"
	"sync"
	"time"

	"github.com/google/uuid"
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
		return fmt.Errorf("failed to listen on %s: %w", addr, err)
	}
	s.listener = listener

	fmt.Printf("Server listening on port %d\n", s.listenPort)

	for {
		conn, err := listener.Accept()
		if err != nil {
			// If listener was closed, return nil (graceful shutdown)
			if opErr, ok := err.(*net.OpError); ok && opErr.Err.Error() == "use of closed network connection" {
				return nil
			}
			return fmt.Errorf("failed to accept connection: %w", err)
		}

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
		fmt.Printf("Failed to read first message: %v\n", err)
		conn.Close()
		return
	}

	// Route based on message type
	switch msg.Type {
	case protocol.MessageTypeHandshake:
		// Control connection - we manage the lifecycle here
		defer conn.Close()
		s.handleControlConnection(conn, msg)
	case protocol.MessageTypeStreamHandshake:
		// Data connection - proxyStream goroutine owns the connection lifecycle
		// Do NOT close conn here; it will be closed by proxyStream when done
		s.handleDataConnection(conn, msg)
	default:
		fmt.Printf("Unexpected message type %d from %s\n", msg.Type, conn.RemoteAddr())
		conn.Close()
	}
}

// handleControlConnection processes a control connection (client handshake)
func (s *Server) handleControlConnection(conn net.Conn, handshakeMsg *protocol.Message) {
	// Decode handshake payload
	handshake, err := protocol.DecodeHandshake(handshakeMsg.Payload)
	if err != nil {
		fmt.Printf("Failed to decode handshake: %v\n", err)
		return
	}

	// Verify protocol version
	if handshake.Version != protocol.ProtocolVersion {
		fmt.Printf("Unsupported protocol version: %d\n", handshake.Version)
		s.sendReject(conn)
		return
	}

	// Authenticate
	if !s.authenticate(handshake.Secret) {
		fmt.Printf("Authentication failed for client %s\n", conn.RemoteAddr())
		s.sendReject(conn)
		return
	}

	// Allocate public port
	publicPort, err := s.portAllocator.Allocate()
	if err != nil {
		fmt.Printf("Failed to allocate port: %v\n", err)
		s.sendReject(conn)
		return
	}

	// Send accept message
	if err := s.sendAccept(conn, publicPort); err != nil {
		fmt.Printf("Failed to send accept message: %v\n", err)
		s.portAllocator.Release(publicPort)
		return
	}

	// Create session
	session := s.createSession(conn)
	session.publicPort = publicPort

	// Create listener on public port
	publicListener, err := s.createPublicListener(publicPort)
	if err != nil {
		fmt.Printf("Failed to create public listener on port %d: %v\n", publicPort, err)
		s.portAllocator.Release(publicPort)
		s.removeSession(session.id)
		return
	}
	session.listener = publicListener

	fmt.Printf("Client %s authenticated, allocated port %d\n", session.id, publicPort)

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
		fmt.Printf("Failed to decode stream handshake: %v\n", err)
		conn.Close()
		return
	}

	uuid := streamHandshake.UUID

	// Look up UUID in pending streams
	s.pendingMu.Lock()
	pending, exists := s.pendingStreams[uuid]
	if !exists {
		s.pendingMu.Unlock()
		fmt.Printf("Stream UUID %s not found or expired\n", uuid)
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
		fmt.Printf("Failed to send stream accept for UUID %s: %v\n", uuid, err)
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
		fmt.Printf("Client session %s not found for UUID %s\n", pending.sessionID, uuid)
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

	fmt.Printf("Stream %s established for session %s\n", uuid, session.id)

	// Start bidirectional raw TCP proxy
	fmt.Printf("Starting proxy for stream %s\n", uuid)
	go s.proxyStream(stream, session)
}

// authenticate checks if the provided secret matches the server's secret
func (s *Server) authenticate(clientSecret string) bool {
	// If server has no secret configured, allow all connections (open mode)
	if s.secret == "" {
		return true
	}

	// Otherwise, require exact match
	return clientSecret == s.secret
}

// sendAccept sends an accept message to the client
func (s *Server) sendAccept(conn net.Conn, publicPort int) error {
	acceptPayload := &protocol.AcceptPayload{
		PublicPort: uint16(publicPort),
		ServerHost: "localhost", // TODO: Get actual server hostname
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
			fmt.Printf("Failed to accept connection on public port %d: %v\n", session.publicPort, err)
			return
		}

		// Generate UUID for this connection
		streamUUID := uuid.New().String()

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
			fmt.Printf("Failed to send connect message for UUID %s: %v\n", streamUUID, err)
			s.cleanupPendingStream(streamUUID)
			continue
		}

		fmt.Printf("New external connection on port %d, assigned UUID %s\n", session.publicPort, streamUUID)

		// TODO: Client will open data connection with this UUID in task 6
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

	fmt.Printf("Cleaned up pending stream %s (timeout or error)\n", uuid)
}

// Close shuts down the server and cleans up resources
func (s *Server) Close() error {
	// Close main listener first
	if s.listener != nil {
		s.listener.Close()
	}

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

	return nil
}

// proxyStream handles bidirectional raw TCP proxying for a stream
func (s *Server) proxyStream(stream *ServerStream, session *ClientSession) {
	fmt.Printf("Proxy started for stream %s\n", stream.uuid)
	defer func() {
		// Clean up stream when proxy exits
		s.cleanupStream(stream, session)
	}()

	// Use a channel to signal when one direction completes
	done := make(chan struct{})

	// Goroutine 1: external → client (externalConn → dataConn)
	go func() {
		fmt.Printf("Stream %s: starting external→client copy\n", stream.uuid)
		_, err := io.Copy(stream.dataConn, stream.externalConn)
		if err != nil {
			fmt.Printf("Stream %s: external→client copy error: %v\n", stream.uuid, err)
		} else {
			fmt.Printf("Stream %s: external→client copy completed\n", stream.uuid)
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
		fmt.Printf("Stream %s: starting client→external copy\n", stream.uuid)
		_, err := io.Copy(stream.externalConn, stream.dataConn)
		if err != nil {
			fmt.Printf("Stream %s: client→external copy error: %v\n", stream.uuid, err)
		} else {
			fmt.Printf("Stream %s: client→external copy completed\n", stream.uuid)
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

	fmt.Printf("Stream %s: bidirectional proxy completed\n", stream.uuid)
}

// cleanupStream removes a stream from the session and closes resources
func (s *Server) cleanupStream(stream *ServerStream, session *ClientSession) {
	// Remove stream from session
	session.streamsMu.Lock()
	delete(session.streams, stream.uuid)
	session.streamsMu.Unlock()

	// Close stream resources
	stream.close()

	fmt.Printf("Stream %s cleaned up\n", stream.uuid)
}
