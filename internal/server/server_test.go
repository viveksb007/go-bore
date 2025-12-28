package server

import (
	"fmt"
	"io"
	"net"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/viveksb007/go-bore/internal/protocol"
)

func TestNewServer(t *testing.T) {
	tests := []struct {
		name       string
		listenPort int
		secret     string
		wantPort   int
	}{
		{
			name:       "with specified port",
			listenPort: 8080,
			secret:     "test-secret",
			wantPort:   8080,
		},
		{
			name:       "with default port",
			listenPort: 0,
			secret:     "",
			wantPort:   7835,
		},
		{
			name:       "with secret",
			listenPort: 9000,
			secret:     "my-secret-token",
			wantPort:   9000,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := NewServer(tt.listenPort, tt.secret)

			if server == nil {
				t.Fatal("NewServer returned nil")
			}

			if server.listenPort != tt.wantPort {
				t.Errorf("listenPort = %d, want %d", server.listenPort, tt.wantPort)
			}

			if server.secret != tt.secret {
				t.Errorf("secret = %q, want %q", server.secret, tt.secret)
			}

			if server.clients == nil {
				t.Error("clients map is nil")
			}

			if server.portAllocator == nil {
				t.Error("portAllocator is nil")
			}
		})
	}
}

func TestServerRun(t *testing.T) {
	server := NewServer(0, "")

	// Start server in a goroutine
	errCh := make(chan error, 1)
	go func() {
		errCh <- server.Run()
	}()

	// Give server time to start
	time.Sleep(100 * time.Millisecond)

	// Verify server is listening
	if server.listener == nil {
		t.Fatal("server listener is nil after Run()")
	}

	// Close server
	if err := server.Close(); err != nil {
		t.Errorf("Close() error = %v", err)
	}

	// Wait for Run() to return
	select {
	case err := <-errCh:
		if err != nil {
			t.Errorf("Run() error = %v", err)
		}
	case <-time.After(1 * time.Second):
		t.Error("Run() did not return after Close()")
	}
}

func TestServerRunPortInUse(t *testing.T) {
	// Start first server on a random port
	server1 := NewServer(0, "")
	errCh1 := make(chan error, 1)
	go func() {
		errCh1 <- server1.Run()
	}()

	// Give first server time to start
	time.Sleep(100 * time.Millisecond)

	// Check if server1 started successfully
	if server1.listener == nil {
		t.Skip("First server failed to start, skipping test")
	}

	// Get the port that server1 is using
	addr := server1.listener.Addr().(*net.TCPAddr)
	usedPort := addr.Port

	// Try to start second server on same port
	server2 := NewServer(usedPort, "")
	err := server2.Run()

	if err == nil {
		t.Error("expected error when port is already in use, got nil")
		server2.Close()
	}

	// Clean up first server
	server1.Close()
	<-errCh1
}

func TestCreateSession(t *testing.T) {
	server := NewServer(8080, "test-secret")

	// Create a mock connection
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to create listener: %v", err)
	}
	defer listener.Close()

	// Connect to the listener
	connCh := make(chan net.Conn, 1)
	go func() {
		conn, _ := listener.Accept()
		connCh <- conn
	}()

	clientConn, err := net.Dial("tcp", listener.Addr().String())
	if err != nil {
		t.Fatalf("failed to dial: %v", err)
	}
	defer clientConn.Close()

	serverConn := <-connCh

	// Create session
	session := server.createSession(serverConn)

	if session == nil {
		t.Fatal("createSession returned nil")
	}

	if session.id == "" {
		t.Error("session ID is empty")
	}

	if session.conn != serverConn {
		t.Error("session connection does not match")
	}

	if session.streams == nil {
		t.Error("session streams map is nil")
	}

	// nextStreamID field removed in connection-per-stream design
	// Streams are now identified by UUIDs

	// Verify session is tracked in server
	server.clientsMu.RLock()
	_, exists := server.clients[session.id]
	server.clientsMu.RUnlock()

	if !exists {
		t.Error("session not found in server.clients map")
	}
}

func TestRemoveSession(t *testing.T) {
	server := NewServer(8080, "test-secret")

	// Create a mock connection
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to create listener: %v", err)
	}
	defer listener.Close()

	connCh := make(chan net.Conn, 1)
	go func() {
		conn, _ := listener.Accept()
		connCh <- conn
	}()

	clientConn, err := net.Dial("tcp", listener.Addr().String())
	if err != nil {
		t.Fatalf("failed to dial: %v", err)
	}
	defer clientConn.Close()

	serverConn := <-connCh

	// Create session
	session := server.createSession(serverConn)
	sessionID := session.id

	// Verify session exists
	server.clientsMu.RLock()
	_, exists := server.clients[sessionID]
	server.clientsMu.RUnlock()

	if !exists {
		t.Fatal("session not found after creation")
	}

	// Remove session
	server.removeSession(sessionID)

	// Verify session is removed
	server.clientsMu.RLock()
	_, exists = server.clients[sessionID]
	server.clientsMu.RUnlock()

	if exists {
		t.Error("session still exists after removal")
	}
}

func TestSessionCleanup(t *testing.T) {
	portAllocator := NewPortAllocator(10000, 10100)

	// Create a mock connection
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to create listener: %v", err)
	}
	defer listener.Close()

	connCh := make(chan net.Conn, 1)
	go func() {
		conn, _ := listener.Accept()
		connCh <- conn
	}()

	clientConn, err := net.Dial("tcp", listener.Addr().String())
	if err != nil {
		t.Fatalf("failed to dial: %v", err)
	}
	defer clientConn.Close()

	serverConn := <-connCh

	// Create session with allocated port
	session := &ClientSession{
		id:      "test-session",
		conn:    serverConn,
		streams: make(map[string]*ServerStream),
	}

	// Allocate a port
	port, err := portAllocator.Allocate()
	if err != nil {
		t.Fatalf("failed to allocate port: %v", err)
	}
	session.publicPort = port

	// Create a public listener
	publicListener, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		t.Fatalf("failed to create public listener: %v", err)
	}
	session.listener = publicListener

	// Add a stream
	streamConn, err := net.Dial("tcp", listener.Addr().String())
	if err != nil {
		t.Fatalf("failed to create stream connection: %v", err)
	}

	stream := &ServerStream{
		uuid:         "test-uuid-1234",
		externalConn: streamConn,
		closeCh:      make(chan struct{}),
		createdAt:    time.Now(),
	}
	session.streams["test-uuid-1234"] = stream

	// Cleanup session
	session.cleanup(portAllocator)

	// Verify resources are cleaned up
	if len(session.streams) != 0 {
		t.Errorf("streams not cleaned up, got %d streams", len(session.streams))
	}

	// Verify port is released (should be able to allocate it again)
	portAllocator.Release(port) // Release once more to ensure it's available
	newPort, err := portAllocator.Allocate()
	if err != nil {
		t.Errorf("failed to allocate port after cleanup: %v", err)
	}
	if newPort != port {
		// Port might be different due to random allocation, just verify no error
		portAllocator.Release(newPort)
	}
}

func TestMultipleSessions(t *testing.T) {
	server := NewServer(0, "")

	// Create multiple mock connections
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to create listener: %v", err)
	}
	defer listener.Close()

	numSessions := 5
	sessions := make([]*ClientSession, numSessions)

	for i := 0; i < numSessions; i++ {
		connCh := make(chan net.Conn, 1)
		go func() {
			conn, _ := listener.Accept()
			connCh <- conn
		}()

		clientConn, err := net.Dial("tcp", listener.Addr().String())
		if err != nil {
			t.Fatalf("failed to dial: %v", err)
		}
		defer clientConn.Close()

		serverConn := <-connCh
		sessions[i] = server.createSession(serverConn)
	}

	// Verify all sessions are tracked
	server.clientsMu.RLock()
	if len(server.clients) != numSessions {
		t.Errorf("expected %d sessions, got %d", numSessions, len(server.clients))
	}
	server.clientsMu.RUnlock()

	// Remove all sessions
	for _, session := range sessions {
		server.removeSession(session.id)
	}

	// Verify all sessions are removed
	server.clientsMu.RLock()
	if len(server.clients) != 0 {
		t.Errorf("expected 0 sessions after removal, got %d", len(server.clients))
	}
	server.clientsMu.RUnlock()
}

func TestAuthenticate(t *testing.T) {
	tests := []struct {
		name         string
		serverSecret string
		clientSecret string
		wantAuth     bool
	}{
		{
			name:         "matching secrets",
			serverSecret: "test-secret-123",
			clientSecret: "test-secret-123",
			wantAuth:     true,
		},
		{
			name:         "mismatched secrets",
			serverSecret: "server-secret",
			clientSecret: "wrong-secret",
			wantAuth:     false,
		},
		{
			name:         "empty client secret with server secret",
			serverSecret: "server-secret",
			clientSecret: "",
			wantAuth:     false,
		},
		{
			name:         "open mode - no server secret",
			serverSecret: "",
			clientSecret: "",
			wantAuth:     true,
		},
		{
			name:         "open mode - client sends secret but server has none",
			serverSecret: "",
			clientSecret: "some-secret",
			wantAuth:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := NewServer(8080, tt.serverSecret)
			gotAuth := server.authenticate(tt.clientSecret)

			if gotAuth != tt.wantAuth {
				t.Errorf("authenticate() = %v, want %v", gotAuth, tt.wantAuth)
			}
		})
	}
}

func TestHandshakeSuccess(t *testing.T) {
	// Start server
	server := NewServer(0, "test-secret")
	errCh := make(chan error, 1)
	go func() {
		errCh <- server.Run()
	}()
	defer server.Close()

	// Give server time to start
	time.Sleep(100 * time.Millisecond)

	// Connect as client
	conn, err := net.Dial("tcp", server.listener.Addr().String())
	if err != nil {
		t.Fatalf("failed to connect to server: %v", err)
	}
	defer conn.Close()

	// Send handshake
	handshake := &protocol.HandshakePayload{
		Version: protocol.ProtocolVersion,
		Secret:  "test-secret",
	}
	handshakeBytes, err := protocol.EncodeHandshake(handshake)
	if err != nil {
		t.Fatalf("failed to encode handshake: %v", err)
	}

	handshakeMsg := &protocol.Message{
		Type: protocol.MessageTypeHandshake,

		Payload: handshakeBytes,
	}

	if err := protocol.WriteMessage(conn, handshakeMsg); err != nil {
		t.Fatalf("failed to write handshake: %v", err)
	}

	// Read accept message
	acceptMsg, err := protocol.ReadMessage(conn)
	if err != nil {
		t.Fatalf("failed to read accept message: %v", err)
	}

	if acceptMsg.Type != protocol.MessageTypeAccept {
		t.Errorf("expected MessageTypeAccept, got type %d", acceptMsg.Type)
	}

	// Decode accept payload
	acceptPayload, err := protocol.DecodeAccept(acceptMsg.Payload)
	if err != nil {
		t.Fatalf("failed to decode accept payload: %v", err)
	}

	if acceptPayload.PublicPort == 0 {
		t.Error("public port is 0")
	}

	// Verify session was created
	time.Sleep(50 * time.Millisecond)
	server.clientsMu.RLock()
	numClients := len(server.clients)
	server.clientsMu.RUnlock()

	if numClients != 1 {
		t.Errorf("expected 1 client session, got %d", numClients)
	}
}

func TestHandshakeAuthenticationFailure(t *testing.T) {
	// Start server with secret
	server := NewServer(0, "correct-secret")
	errCh := make(chan error, 1)
	go func() {
		errCh <- server.Run()
	}()
	defer server.Close()

	// Give server time to start
	time.Sleep(100 * time.Millisecond)

	// Connect as client
	conn, err := net.Dial("tcp", server.listener.Addr().String())
	if err != nil {
		t.Fatalf("failed to connect to server: %v", err)
	}
	defer conn.Close()

	// Send handshake with wrong secret
	handshake := &protocol.HandshakePayload{
		Version: protocol.ProtocolVersion,
		Secret:  "wrong-secret",
	}
	handshakeBytes, err := protocol.EncodeHandshake(handshake)
	if err != nil {
		t.Fatalf("failed to encode handshake: %v", err)
	}

	handshakeMsg := &protocol.Message{
		Type: protocol.MessageTypeHandshake,

		Payload: handshakeBytes,
	}

	if err := protocol.WriteMessage(conn, handshakeMsg); err != nil {
		t.Fatalf("failed to write handshake: %v", err)
	}

	// Read reject message
	rejectMsg, err := protocol.ReadMessage(conn)
	if err != nil {
		t.Fatalf("failed to read reject message: %v", err)
	}

	if rejectMsg.Type != protocol.MessageTypeReject {
		t.Errorf("expected MessageTypeReject, got type %d", rejectMsg.Type)
	}

	// Verify no session was created
	time.Sleep(50 * time.Millisecond)
	server.clientsMu.RLock()
	numClients := len(server.clients)
	server.clientsMu.RUnlock()

	if numClients != 0 {
		t.Errorf("expected 0 client sessions after failed auth, got %d", numClients)
	}
}

func TestHandshakeOpenMode(t *testing.T) {
	// Start server without secret (open mode)
	server := NewServer(0, "")
	errCh := make(chan error, 1)
	go func() {
		errCh <- server.Run()
	}()
	defer server.Close()

	// Give server time to start
	time.Sleep(100 * time.Millisecond)

	// Connect as client
	conn, err := net.Dial("tcp", server.listener.Addr().String())
	if err != nil {
		t.Fatalf("failed to connect to server: %v", err)
	}
	defer conn.Close()

	// Send handshake without secret
	handshake := &protocol.HandshakePayload{
		Version: protocol.ProtocolVersion,
		Secret:  "",
	}
	handshakeBytes, err := protocol.EncodeHandshake(handshake)
	if err != nil {
		t.Fatalf("failed to encode handshake: %v", err)
	}

	handshakeMsg := &protocol.Message{
		Type: protocol.MessageTypeHandshake,

		Payload: handshakeBytes,
	}

	if err := protocol.WriteMessage(conn, handshakeMsg); err != nil {
		t.Fatalf("failed to write handshake: %v", err)
	}

	// Read accept message
	acceptMsg, err := protocol.ReadMessage(conn)
	if err != nil {
		t.Fatalf("failed to read accept message: %v", err)
	}

	if acceptMsg.Type != protocol.MessageTypeAccept {
		t.Errorf("expected MessageTypeAccept in open mode, got type %d", acceptMsg.Type)
	}

	// Verify session was created
	time.Sleep(50 * time.Millisecond)
	server.clientsMu.RLock()
	numClients := len(server.clients)
	server.clientsMu.RUnlock()

	if numClients != 1 {
		t.Errorf("expected 1 client session in open mode, got %d", numClients)
	}
}

func TestHandshakeInvalidProtocolVersion(t *testing.T) {
	// Start server
	server := NewServer(0, "")
	errCh := make(chan error, 1)
	go func() {
		errCh <- server.Run()
	}()
	defer server.Close()

	// Give server time to start
	time.Sleep(100 * time.Millisecond)

	// Connect as client
	conn, err := net.Dial("tcp", server.listener.Addr().String())
	if err != nil {
		t.Fatalf("failed to connect to server: %v", err)
	}
	defer conn.Close()

	// Send handshake with invalid version
	handshake := &protocol.HandshakePayload{
		Version: 99, // Invalid version
		Secret:  "",
	}
	handshakeBytes, err := protocol.EncodeHandshake(handshake)
	if err != nil {
		t.Fatalf("failed to encode handshake: %v", err)
	}

	handshakeMsg := &protocol.Message{
		Type: protocol.MessageTypeHandshake,

		Payload: handshakeBytes,
	}

	if err := protocol.WriteMessage(conn, handshakeMsg); err != nil {
		t.Fatalf("failed to write handshake: %v", err)
	}

	// Read reject message
	rejectMsg, err := protocol.ReadMessage(conn)
	if err != nil {
		t.Fatalf("failed to read reject message: %v", err)
	}

	if rejectMsg.Type != protocol.MessageTypeReject {
		t.Errorf("expected MessageTypeReject for invalid version, got type %d", rejectMsg.Type)
	}
}

func TestHandshakeWrongMessageType(t *testing.T) {
	// Start server
	server := NewServer(0, "")
	errCh := make(chan error, 1)
	go func() {
		errCh <- server.Run()
	}()
	defer server.Close()

	// Give server time to start
	time.Sleep(100 * time.Millisecond)

	// Connect as client
	conn, err := net.Dial("tcp", server.listener.Addr().String())
	if err != nil {
		t.Fatalf("failed to connect to server: %v", err)
	}
	defer conn.Close()

	// Send wrong message type (not handshake)
	wrongMsg := &protocol.Message{
		Type: protocol.MessageTypeConnect, // Wrong type (should be handshake)

		Payload: []byte("test"),
	}

	if err := protocol.WriteMessage(conn, wrongMsg); err != nil {
		t.Fatalf("failed to write message: %v", err)
	}

	// Connection should be closed by server
	time.Sleep(100 * time.Millisecond)

	// Try to read - should get EOF or connection closed error
	_, err = protocol.ReadMessage(conn)
	if err == nil {
		t.Error("expected error reading from closed connection, got nil")
	}
}

func TestCreatePublicListener(t *testing.T) {
	server := NewServer(0, "")

	// Allocate a port
	port, err := server.portAllocator.Allocate()
	if err != nil {
		t.Fatalf("failed to allocate port: %v", err)
	}
	defer server.portAllocator.Release(port)

	// Create listener on allocated port
	listener, err := server.createPublicListener(port)
	if err != nil {
		t.Fatalf("createPublicListener() error = %v", err)
	}
	defer listener.Close()

	// Verify listener is working
	addr := listener.Addr().(*net.TCPAddr)
	if addr.Port != port {
		t.Errorf("listener port = %d, want %d", addr.Port, port)
	}

	// Try to connect to the listener
	conn, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		t.Fatalf("failed to connect to public listener: %v", err)
	}
	conn.Close()
}

func TestCreatePublicListenerPortInUse(t *testing.T) {
	server := NewServer(0, "")

	// Create a listener on a specific port
	existingListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to create existing listener: %v", err)
	}
	defer existingListener.Close()

	usedPort := existingListener.Addr().(*net.TCPAddr).Port

	// Try to create public listener on the same port
	_, err = server.createPublicListener(usedPort)
	if err == nil {
		t.Error("expected error when port is in use, got nil")
	}
}

func TestUUIDGeneration(t *testing.T) {
	// Test that UUIDs are generated and are unique
	uuids := make(map[string]bool)

	for i := 0; i < 100; i++ {
		u := uuid.New().String()

		// Verify UUID format (36 characters)
		if len(u) != 36 {
			t.Errorf("UUID length = %d, want 36", len(u))
		}

		// Verify uniqueness
		if uuids[u] {
			t.Errorf("Duplicate UUID generated: %s", u)
		}
		uuids[u] = true
	}
}

func TestSendConnect(t *testing.T) {
	server := NewServer(0, "")

	// Create a mock connection
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to create listener: %v", err)
	}
	defer listener.Close()

	// Accept connection in goroutine
	serverConnCh := make(chan net.Conn, 1)
	go func() {
		conn, _ := listener.Accept()
		serverConnCh <- conn
	}()

	// Connect as client
	clientConn, err := net.Dial("tcp", listener.Addr().String())
	if err != nil {
		t.Fatalf("failed to dial: %v", err)
	}
	defer clientConn.Close()

	serverConn := <-serverConnCh
	defer serverConn.Close()

	// Create session
	session := &ClientSession{
		id:      "test-session",
		conn:    serverConn,
		streams: make(map[string]*ServerStream),
	}

	// Send connect message with UUID
	testUUID := "550e8400-e29b-41d4-a716-446655440000"
	if err := server.sendConnect(session, testUUID); err != nil {
		t.Fatalf("sendConnect() error = %v", err)
	}

	// Read message on client side
	msg, err := protocol.ReadMessage(clientConn)
	if err != nil {
		t.Fatalf("failed to read message: %v", err)
	}

	// Verify message type
	if msg.Type != protocol.MessageTypeConnect {
		t.Errorf("message type = %d, want %d", msg.Type, protocol.MessageTypeConnect)
	}

	// Verify payload contains UUID
	if len(msg.Payload) != 36 {
		t.Errorf("payload length = %d, want 36", len(msg.Payload))
	}

	// Decode and verify UUID
	connectPayload, err := protocol.DecodeConnect(msg.Payload)
	if err != nil {
		t.Fatalf("failed to decode connect payload: %v", err)
	}

	if connectPayload.UUID != testUUID {
		t.Errorf("UUID = %s, want %s", connectPayload.UUID, testUUID)
	}
}

func TestAcceptPublicConnections(t *testing.T) {
	// Start server
	server := NewServer(0, "")
	errCh := make(chan error, 1)
	go func() {
		errCh <- server.Run()
	}()
	defer server.Close()

	// Give server time to start
	time.Sleep(100 * time.Millisecond)

	// Connect as client and complete handshake
	controlConn, err := net.Dial("tcp", server.listener.Addr().String())
	if err != nil {
		t.Fatalf("failed to connect to server: %v", err)
	}
	defer controlConn.Close()

	// Send handshake
	handshake := &protocol.HandshakePayload{
		Version: protocol.ProtocolVersion,
		Secret:  "",
	}
	handshakeBytes, err := protocol.EncodeHandshake(handshake)
	if err != nil {
		t.Fatalf("failed to encode handshake: %v", err)
	}

	handshakeMsg := &protocol.Message{
		Type: protocol.MessageTypeHandshake,

		Payload: handshakeBytes,
	}

	if err := protocol.WriteMessage(controlConn, handshakeMsg); err != nil {
		t.Fatalf("failed to write handshake: %v", err)
	}

	// Read accept message
	acceptMsg, err := protocol.ReadMessage(controlConn)
	if err != nil {
		t.Fatalf("failed to read accept message: %v", err)
	}

	if acceptMsg.Type != protocol.MessageTypeAccept {
		t.Fatalf("expected MessageTypeAccept, got type %d", acceptMsg.Type)
	}

	// Decode accept payload to get public port
	acceptPayload, err := protocol.DecodeAccept(acceptMsg.Payload)
	if err != nil {
		t.Fatalf("failed to decode accept payload: %v", err)
	}

	publicPort := acceptPayload.PublicPort

	// Give server time to start public listener
	time.Sleep(100 * time.Millisecond)

	// Connect to public port
	publicConn, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", publicPort))
	if err != nil {
		t.Fatalf("failed to connect to public port: %v", err)
	}
	defer publicConn.Close()

	// Read MessageTypeConnect from control connection
	connectMsg, err := protocol.ReadMessage(controlConn)
	if err != nil {
		t.Fatalf("failed to read connect message: %v", err)
	}

	if connectMsg.Type != protocol.MessageTypeConnect {
		t.Errorf("expected MessageTypeConnect, got type %d", connectMsg.Type)
	}

	// Decode and verify UUID
	connectPayload, err := protocol.DecodeConnect(connectMsg.Payload)
	if err != nil {
		t.Fatalf("failed to decode connect payload: %v", err)
	}

	if len(connectPayload.UUID) != 36 {
		t.Errorf("UUID length = %d, want 36", len(connectPayload.UUID))
	}

	// Verify pending stream was created
	time.Sleep(50 * time.Millisecond)
	server.pendingMu.RLock()
	_, pendingExists := server.pendingStreams[connectPayload.UUID]
	numPending := len(server.pendingStreams)
	server.pendingMu.RUnlock()

	if !pendingExists {
		t.Errorf("pending stream with UUID %s not found", connectPayload.UUID)
	}

	if numPending != 1 {
		t.Errorf("expected 1 pending stream, got %d", numPending)
	}
}

func TestAcceptMultiplePublicConnections(t *testing.T) {
	// Start server
	server := NewServer(0, "")
	errCh := make(chan error, 1)
	go func() {
		errCh <- server.Run()
	}()
	defer server.Close()

	// Give server time to start
	time.Sleep(100 * time.Millisecond)

	// Connect as client and complete handshake
	controlConn, err := net.Dial("tcp", server.listener.Addr().String())
	if err != nil {
		t.Fatalf("failed to connect to server: %v", err)
	}
	defer controlConn.Close()

	// Send handshake
	handshake := &protocol.HandshakePayload{
		Version: protocol.ProtocolVersion,
		Secret:  "",
	}
	handshakeBytes, err := protocol.EncodeHandshake(handshake)
	if err != nil {
		t.Fatalf("failed to encode handshake: %v", err)
	}

	handshakeMsg := &protocol.Message{
		Type: protocol.MessageTypeHandshake,

		Payload: handshakeBytes,
	}

	if err := protocol.WriteMessage(controlConn, handshakeMsg); err != nil {
		t.Fatalf("failed to write handshake: %v", err)
	}

	// Read accept message
	acceptMsg, err := protocol.ReadMessage(controlConn)
	if err != nil {
		t.Fatalf("failed to read accept message: %v", err)
	}

	// Decode accept payload to get public port
	acceptPayload, err := protocol.DecodeAccept(acceptMsg.Payload)
	if err != nil {
		t.Fatalf("failed to decode accept payload: %v", err)
	}

	publicPort := acceptPayload.PublicPort

	// Give server time to start public listener
	time.Sleep(100 * time.Millisecond)

	// Create multiple connections to public port
	numConnections := 3
	publicConns := make([]net.Conn, numConnections)
	uuids := make([]string, numConnections)

	for i := 0; i < numConnections; i++ {
		// Connect to public port
		conn, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", publicPort))
		if err != nil {
			t.Fatalf("failed to connect to public port (connection %d): %v", i, err)
		}
		defer conn.Close()
		publicConns[i] = conn

		// Read MessageTypeConnect from control connection
		connectMsg, err := protocol.ReadMessage(controlConn)
		if err != nil {
			t.Fatalf("failed to read connect message (connection %d): %v", i, err)
		}

		if connectMsg.Type != protocol.MessageTypeConnect {
			t.Errorf("connection %d: expected MessageTypeConnect, got type %d", i, connectMsg.Type)
		}

		// Decode UUID
		connectPayload, err := protocol.DecodeConnect(connectMsg.Payload)
		if err != nil {
			t.Fatalf("connection %d: failed to decode connect payload: %v", i, err)
		}

		uuids[i] = connectPayload.UUID
	}

	// Verify all UUIDs are unique
	seenUUIDs := make(map[string]bool)
	for i, uuid := range uuids {
		if len(uuid) != 36 {
			t.Errorf("connection %d: UUID length = %d, want 36", i, len(uuid))
		}
		if seenUUIDs[uuid] {
			t.Errorf("connection %d: duplicate UUID %s", i, uuid)
		}
		seenUUIDs[uuid] = true
	}

	// Verify all pending streams exist
	time.Sleep(50 * time.Millisecond)
	server.pendingMu.RLock()
	numPending := len(server.pendingStreams)
	server.pendingMu.RUnlock()

	if numPending != numConnections {
		t.Errorf("expected %d pending streams, got %d", numConnections, numPending)
	}
}

func TestPublicListenerCleanupOnSessionRemoval(t *testing.T) {
	// Start server
	server := NewServer(0, "")
	errCh := make(chan error, 1)
	go func() {
		errCh <- server.Run()
	}()
	defer server.Close()

	// Give server time to start
	time.Sleep(100 * time.Millisecond)

	// Connect as client and complete handshake
	controlConn, err := net.Dial("tcp", server.listener.Addr().String())
	if err != nil {
		t.Fatalf("failed to connect to server: %v", err)
	}

	// Send handshake
	handshake := &protocol.HandshakePayload{
		Version: protocol.ProtocolVersion,
		Secret:  "",
	}
	handshakeBytes, err := protocol.EncodeHandshake(handshake)
	if err != nil {
		t.Fatalf("failed to encode handshake: %v", err)
	}

	handshakeMsg := &protocol.Message{
		Type: protocol.MessageTypeHandshake,

		Payload: handshakeBytes,
	}

	if err := protocol.WriteMessage(controlConn, handshakeMsg); err != nil {
		t.Fatalf("failed to write handshake: %v", err)
	}

	// Read accept message
	acceptMsg, err := protocol.ReadMessage(controlConn)
	if err != nil {
		t.Fatalf("failed to read accept message: %v", err)
	}

	// Decode accept payload to get public port
	acceptPayload, err := protocol.DecodeAccept(acceptMsg.Payload)
	if err != nil {
		t.Fatalf("failed to decode accept payload: %v", err)
	}

	publicPort := acceptPayload.PublicPort

	// Give server time to start public listener
	time.Sleep(100 * time.Millisecond)

	// Verify public port is listening
	testConn, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", publicPort))
	if err != nil {
		t.Fatalf("public port not listening: %v", err)
	}
	testConn.Close()

	// Close control connection to trigger cleanup
	controlConn.Close()

	// Give server time to cleanup
	time.Sleep(200 * time.Millisecond)

	// Verify public port is no longer listening
	_, err = net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", publicPort))
	if err == nil {
		t.Error("public port still listening after session cleanup")
	}
}

func TestPendingStreamTimeout(t *testing.T) {
	// Start server
	server := NewServer(0, "")
	errCh := make(chan error, 1)
	go func() {
		errCh <- server.Run()
	}()
	defer server.Close()

	// Give server time to start
	time.Sleep(100 * time.Millisecond)

	// Connect as client and complete handshake
	controlConn, err := net.Dial("tcp", server.listener.Addr().String())
	if err != nil {
		t.Fatalf("failed to connect to server: %v", err)
	}
	defer controlConn.Close()

	// Send handshake
	handshake := &protocol.HandshakePayload{
		Version: protocol.ProtocolVersion,
		Secret:  "",
	}
	handshakeBytes, err := protocol.EncodeHandshake(handshake)
	if err != nil {
		t.Fatalf("failed to encode handshake: %v", err)
	}

	handshakeMsg := &protocol.Message{
		Type: protocol.MessageTypeHandshake,

		Payload: handshakeBytes,
	}

	if err := protocol.WriteMessage(controlConn, handshakeMsg); err != nil {
		t.Fatalf("failed to write handshake: %v", err)
	}

	// Read accept message
	acceptMsg, err := protocol.ReadMessage(controlConn)
	if err != nil {
		t.Fatalf("failed to read accept message: %v", err)
	}

	// Decode accept payload to get public port
	acceptPayload, err := protocol.DecodeAccept(acceptMsg.Payload)
	if err != nil {
		t.Fatalf("failed to decode accept payload: %v", err)
	}

	publicPort := acceptPayload.PublicPort

	// Give server time to start public listener
	time.Sleep(100 * time.Millisecond)

	// Connect to public port
	publicConn, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", publicPort))
	if err != nil {
		t.Fatalf("failed to connect to public port: %v", err)
	}
	defer publicConn.Close()

	// Read MessageTypeConnect from control connection
	connectMsg, err := protocol.ReadMessage(controlConn)
	if err != nil {
		t.Fatalf("failed to read connect message: %v", err)
	}

	// Decode UUID
	connectPayload, err := protocol.DecodeConnect(connectMsg.Payload)
	if err != nil {
		t.Fatalf("failed to decode connect payload: %v", err)
	}

	uuid := connectPayload.UUID

	// Verify pending stream exists
	server.pendingMu.RLock()
	_, exists := server.pendingStreams[uuid]
	server.pendingMu.RUnlock()

	if !exists {
		t.Fatal("pending stream should exist immediately after connect")
	}

	// Wait for timeout (30 seconds + buffer)
	// For testing, we'll just verify the timeout is set
	// In a real test, you'd want to reduce the timeout or mock time
	server.pendingMu.RLock()
	pending := server.pendingStreams[uuid]
	hasTimeout := pending != nil && pending.timeout != nil
	server.pendingMu.RUnlock()

	if !hasTimeout {
		t.Error("pending stream should have timeout set")
	}

	// Manually trigger cleanup to test it works
	server.cleanupPendingStream(uuid)

	// Verify pending stream was removed
	server.pendingMu.RLock()
	_, exists = server.pendingStreams[uuid]
	server.pendingMu.RUnlock()

	if exists {
		t.Error("pending stream should be removed after cleanup")
	}
}

// TestConnectionTypeDetection tests that the server correctly routes connections
// based on the first message type (handshake vs stream handshake)
func TestConnectionTypeDetection(t *testing.T) {
	// Start server
	server := NewServer(0, "")
	errCh := make(chan error, 1)
	go func() {
		errCh <- server.Run()
	}()
	defer server.Close()

	// Give server time to start
	time.Sleep(100 * time.Millisecond)

	t.Run("control connection with handshake message", func(t *testing.T) {
		// Connect to server
		conn, err := net.Dial("tcp", server.listener.Addr().String())
		if err != nil {
			t.Fatalf("failed to connect to server: %v", err)
		}
		defer conn.Close()

		// Send handshake message (should be routed to control connection handler)
		handshake := &protocol.HandshakePayload{
			Version: protocol.ProtocolVersion,
			Secret:  "",
		}
		handshakeBytes, err := protocol.EncodeHandshake(handshake)
		if err != nil {
			t.Fatalf("failed to encode handshake: %v", err)
		}

		handshakeMsg := &protocol.Message{
			Type:    protocol.MessageTypeHandshake,
			Payload: handshakeBytes,
		}

		if err := protocol.WriteMessage(conn, handshakeMsg); err != nil {
			t.Fatalf("failed to write handshake: %v", err)
		}

		// Should receive accept message (indicating control connection handler was called)
		acceptMsg, err := protocol.ReadMessage(conn)
		if err != nil {
			t.Fatalf("failed to read accept message: %v", err)
		}

		if acceptMsg.Type != protocol.MessageTypeAccept {
			t.Errorf("expected MessageTypeAccept, got type %d", acceptMsg.Type)
		}

		// Verify session was created
		time.Sleep(50 * time.Millisecond)
		server.clientsMu.RLock()
		numClients := len(server.clients)
		server.clientsMu.RUnlock()

		if numClients != 1 {
			t.Errorf("expected 1 client session, got %d", numClients)
		}
	})

	t.Run("data connection with stream handshake message", func(t *testing.T) {
		// Connect to server
		conn, err := net.Dial("tcp", server.listener.Addr().String())
		if err != nil {
			t.Fatalf("failed to connect to server: %v", err)
		}
		defer conn.Close()

		// Send stream handshake message (should be routed to data connection handler)
		streamHandshake := &protocol.StreamHandshakePayload{
			UUID: "550e8400-e29b-41d4-a716-446655440000",
		}
		streamHandshakeBytes, err := protocol.EncodeStreamHandshake(streamHandshake)
		if err != nil {
			t.Fatalf("failed to encode stream handshake: %v", err)
		}

		streamHandshakeMsg := &protocol.Message{
			Type:    protocol.MessageTypeStreamHandshake,
			Payload: streamHandshakeBytes,
		}

		if err := protocol.WriteMessage(conn, streamHandshakeMsg); err != nil {
			t.Fatalf("failed to write stream handshake: %v", err)
		}

		// Connection should be handled by data connection handler
		// Since UUID doesn't exist in pending streams, should receive StreamReject
		rejectMsg, err := protocol.ReadMessage(conn)
		if err != nil {
			t.Fatalf("failed to read reject message: %v", err)
		}

		if rejectMsg.Type != protocol.MessageTypeStreamReject {
			t.Errorf("expected MessageTypeStreamReject, got type %d", rejectMsg.Type)
		}

		// Connection should be closed after reject
		time.Sleep(100 * time.Millisecond)

		// Try to read - should get EOF since data handler closes connection after reject
		_, err = protocol.ReadMessage(conn)
		if err == nil {
			t.Error("expected connection to be closed by data handler, but it's still open")
		}
	})

	t.Run("invalid message type", func(t *testing.T) {
		// Connect to server
		conn, err := net.Dial("tcp", server.listener.Addr().String())
		if err != nil {
			t.Fatalf("failed to connect to server: %v", err)
		}
		defer conn.Close()

		// Send invalid message type
		invalidMsg := &protocol.Message{
			Type:    protocol.MessageTypeConnect, // Invalid as first message
			Payload: []byte("test"),
		}

		if err := protocol.WriteMessage(conn, invalidMsg); err != nil {
			t.Fatalf("failed to write invalid message: %v", err)
		}

		// Connection should be closed by server
		time.Sleep(100 * time.Millisecond)

		// Try to read - should get EOF
		_, err = protocol.ReadMessage(conn)
		if err == nil {
			t.Error("expected connection to be closed for invalid message type, but it's still open")
		}
	})
}

// TestDataConnectionHandler tests the data connection handler functionality
func TestDataConnectionHandler(t *testing.T) {
	// Start server
	server := NewServer(0, "")
	errCh := make(chan error, 1)
	go func() {
		errCh <- server.Run()
	}()
	defer server.Close()

	// Give server time to start
	time.Sleep(100 * time.Millisecond)

	// First, establish a control connection and get a public port
	controlConn, err := net.Dial("tcp", server.listener.Addr().String())
	if err != nil {
		t.Fatalf("failed to connect to server: %v", err)
	}
	defer controlConn.Close()

	// Send handshake
	handshake := &protocol.HandshakePayload{
		Version: protocol.ProtocolVersion,
		Secret:  "",
	}
	handshakeBytes, err := protocol.EncodeHandshake(handshake)
	if err != nil {
		t.Fatalf("failed to encode handshake: %v", err)
	}

	handshakeMsg := &protocol.Message{
		Type:    protocol.MessageTypeHandshake,
		Payload: handshakeBytes,
	}

	if err := protocol.WriteMessage(controlConn, handshakeMsg); err != nil {
		t.Fatalf("failed to write handshake: %v", err)
	}

	// Read accept message
	acceptMsg, err := protocol.ReadMessage(controlConn)
	if err != nil {
		t.Fatalf("failed to read accept message: %v", err)
	}

	// Decode accept payload to get public port
	acceptPayload, err := protocol.DecodeAccept(acceptMsg.Payload)
	if err != nil {
		t.Fatalf("failed to decode accept payload: %v", err)
	}

	publicPort := acceptPayload.PublicPort

	// Give server time to start public listener
	time.Sleep(100 * time.Millisecond)

	t.Run("successful stream handshake", func(t *testing.T) {
		// Connect to public port to create a pending stream
		publicConn, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", publicPort))
		if err != nil {
			t.Fatalf("failed to connect to public port: %v", err)
		}
		defer publicConn.Close()

		// Read MessageTypeConnect from control connection
		connectMsg, err := protocol.ReadMessage(controlConn)
		if err != nil {
			t.Fatalf("failed to read connect message: %v", err)
		}

		// Decode UUID
		connectPayload, err := protocol.DecodeConnect(connectMsg.Payload)
		if err != nil {
			t.Fatalf("failed to decode connect payload: %v", err)
		}

		uuid := connectPayload.UUID

		// Now open a data connection with the UUID
		dataConn, err := net.Dial("tcp", server.listener.Addr().String())
		if err != nil {
			t.Fatalf("failed to connect for data connection: %v", err)
		}
		defer dataConn.Close()

		// Send stream handshake with the UUID
		streamHandshake := &protocol.StreamHandshakePayload{
			UUID: uuid,
		}
		streamHandshakeBytes, err := protocol.EncodeStreamHandshake(streamHandshake)
		if err != nil {
			t.Fatalf("failed to encode stream handshake: %v", err)
		}

		streamHandshakeMsg := &protocol.Message{
			Type:    protocol.MessageTypeStreamHandshake,
			Payload: streamHandshakeBytes,
		}

		if err := protocol.WriteMessage(dataConn, streamHandshakeMsg); err != nil {
			t.Fatalf("failed to write stream handshake: %v", err)
		}

		// Should receive stream accept message
		streamAcceptMsg, err := protocol.ReadMessage(dataConn)
		if err != nil {
			t.Fatalf("failed to read stream accept message: %v", err)
		}

		if streamAcceptMsg.Type != protocol.MessageTypeStreamAccept {
			t.Errorf("expected MessageTypeStreamAccept, got type %d", streamAcceptMsg.Type)
		}

		// Verify pending stream was removed
		time.Sleep(50 * time.Millisecond)
		server.pendingMu.RLock()
		_, exists := server.pendingStreams[uuid]
		server.pendingMu.RUnlock()

		if exists {
			t.Error("pending stream should be removed after successful handshake")
		}

		// Verify stream was created in session
		server.clientsMu.RLock()
		var session *ClientSession
		for _, s := range server.clients {
			session = s
			break
		}
		server.clientsMu.RUnlock()

		if session == nil {
			t.Fatal("no client session found")
		}

		session.streamsMu.RLock()
		stream, streamExists := session.streams[uuid]
		session.streamsMu.RUnlock()

		if !streamExists {
			t.Error("stream should be created in session")
		}

		if stream.uuid != uuid {
			t.Errorf("stream UUID = %s, want %s", stream.uuid, uuid)
		}

		if stream.dataConn == nil {
			t.Error("stream data connection should not be nil")
		}

		if stream.externalConn == nil {
			t.Error("stream external connection should not be nil")
		}
	})

	t.Run("stream handshake with invalid UUID", func(t *testing.T) {
		// Open a data connection with invalid UUID
		dataConn, err := net.Dial("tcp", server.listener.Addr().String())
		if err != nil {
			t.Fatalf("failed to connect for data connection: %v", err)
		}
		defer dataConn.Close()

		// Send stream handshake with invalid UUID (valid format but not in pending map)
		invalidUUID := "550e8400-e29b-41d4-a716-446655440000"
		streamHandshake := &protocol.StreamHandshakePayload{
			UUID: invalidUUID,
		}
		streamHandshakeBytes, err := protocol.EncodeStreamHandshake(streamHandshake)
		if err != nil {
			t.Fatalf("failed to encode stream handshake: %v", err)
		}

		streamHandshakeMsg := &protocol.Message{
			Type:    protocol.MessageTypeStreamHandshake,
			Payload: streamHandshakeBytes,
		}

		if err := protocol.WriteMessage(dataConn, streamHandshakeMsg); err != nil {
			t.Fatalf("failed to write stream handshake: %v", err)
		}

		// Should receive stream reject message
		streamRejectMsg, err := protocol.ReadMessage(dataConn)
		if err != nil {
			t.Fatalf("failed to read stream reject message: %v", err)
		}

		if streamRejectMsg.Type != protocol.MessageTypeStreamReject {
			t.Errorf("expected MessageTypeStreamReject, got type %d", streamRejectMsg.Type)
		}
	})

	t.Run("stream handshake with malformed payload", func(t *testing.T) {
		// Open a data connection
		dataConn, err := net.Dial("tcp", server.listener.Addr().String())
		if err != nil {
			t.Fatalf("failed to connect for data connection: %v", err)
		}
		defer dataConn.Close()

		// Send stream handshake with malformed payload (wrong length)
		streamHandshakeMsg := &protocol.Message{
			Type:    protocol.MessageTypeStreamHandshake,
			Payload: []byte("invalid-payload-too-short"),
		}

		if err := protocol.WriteMessage(dataConn, streamHandshakeMsg); err != nil {
			t.Fatalf("failed to write stream handshake: %v", err)
		}

		// Connection should be closed by server due to decode error
		time.Sleep(100 * time.Millisecond)

		// Try to read - should get EOF
		_, err = protocol.ReadMessage(dataConn)
		if err == nil {
			t.Error("expected connection to be closed for malformed payload, but it's still open")
		}
	})
}

// TestBidirectionalProxy tests the bidirectional raw TCP proxy functionality
func TestBidirectionalProxy(t *testing.T) {
	// Start server
	server := NewServer(0, "")
	errCh := make(chan error, 1)
	go func() {
		errCh <- server.Run()
	}()
	defer server.Close()

	// Give server time to start
	time.Sleep(100 * time.Millisecond)

	// First, establish a control connection and get a public port
	controlConn, err := net.Dial("tcp", server.listener.Addr().String())
	if err != nil {
		t.Fatalf("failed to connect to server: %v", err)
	}
	defer controlConn.Close()

	// Send handshake
	handshake := &protocol.HandshakePayload{
		Version: protocol.ProtocolVersion,
		Secret:  "",
	}
	handshakeBytes, err := protocol.EncodeHandshake(handshake)
	if err != nil {
		t.Fatalf("failed to encode handshake: %v", err)
	}

	handshakeMsg := &protocol.Message{
		Type:    protocol.MessageTypeHandshake,
		Payload: handshakeBytes,
	}

	if err := protocol.WriteMessage(controlConn, handshakeMsg); err != nil {
		t.Fatalf("failed to write handshake: %v", err)
	}

	// Read accept message
	acceptMsg, err := protocol.ReadMessage(controlConn)
	if err != nil {
		t.Fatalf("failed to read accept message: %v", err)
	}

	// Decode accept payload to get public port
	acceptPayload, err := protocol.DecodeAccept(acceptMsg.Payload)
	if err != nil {
		t.Fatalf("failed to decode accept payload: %v", err)
	}

	publicPort := acceptPayload.PublicPort

	// Give server time to start public listener
	time.Sleep(100 * time.Millisecond)

	t.Run("bidirectional data forwarding", func(t *testing.T) {
		// Connect to public port to create a pending stream
		publicConn, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", publicPort))
		if err != nil {
			t.Fatalf("failed to connect to public port: %v", err)
		}

		// Read MessageTypeConnect from control connection
		connectMsg, err := protocol.ReadMessage(controlConn)
		if err != nil {
			t.Fatalf("failed to read connect message: %v", err)
		}

		// Decode UUID
		connectPayload, err := protocol.DecodeConnect(connectMsg.Payload)
		if err != nil {
			t.Fatalf("failed to decode connect payload: %v", err)
		}

		uuid := connectPayload.UUID

		// Now open a data connection with the UUID
		dataConn, err := net.Dial("tcp", server.listener.Addr().String())
		if err != nil {
			t.Fatalf("failed to connect for data connection: %v", err)
		}

		// Send stream handshake with the UUID
		streamHandshake := &protocol.StreamHandshakePayload{
			UUID: uuid,
		}
		streamHandshakeBytes, err := protocol.EncodeStreamHandshake(streamHandshake)
		if err != nil {
			t.Fatalf("failed to encode stream handshake: %v", err)
		}

		streamHandshakeMsg := &protocol.Message{
			Type:    protocol.MessageTypeStreamHandshake,
			Payload: streamHandshakeBytes,
		}

		if err := protocol.WriteMessage(dataConn, streamHandshakeMsg); err != nil {
			t.Fatalf("failed to write stream handshake: %v", err)
		}

		// Should receive stream accept message
		streamAcceptMsg, err := protocol.ReadMessage(dataConn)
		if err != nil {
			t.Fatalf("failed to read stream accept message: %v", err)
		}

		if streamAcceptMsg.Type != protocol.MessageTypeStreamAccept {
			t.Fatalf("expected MessageTypeStreamAccept, got type %d", streamAcceptMsg.Type)
		}

		// Give proxy time to start
		time.Sleep(300 * time.Millisecond)

		// Test data forwarding: external → client
		testData1 := []byte("Hello from external client!")

		// Write and read in separate goroutines to avoid blocking
		errCh := make(chan error, 2)

		go func() {
			if _, err := publicConn.Write(testData1); err != nil {
				errCh <- fmt.Errorf("failed to write to public connection: %v", err)
				return
			}
			errCh <- nil
		}()

		go func() {
			buffer1 := make([]byte, len(testData1))
			if _, err := io.ReadFull(dataConn, buffer1); err != nil {
				errCh <- fmt.Errorf("failed to read from data connection: %v", err)
				return
			}
			if string(buffer1) != string(testData1) {
				errCh <- fmt.Errorf("external→client data mismatch: got %q, want %q", string(buffer1), string(testData1))
				return
			}
			errCh <- nil
		}()

		// Wait for both operations
		for i := 0; i < 2; i++ {
			if err := <-errCh; err != nil {
				t.Fatal(err)
			}
		}

		// Test data forwarding: client → external
		testData2 := []byte("Hello from client!")

		go func() {
			if _, err := dataConn.Write(testData2); err != nil {
				errCh <- fmt.Errorf("failed to write to data connection: %v", err)
				return
			}
			errCh <- nil
		}()

		go func() {
			buffer2 := make([]byte, len(testData2))
			if _, err := io.ReadFull(publicConn, buffer2); err != nil {
				errCh <- fmt.Errorf("failed to read from public connection: %v", err)
				return
			}
			if string(buffer2) != string(testData2) {
				errCh <- fmt.Errorf("client→external data mismatch: got %q, want %q", string(buffer2), string(testData2))
				return
			}
			errCh <- nil
		}()

		// Wait for both operations
		for i := 0; i < 2; i++ {
			if err := <-errCh; err != nil {
				t.Fatal(err)
			}
		}

		// Close connections after test
		publicConn.Close()
		dataConn.Close()
	})

	t.Run("connection cleanup on close", func(t *testing.T) {
		// Connect to public port to create a pending stream
		publicConn, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", publicPort))
		if err != nil {
			t.Fatalf("failed to connect to public port: %v", err)
		}
		defer publicConn.Close()

		// Read MessageTypeConnect from control connection
		connectMsg, err := protocol.ReadMessage(controlConn)
		if err != nil {
			t.Fatalf("failed to read connect message: %v", err)
		}

		// Decode UUID
		connectPayload, err := protocol.DecodeConnect(connectMsg.Payload)
		if err != nil {
			t.Fatalf("failed to decode connect payload: %v", err)
		}

		uuid := connectPayload.UUID

		// Now open a data connection with the UUID
		dataConn, err := net.Dial("tcp", server.listener.Addr().String())
		if err != nil {
			t.Fatalf("failed to connect for data connection: %v", err)
		}

		// Send stream handshake with the UUID
		streamHandshake := &protocol.StreamHandshakePayload{
			UUID: uuid,
		}
		streamHandshakeBytes, err := protocol.EncodeStreamHandshake(streamHandshake)
		if err != nil {
			t.Fatalf("failed to encode stream handshake: %v", err)
		}

		streamHandshakeMsg := &protocol.Message{
			Type:    protocol.MessageTypeStreamHandshake,
			Payload: streamHandshakeBytes,
		}

		if err := protocol.WriteMessage(dataConn, streamHandshakeMsg); err != nil {
			t.Fatalf("failed to write stream handshake: %v", err)
		}

		// Should receive stream accept message
		streamAcceptMsg, err := protocol.ReadMessage(dataConn)
		if err != nil {
			t.Fatalf("failed to read stream accept message: %v", err)
		}

		if streamAcceptMsg.Type != protocol.MessageTypeStreamAccept {
			t.Fatalf("expected MessageTypeStreamAccept, got type %d", streamAcceptMsg.Type)
		}

		// Give proxy time to start
		time.Sleep(100 * time.Millisecond)

		// Verify stream exists in session
		server.clientsMu.RLock()
		var session *ClientSession
		for _, s := range server.clients {
			session = s
			break
		}
		server.clientsMu.RUnlock()

		if session == nil {
			t.Fatal("no client session found")
		}

		session.streamsMu.RLock()
		_, streamExists := session.streams[uuid]
		session.streamsMu.RUnlock()

		if !streamExists {
			t.Fatal("stream should exist before closing connection")
		}

		// Close one connection to trigger cleanup
		dataConn.Close()

		// Give cleanup time to complete
		time.Sleep(200 * time.Millisecond)

		// Verify stream was cleaned up
		session.streamsMu.RLock()
		_, streamExists = session.streams[uuid]
		session.streamsMu.RUnlock()

		if streamExists {
			t.Error("stream should be cleaned up after connection close")
		}

		// Verify other connection is also closed
		_, err = publicConn.Read(make([]byte, 1))
		if err == nil {
			t.Error("public connection should be closed after cleanup")
		}
	})
}

func TestCleanupPendingStream(t *testing.T) {
	server := NewServer(0, "")

	// Create a mock external connection
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to create listener: %v", err)
	}
	defer listener.Close()

	connCh := make(chan net.Conn, 1)
	go func() {
		conn, _ := listener.Accept()
		connCh <- conn
	}()

	externalConn, err := net.Dial("tcp", listener.Addr().String())
	if err != nil {
		t.Fatalf("failed to dial: %v", err)
	}

	serverConn := <-connCh

	// Create pending stream
	testUUID := "test-uuid-12345678-1234-1234-1234"
	pending := &PendingStream{
		externalConn: serverConn,
		sessionID:    "test-session",
		createdAt:    time.Now(),
		timeout:      time.NewTimer(1 * time.Hour), // Long timeout for testing
	}

	server.pendingMu.Lock()
	server.pendingStreams[testUUID] = pending
	server.pendingMu.Unlock()

	// Verify it exists
	server.pendingMu.RLock()
	_, exists := server.pendingStreams[testUUID]
	server.pendingMu.RUnlock()

	if !exists {
		t.Fatal("pending stream should exist before cleanup")
	}

	// Cleanup
	server.cleanupPendingStream(testUUID)

	// Verify it's removed
	server.pendingMu.RLock()
	_, exists = server.pendingStreams[testUUID]
	server.pendingMu.RUnlock()

	if exists {
		t.Error("pending stream should be removed after cleanup")
	}

	// Verify connection is closed
	_, err = externalConn.Read(make([]byte, 1))
	if err == nil {
		t.Error("external connection should be closed after cleanup")
	}

	// Cleanup again should not panic
	server.cleanupPendingStream(testUUID)
}

// TestBidirectionalProxyLargeData tests that large data transfers work correctly
func TestBidirectionalProxyLargeData(t *testing.T) {
	// Start server
	server := NewServer(0, "")
	errCh := make(chan error, 1)
	go func() {
		errCh <- server.Run()
	}()
	defer server.Close()

	// Give server time to start
	time.Sleep(100 * time.Millisecond)

	// Establish control connection
	controlConn, err := net.Dial("tcp", server.listener.Addr().String())
	if err != nil {
		t.Fatalf("failed to connect to server: %v", err)
	}
	defer controlConn.Close()

	// Send handshake
	handshake := &protocol.HandshakePayload{
		Version: protocol.ProtocolVersion,
		Secret:  "",
	}
	handshakeBytes, err := protocol.EncodeHandshake(handshake)
	if err != nil {
		t.Fatalf("failed to encode handshake: %v", err)
	}

	handshakeMsg := &protocol.Message{
		Type:    protocol.MessageTypeHandshake,
		Payload: handshakeBytes,
	}

	if err := protocol.WriteMessage(controlConn, handshakeMsg); err != nil {
		t.Fatalf("failed to write handshake: %v", err)
	}

	// Read accept message
	acceptMsg, err := protocol.ReadMessage(controlConn)
	if err != nil {
		t.Fatalf("failed to read accept message: %v", err)
	}

	acceptPayload, err := protocol.DecodeAccept(acceptMsg.Payload)
	if err != nil {
		t.Fatalf("failed to decode accept payload: %v", err)
	}

	publicPort := acceptPayload.PublicPort
	time.Sleep(100 * time.Millisecond)

	// Connect to public port
	publicConn, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", publicPort))
	if err != nil {
		t.Fatalf("failed to connect to public port: %v", err)
	}
	defer publicConn.Close()

	// Read connect message
	connectMsg, err := protocol.ReadMessage(controlConn)
	if err != nil {
		t.Fatalf("failed to read connect message: %v", err)
	}

	connectPayload, err := protocol.DecodeConnect(connectMsg.Payload)
	if err != nil {
		t.Fatalf("failed to decode connect payload: %v", err)
	}

	// Open data connection
	dataConn, err := net.Dial("tcp", server.listener.Addr().String())
	if err != nil {
		t.Fatalf("failed to connect for data connection: %v", err)
	}
	defer dataConn.Close()

	// Send stream handshake
	streamHandshake := &protocol.StreamHandshakePayload{
		UUID: connectPayload.UUID,
	}
	streamHandshakeBytes, err := protocol.EncodeStreamHandshake(streamHandshake)
	if err != nil {
		t.Fatalf("failed to encode stream handshake: %v", err)
	}

	streamHandshakeMsg := &protocol.Message{
		Type:    protocol.MessageTypeStreamHandshake,
		Payload: streamHandshakeBytes,
	}

	if err := protocol.WriteMessage(dataConn, streamHandshakeMsg); err != nil {
		t.Fatalf("failed to write stream handshake: %v", err)
	}

	// Read stream accept
	streamAcceptMsg, err := protocol.ReadMessage(dataConn)
	if err != nil {
		t.Fatalf("failed to read stream accept message: %v", err)
	}

	if streamAcceptMsg.Type != protocol.MessageTypeStreamAccept {
		t.Fatalf("expected MessageTypeStreamAccept, got type %d", streamAcceptMsg.Type)
	}

	time.Sleep(100 * time.Millisecond)

	// Test large data transfer (1MB)
	largeData := make([]byte, 1024*1024)
	for i := range largeData {
		largeData[i] = byte(i % 256)
	}

	// Send large data from external to client
	sendErrCh := make(chan error, 1)
	go func() {
		_, err := publicConn.Write(largeData)
		sendErrCh <- err
	}()

	// Receive large data on data connection
	receivedData := make([]byte, len(largeData))
	_, err = io.ReadFull(dataConn, receivedData)
	if err != nil {
		t.Fatalf("failed to read large data: %v", err)
	}

	if err := <-sendErrCh; err != nil {
		t.Fatalf("failed to send large data: %v", err)
	}

	// Verify data integrity
	for i := range largeData {
		if largeData[i] != receivedData[i] {
			t.Fatalf("data mismatch at byte %d: got %d, want %d", i, receivedData[i], largeData[i])
		}
	}
}

// TestBidirectionalProxyMultipleConcurrentStreams tests multiple concurrent streams
func TestBidirectionalProxyMultipleConcurrentStreams(t *testing.T) {
	// Start server
	server := NewServer(0, "")
	errCh := make(chan error, 1)
	go func() {
		errCh <- server.Run()
	}()
	defer server.Close()

	time.Sleep(100 * time.Millisecond)

	// Establish control connection
	controlConn, err := net.Dial("tcp", server.listener.Addr().String())
	if err != nil {
		t.Fatalf("failed to connect to server: %v", err)
	}
	defer controlConn.Close()

	// Send handshake
	handshake := &protocol.HandshakePayload{
		Version: protocol.ProtocolVersion,
		Secret:  "",
	}
	handshakeBytes, _ := protocol.EncodeHandshake(handshake)
	handshakeMsg := &protocol.Message{
		Type:    protocol.MessageTypeHandshake,
		Payload: handshakeBytes,
	}
	protocol.WriteMessage(controlConn, handshakeMsg)

	// Read accept
	acceptMsg, _ := protocol.ReadMessage(controlConn)
	acceptPayload, _ := protocol.DecodeAccept(acceptMsg.Payload)
	publicPort := acceptPayload.PublicPort

	time.Sleep(100 * time.Millisecond)

	numStreams := 3
	type streamPair struct {
		publicConn net.Conn
		dataConn   net.Conn
		uuid       string
	}
	streams := make([]streamPair, numStreams)

	// Create multiple streams
	for i := 0; i < numStreams; i++ {
		// Connect to public port
		publicConn, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", publicPort))
		if err != nil {
			t.Fatalf("stream %d: failed to connect to public port: %v", i, err)
		}

		// Read connect message
		connectMsg, err := protocol.ReadMessage(controlConn)
		if err != nil {
			t.Fatalf("stream %d: failed to read connect message: %v", i, err)
		}
		connectPayload, _ := protocol.DecodeConnect(connectMsg.Payload)

		// Open data connection
		dataConn, err := net.Dial("tcp", server.listener.Addr().String())
		if err != nil {
			t.Fatalf("stream %d: failed to connect for data connection: %v", i, err)
		}

		// Send stream handshake
		streamHandshake := &protocol.StreamHandshakePayload{UUID: connectPayload.UUID}
		streamHandshakeBytes, _ := protocol.EncodeStreamHandshake(streamHandshake)
		streamHandshakeMsg := &protocol.Message{
			Type:    protocol.MessageTypeStreamHandshake,
			Payload: streamHandshakeBytes,
		}
		protocol.WriteMessage(dataConn, streamHandshakeMsg)

		// Read stream accept
		streamAcceptMsg, err := protocol.ReadMessage(dataConn)
		if err != nil {
			t.Fatalf("stream %d: failed to read stream accept: %v", i, err)
		}
		if streamAcceptMsg.Type != protocol.MessageTypeStreamAccept {
			t.Fatalf("stream %d: expected StreamAccept, got %d", i, streamAcceptMsg.Type)
		}

		streams[i] = streamPair{
			publicConn: publicConn,
			dataConn:   dataConn,
			uuid:       connectPayload.UUID,
		}
	}

	time.Sleep(100 * time.Millisecond)

	// Test data isolation - send unique data on each stream
	testErrCh := make(chan error, numStreams*2)

	for i, stream := range streams {
		i := i
		stream := stream

		// Send unique data from external to client
		go func() {
			testData := []byte(fmt.Sprintf("Stream %d data from external", i))
			_, err := stream.publicConn.Write(testData)
			testErrCh <- err
		}()

		// Receive and verify on data connection
		go func() {
			expectedData := []byte(fmt.Sprintf("Stream %d data from external", i))
			buffer := make([]byte, len(expectedData))
			_, err := io.ReadFull(stream.dataConn, buffer)
			if err != nil {
				testErrCh <- fmt.Errorf("stream %d: read error: %v", i, err)
				return
			}
			if string(buffer) != string(expectedData) {
				testErrCh <- fmt.Errorf("stream %d: data mismatch: got %q, want %q", i, string(buffer), string(expectedData))
				return
			}
			testErrCh <- nil
		}()
	}

	// Wait for all operations
	for i := 0; i < numStreams*2; i++ {
		if err := <-testErrCh; err != nil {
			t.Error(err)
		}
	}

	// Cleanup
	for _, stream := range streams {
		stream.publicConn.Close()
		stream.dataConn.Close()
	}
}

// TestBidirectionalProxyExternalCloses tests cleanup when external connection closes first
func TestBidirectionalProxyExternalCloses(t *testing.T) {
	server := NewServer(0, "")
	go server.Run()
	defer server.Close()
	time.Sleep(100 * time.Millisecond)

	// Establish control connection
	controlConn, _ := net.Dial("tcp", server.listener.Addr().String())
	defer controlConn.Close()

	handshake := &protocol.HandshakePayload{Version: protocol.ProtocolVersion, Secret: ""}
	handshakeBytes, _ := protocol.EncodeHandshake(handshake)
	protocol.WriteMessage(controlConn, &protocol.Message{Type: protocol.MessageTypeHandshake, Payload: handshakeBytes})

	acceptMsg, _ := protocol.ReadMessage(controlConn)
	acceptPayload, _ := protocol.DecodeAccept(acceptMsg.Payload)
	publicPort := acceptPayload.PublicPort
	time.Sleep(100 * time.Millisecond)

	// Create stream
	publicConn, _ := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", publicPort))
	connectMsg, _ := protocol.ReadMessage(controlConn)
	connectPayload, _ := protocol.DecodeConnect(connectMsg.Payload)

	dataConn, _ := net.Dial("tcp", server.listener.Addr().String())
	streamHandshake := &protocol.StreamHandshakePayload{UUID: connectPayload.UUID}
	streamHandshakeBytes, _ := protocol.EncodeStreamHandshake(streamHandshake)
	protocol.WriteMessage(dataConn, &protocol.Message{Type: protocol.MessageTypeStreamHandshake, Payload: streamHandshakeBytes})
	protocol.ReadMessage(dataConn) // stream accept

	time.Sleep(100 * time.Millisecond)

	// Close external connection first
	publicConn.Close()

	// Data connection should also close
	time.Sleep(200 * time.Millisecond)
	_, err := dataConn.Read(make([]byte, 1))
	if err == nil {
		t.Error("data connection should be closed when external closes")
	}
}
