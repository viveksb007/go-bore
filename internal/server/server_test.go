package server

import (
	"fmt"
	"io"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/viveksb007/go-bore/internal/protocol"
)

// TestEnvironment provides a reusable test setup similar to k8s envtest.
// It manages server lifecycle and provides helpers for common test operations.
type TestEnvironment struct {
	Server *Server
	t      *testing.T
	errCh  chan error
}

// TestEnvConfig holds configuration for the test environment.
type TestEnvConfig struct {
	Port   int
	Secret string
}

// NewTestEnvironment creates and starts a new test environment.
func NewTestEnvironment(t *testing.T, cfg *TestEnvConfig) *TestEnvironment {
	t.Helper()

	if cfg == nil {
		cfg = &TestEnvConfig{}
	}

	server := NewServer(cfg.Port, cfg.Secret)
	env := &TestEnvironment{
		Server: server,
		t:      t,
		errCh:  make(chan error, 1),
	}

	go func() {
		env.errCh <- server.Run()
	}()

	// Wait for server to start
	time.Sleep(100 * time.Millisecond)

	if server.listener == nil {
		t.Fatal("server failed to start")
	}

	return env
}

// Stop shuts down the test environment.
func (env *TestEnvironment) Stop() {
	env.t.Helper()
	if err := env.Server.Close(); err != nil {
		env.t.Errorf("failed to close server: %v", err)
	}
}

// ServerAddr returns the server's listen address.
func (env *TestEnvironment) ServerAddr() string {
	return env.Server.listener.Addr().String()
}

// ConnectedClient represents a client that has completed handshake with the server.
type ConnectedClient struct {
	ControlConn net.Conn
	PublicPort  uint16
	env         *TestEnvironment
}

// ConnectClient establishes a control connection and completes handshake.
func (env *TestEnvironment) ConnectClient(secret string) *ConnectedClient {
	env.t.Helper()

	conn, err := net.Dial("tcp", env.ServerAddr())
	if err != nil {
		env.t.Fatalf("failed to connect to server: %v", err)
	}

	// Send handshake
	handshake := &protocol.HandshakePayload{
		Version: protocol.ProtocolVersion,
		Secret:  secret,
	}
	handshakeBytes, err := protocol.EncodeHandshake(handshake)
	if err != nil {
		conn.Close()
		env.t.Fatalf("failed to encode handshake: %v", err)
	}

	msg := &protocol.Message{
		Type:    protocol.MessageTypeHandshake,
		Payload: handshakeBytes,
	}

	if err := protocol.WriteMessage(conn, msg); err != nil {
		conn.Close()
		env.t.Fatalf("failed to write handshake: %v", err)
	}

	// Read accept message
	acceptMsg, err := protocol.ReadMessage(conn)
	if err != nil {
		conn.Close()
		env.t.Fatalf("failed to read accept message: %v", err)
	}

	if acceptMsg.Type != protocol.MessageTypeAccept {
		conn.Close()
		env.t.Fatalf("expected MessageTypeAccept, got type %d", acceptMsg.Type)
	}

	acceptPayload, err := protocol.DecodeAccept(acceptMsg.Payload)
	if err != nil {
		conn.Close()
		env.t.Fatalf("failed to decode accept payload: %v", err)
	}

	// Wait for public listener to start
	time.Sleep(100 * time.Millisecond)

	return &ConnectedClient{
		ControlConn: conn,
		PublicPort:  acceptPayload.PublicPort,
		env:         env,
	}
}

// Close closes the client's control connection.
func (c *ConnectedClient) Close() {
	c.ControlConn.Close()
}

// ConnectToPublicPort connects to the client's public port and returns the connection
// along with the UUID received from the control connection.
func (c *ConnectedClient) ConnectToPublicPort() (publicConn net.Conn, uuid string) {
	c.env.t.Helper()

	conn, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", c.PublicPort))
	if err != nil {
		c.env.t.Fatalf("failed to connect to public port: %v", err)
	}

	// Read connect message from control connection
	connectMsg, err := protocol.ReadMessage(c.ControlConn)
	if err != nil {
		conn.Close()
		c.env.t.Fatalf("failed to read connect message: %v", err)
	}

	if connectMsg.Type != protocol.MessageTypeConnect {
		conn.Close()
		c.env.t.Fatalf("expected MessageTypeConnect, got type %d", connectMsg.Type)
	}

	connectPayload, err := protocol.DecodeConnect(connectMsg.Payload)
	if err != nil {
		conn.Close()
		c.env.t.Fatalf("failed to decode connect payload: %v", err)
	}

	return conn, connectPayload.UUID
}

// EstablishStream creates a complete stream by connecting to public port and
// completing the data connection handshake.
func (c *ConnectedClient) EstablishStream() (publicConn, dataConn net.Conn, uuid string) {
	c.env.t.Helper()

	publicConn, uuid = c.ConnectToPublicPort()

	// Open data connection
	dataConn, err := net.Dial("tcp", c.env.ServerAddr())
	if err != nil {
		publicConn.Close()
		c.env.t.Fatalf("failed to connect for data connection: %v", err)
	}

	// Send stream handshake
	streamHandshake := &protocol.StreamHandshakePayload{UUID: uuid}
	streamHandshakeBytes, err := protocol.EncodeStreamHandshake(streamHandshake)
	if err != nil {
		publicConn.Close()
		dataConn.Close()
		c.env.t.Fatalf("failed to encode stream handshake: %v", err)
	}

	msg := &protocol.Message{
		Type:    protocol.MessageTypeStreamHandshake,
		Payload: streamHandshakeBytes,
	}

	if err := protocol.WriteMessage(dataConn, msg); err != nil {
		publicConn.Close()
		dataConn.Close()
		c.env.t.Fatalf("failed to write stream handshake: %v", err)
	}

	// Read stream accept
	acceptMsg, err := protocol.ReadMessage(dataConn)
	if err != nil {
		publicConn.Close()
		dataConn.Close()
		c.env.t.Fatalf("failed to read stream accept: %v", err)
	}

	if acceptMsg.Type != protocol.MessageTypeStreamAccept {
		publicConn.Close()
		dataConn.Close()
		c.env.t.Fatalf("expected MessageTypeStreamAccept, got type %d", acceptMsg.Type)
	}

	// Wait for proxy to start
	time.Sleep(100 * time.Millisecond)

	return publicConn, dataConn, uuid
}

// MockConnPair creates a pair of connected net.Conn for testing.
type MockConnPair struct {
	listener   net.Listener
	ServerConn net.Conn
	ClientConn net.Conn
}

// NewMockConnPair creates a new mock connection pair.
func NewMockConnPair(t *testing.T) *MockConnPair {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to create listener: %v", err)
	}

	connCh := make(chan net.Conn, 1)
	go func() {
		conn, _ := listener.Accept()
		connCh <- conn
	}()

	clientConn, err := net.Dial("tcp", listener.Addr().String())
	if err != nil {
		listener.Close()
		t.Fatalf("failed to dial: %v", err)
	}

	serverConn := <-connCh

	return &MockConnPair{
		listener:   listener,
		ServerConn: serverConn,
		ClientConn: clientConn,
	}
}

// Close closes all connections in the pair.
func (p *MockConnPair) Close() {
	p.ClientConn.Close()
	p.ServerConn.Close()
	p.listener.Close()
}

// ============================================================================
// Unit Tests
// ============================================================================

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
	env := NewTestEnvironment(t, nil)
	defer env.Stop()

	if env.Server.listener == nil {
		t.Fatal("server listener is nil after Run()")
	}
}

func TestServerRunPortInUse(t *testing.T) {
	env := NewTestEnvironment(t, nil)
	defer env.Stop()

	addr := env.Server.listener.Addr().(*net.TCPAddr)
	usedPort := addr.Port

	// Try to start second server on same port
	server2 := NewServer(usedPort, "")
	err := server2.Run()

	if err == nil {
		t.Error("expected error when port is already in use, got nil")
		server2.Close()
	}
}

func TestCreateSession(t *testing.T) {
	server := NewServer(8080, "test-secret")
	pair := NewMockConnPair(t)
	defer pair.Close()

	session := server.createSession(pair.ServerConn)

	if session == nil {
		t.Fatal("createSession returned nil")
	}
	if session.id == "" {
		t.Error("session ID is empty")
	}
	if session.conn != pair.ServerConn {
		t.Error("session connection does not match")
	}
	if session.streams == nil {
		t.Error("session streams map is nil")
	}

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
	pair := NewMockConnPair(t)
	defer pair.Close()

	session := server.createSession(pair.ServerConn)
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
	pair := NewMockConnPair(t)
	defer pair.Close()

	// Create session with allocated port
	session := &ClientSession{
		id:      "test-session",
		conn:    pair.ServerConn,
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
	streamPair := NewMockConnPair(t)
	defer streamPair.Close()

	stream := &ServerStream{
		uuid:         "test-uuid-1234",
		externalConn: streamPair.ServerConn,
		createdAt:    time.Now(),
	}
	session.streams["test-uuid-1234"] = stream

	// Cleanup session
	session.cleanup(portAllocator)

	// Verify resources are cleaned up
	if len(session.streams) != 0 {
		t.Errorf("streams not cleaned up, got %d streams", len(session.streams))
	}

	// Verify port is released
	portAllocator.Release(port)
	newPort, err := portAllocator.Allocate()
	if err != nil {
		t.Errorf("failed to allocate port after cleanup: %v", err)
	}
	if newPort != port {
		portAllocator.Release(newPort)
	}
}

func TestMultipleSessions(t *testing.T) {
	server := NewServer(0, "")
	numSessions := 5
	sessions := make([]*ClientSession, numSessions)
	pairs := make([]*MockConnPair, numSessions)

	for i := 0; i < numSessions; i++ {
		pairs[i] = NewMockConnPair(t)
		defer pairs[i].Close()
		sessions[i] = server.createSession(pairs[i].ServerConn)
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

// ============================================================================
// Handshake Tests
// ============================================================================

func TestHandshakeSuccess(t *testing.T) {
	env := NewTestEnvironment(t, &TestEnvConfig{Secret: "test-secret"})
	defer env.Stop()

	client := env.ConnectClient("test-secret")
	defer client.Close()

	if client.PublicPort == 0 {
		t.Error("public port is 0")
	}

	// Verify session was created
	time.Sleep(50 * time.Millisecond)
	env.Server.clientsMu.RLock()
	numClients := len(env.Server.clients)
	env.Server.clientsMu.RUnlock()

	if numClients != 1 {
		t.Errorf("expected 1 client session, got %d", numClients)
	}
}

func TestHandshakeAuthenticationFailure(t *testing.T) {
	env := NewTestEnvironment(t, &TestEnvConfig{Secret: "correct-secret"})
	defer env.Stop()

	conn, err := net.Dial("tcp", env.ServerAddr())
	if err != nil {
		t.Fatalf("failed to connect to server: %v", err)
	}
	defer conn.Close()

	// Send handshake with wrong secret
	handshake := &protocol.HandshakePayload{
		Version: protocol.ProtocolVersion,
		Secret:  "wrong-secret",
	}
	handshakeBytes, _ := protocol.EncodeHandshake(handshake)
	msg := &protocol.Message{Type: protocol.MessageTypeHandshake, Payload: handshakeBytes}
	protocol.WriteMessage(conn, msg)

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
	env.Server.clientsMu.RLock()
	numClients := len(env.Server.clients)
	env.Server.clientsMu.RUnlock()

	if numClients != 0 {
		t.Errorf("expected 0 client sessions after failed auth, got %d", numClients)
	}
}

func TestHandshakeOpenMode(t *testing.T) {
	env := NewTestEnvironment(t, nil) // No secret = open mode
	defer env.Stop()

	client := env.ConnectClient("")
	defer client.Close()

	// Verify session was created
	time.Sleep(50 * time.Millisecond)
	env.Server.clientsMu.RLock()
	numClients := len(env.Server.clients)
	env.Server.clientsMu.RUnlock()

	if numClients != 1 {
		t.Errorf("expected 1 client session in open mode, got %d", numClients)
	}
}

func TestHandshakeInvalidProtocolVersion(t *testing.T) {
	env := NewTestEnvironment(t, nil)
	defer env.Stop()

	conn, err := net.Dial("tcp", env.ServerAddr())
	if err != nil {
		t.Fatalf("failed to connect to server: %v", err)
	}
	defer conn.Close()

	// Send handshake with invalid version
	handshake := &protocol.HandshakePayload{Version: 99, Secret: ""}
	handshakeBytes, _ := protocol.EncodeHandshake(handshake)
	msg := &protocol.Message{Type: protocol.MessageTypeHandshake, Payload: handshakeBytes}
	protocol.WriteMessage(conn, msg)

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
	env := NewTestEnvironment(t, nil)
	defer env.Stop()

	conn, err := net.Dial("tcp", env.ServerAddr())
	if err != nil {
		t.Fatalf("failed to connect to server: %v", err)
	}
	defer conn.Close()

	// Send wrong message type (not handshake)
	wrongMsg := &protocol.Message{
		Type:    protocol.MessageTypeConnect,
		Payload: []byte("test"),
	}
	protocol.WriteMessage(conn, wrongMsg)

	// Connection should be closed by server
	time.Sleep(100 * time.Millisecond)

	_, err = protocol.ReadMessage(conn)
	if err == nil {
		t.Error("expected error reading from closed connection, got nil")
	}
}

// ============================================================================
// Public Listener Tests
// ============================================================================

func TestCreatePublicListener(t *testing.T) {
	server := NewServer(0, "")

	port, err := server.portAllocator.Allocate()
	if err != nil {
		t.Fatalf("failed to allocate port: %v", err)
	}
	defer server.portAllocator.Release(port)

	listener, err := server.createPublicListener(port)
	if err != nil {
		t.Fatalf("createPublicListener() error = %v", err)
	}
	defer listener.Close()

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

	existingListener, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Fatalf("failed to create existing listener: %v", err)
	}
	defer existingListener.Close()

	usedPort := existingListener.Addr().(*net.TCPAddr).Port

	_, err = server.createPublicListener(usedPort)
	if err == nil {
		t.Error("expected error when port is in use, got nil")
	}
}

func TestUUIDGeneration(t *testing.T) {
	uuids := make(map[string]bool)

	for i := 0; i < 100; i++ {
		u := uuid.New().String()

		if len(u) != 36 {
			t.Errorf("UUID length = %d, want 36", len(u))
		}
		if uuids[u] {
			t.Errorf("Duplicate UUID generated: %s", u)
		}
		uuids[u] = true
	}
}

func TestSendConnect(t *testing.T) {
	server := NewServer(0, "")
	pair := NewMockConnPair(t)
	defer pair.Close()

	session := &ClientSession{
		id:      "test-session",
		conn:    pair.ServerConn,
		streams: make(map[string]*ServerStream),
	}

	testUUID := "550e8400-e29b-41d4-a716-446655440000"
	if err := server.sendConnect(session, testUUID); err != nil {
		t.Fatalf("sendConnect() error = %v", err)
	}

	// Read message on client side
	msg, err := protocol.ReadMessage(pair.ClientConn)
	if err != nil {
		t.Fatalf("failed to read message: %v", err)
	}

	if msg.Type != protocol.MessageTypeConnect {
		t.Errorf("message type = %d, want %d", msg.Type, protocol.MessageTypeConnect)
	}

	connectPayload, err := protocol.DecodeConnect(msg.Payload)
	if err != nil {
		t.Fatalf("failed to decode connect payload: %v", err)
	}

	if connectPayload.UUID != testUUID {
		t.Errorf("UUID = %s, want %s", connectPayload.UUID, testUUID)
	}
}

// ============================================================================
// Public Connection Tests
// ============================================================================

func TestAcceptPublicConnections(t *testing.T) {
	env := NewTestEnvironment(t, nil)
	defer env.Stop()

	client := env.ConnectClient("")
	defer client.Close()

	publicConn, uuid := client.ConnectToPublicPort()
	defer publicConn.Close()

	if len(uuid) != 36 {
		t.Errorf("UUID length = %d, want 36", len(uuid))
	}

	// Verify pending stream was created
	time.Sleep(50 * time.Millisecond)
	env.Server.pendingMu.RLock()
	_, pendingExists := env.Server.pendingStreams[uuid]
	numPending := len(env.Server.pendingStreams)
	env.Server.pendingMu.RUnlock()

	if !pendingExists {
		t.Errorf("pending stream with UUID %s not found", uuid)
	}
	if numPending != 1 {
		t.Errorf("expected 1 pending stream, got %d", numPending)
	}
}

func TestAcceptMultiplePublicConnections(t *testing.T) {
	env := NewTestEnvironment(t, nil)
	defer env.Stop()

	client := env.ConnectClient("")
	defer client.Close()

	numConnections := 3
	publicConns := make([]net.Conn, numConnections)
	uuids := make([]string, numConnections)

	for i := 0; i < numConnections; i++ {
		publicConns[i], uuids[i] = client.ConnectToPublicPort()
		defer publicConns[i].Close()
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
	env.Server.pendingMu.RLock()
	numPending := len(env.Server.pendingStreams)
	env.Server.pendingMu.RUnlock()

	if numPending != numConnections {
		t.Errorf("expected %d pending streams, got %d", numConnections, numPending)
	}
}

func TestPublicListenerCleanupOnSessionRemoval(t *testing.T) {
	env := NewTestEnvironment(t, nil)
	defer env.Stop()

	client := env.ConnectClient("")
	publicPort := client.PublicPort

	// Verify public port is listening
	testConn, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", publicPort))
	if err != nil {
		t.Fatalf("public port not listening: %v", err)
	}
	testConn.Close()

	// Close control connection to trigger cleanup
	client.Close()

	// Give server time to cleanup
	time.Sleep(200 * time.Millisecond)

	// Verify public port is no longer listening
	_, err = net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", publicPort))
	if err == nil {
		t.Error("public port still listening after session cleanup")
	}
}

func TestPendingStreamTimeout(t *testing.T) {
	env := NewTestEnvironment(t, nil)
	defer env.Stop()

	client := env.ConnectClient("")
	defer client.Close()

	publicConn, uuid := client.ConnectToPublicPort()
	defer publicConn.Close()

	// Verify pending stream exists
	env.Server.pendingMu.RLock()
	_, exists := env.Server.pendingStreams[uuid]
	env.Server.pendingMu.RUnlock()

	if !exists {
		t.Fatal("pending stream should exist immediately after connect")
	}

	// Verify timeout is set
	env.Server.pendingMu.RLock()
	pending := env.Server.pendingStreams[uuid]
	hasTimeout := pending != nil && pending.timeout != nil
	env.Server.pendingMu.RUnlock()

	if !hasTimeout {
		t.Error("pending stream should have timeout set")
	}

	// Manually trigger cleanup to test it works
	env.Server.cleanupPendingStream(uuid)

	// Verify pending stream was removed
	env.Server.pendingMu.RLock()
	_, exists = env.Server.pendingStreams[uuid]
	env.Server.pendingMu.RUnlock()

	if exists {
		t.Error("pending stream should be removed after cleanup")
	}
}

// ============================================================================
// Connection Type Detection Tests
// ============================================================================

func TestConnectionTypeDetection(t *testing.T) {
	env := NewTestEnvironment(t, nil)
	defer env.Stop()

	t.Run("control connection with handshake message", func(t *testing.T) {
		client := env.ConnectClient("")
		defer client.Close()

		// Verify session was created
		time.Sleep(50 * time.Millisecond)
		env.Server.clientsMu.RLock()
		numClients := len(env.Server.clients)
		env.Server.clientsMu.RUnlock()

		if numClients < 1 {
			t.Errorf("expected at least 1 client session, got %d", numClients)
		}
	})

	t.Run("data connection with stream handshake message", func(t *testing.T) {
		conn, err := net.Dial("tcp", env.ServerAddr())
		if err != nil {
			t.Fatalf("failed to connect to server: %v", err)
		}
		defer conn.Close()

		// Send stream handshake with non-existent UUID
		streamHandshake := &protocol.StreamHandshakePayload{
			UUID: "550e8400-e29b-41d4-a716-446655440000",
		}
		streamHandshakeBytes, _ := protocol.EncodeStreamHandshake(streamHandshake)
		msg := &protocol.Message{
			Type:    protocol.MessageTypeStreamHandshake,
			Payload: streamHandshakeBytes,
		}
		protocol.WriteMessage(conn, msg)

		// Should receive StreamReject since UUID doesn't exist
		rejectMsg, err := protocol.ReadMessage(conn)
		if err != nil {
			t.Fatalf("failed to read reject message: %v", err)
		}

		if rejectMsg.Type != protocol.MessageTypeStreamReject {
			t.Errorf("expected MessageTypeStreamReject, got type %d", rejectMsg.Type)
		}
	})

	t.Run("invalid message type", func(t *testing.T) {
		conn, err := net.Dial("tcp", env.ServerAddr())
		if err != nil {
			t.Fatalf("failed to connect to server: %v", err)
		}
		defer conn.Close()

		invalidMsg := &protocol.Message{
			Type:    protocol.MessageTypeConnect,
			Payload: []byte("test"),
		}
		protocol.WriteMessage(conn, invalidMsg)

		time.Sleep(100 * time.Millisecond)

		_, err = protocol.ReadMessage(conn)
		if err == nil {
			t.Error("expected connection to be closed for invalid message type")
		}
	})
}

// ============================================================================
// Data Connection Handler Tests
// ============================================================================

func TestDataConnectionHandler(t *testing.T) {
	env := NewTestEnvironment(t, nil)
	defer env.Stop()

	client := env.ConnectClient("")
	defer client.Close()

	t.Run("successful stream handshake", func(t *testing.T) {
		publicConn, dataConn, uuid := client.EstablishStream()
		defer publicConn.Close()
		defer dataConn.Close()

		// Verify pending stream was removed
		time.Sleep(50 * time.Millisecond)
		env.Server.pendingMu.RLock()
		_, exists := env.Server.pendingStreams[uuid]
		env.Server.pendingMu.RUnlock()

		if exists {
			t.Error("pending stream should be removed after successful handshake")
		}

		// Verify stream was created in session
		env.Server.clientsMu.RLock()
		var session *ClientSession
		for _, s := range env.Server.clients {
			session = s
			break
		}
		env.Server.clientsMu.RUnlock()

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
		dataConn, err := net.Dial("tcp", env.ServerAddr())
		if err != nil {
			t.Fatalf("failed to connect for data connection: %v", err)
		}
		defer dataConn.Close()

		invalidUUID := "550e8400-e29b-41d4-a716-446655440000"
		streamHandshake := &protocol.StreamHandshakePayload{UUID: invalidUUID}
		streamHandshakeBytes, _ := protocol.EncodeStreamHandshake(streamHandshake)
		msg := &protocol.Message{
			Type:    protocol.MessageTypeStreamHandshake,
			Payload: streamHandshakeBytes,
		}
		protocol.WriteMessage(dataConn, msg)

		rejectMsg, err := protocol.ReadMessage(dataConn)
		if err != nil {
			t.Fatalf("failed to read stream reject message: %v", err)
		}

		if rejectMsg.Type != protocol.MessageTypeStreamReject {
			t.Errorf("expected MessageTypeStreamReject, got type %d", rejectMsg.Type)
		}
	})

	t.Run("stream handshake with malformed payload", func(t *testing.T) {
		dataConn, err := net.Dial("tcp", env.ServerAddr())
		if err != nil {
			t.Fatalf("failed to connect for data connection: %v", err)
		}
		defer dataConn.Close()

		msg := &protocol.Message{
			Type:    protocol.MessageTypeStreamHandshake,
			Payload: []byte("invalid-payload-too-short"),
		}
		protocol.WriteMessage(dataConn, msg)

		time.Sleep(100 * time.Millisecond)

		_, err = protocol.ReadMessage(dataConn)
		if err == nil {
			t.Error("expected connection to be closed for malformed payload")
		}
	})
}

// ============================================================================
// Bidirectional Proxy Tests
// ============================================================================

func TestBidirectionalProxy(t *testing.T) {
	env := NewTestEnvironment(t, nil)
	defer env.Stop()

	client := env.ConnectClient("")
	defer client.Close()

	t.Run("bidirectional data forwarding", func(t *testing.T) {
		publicConn, dataConn, _ := client.EstablishStream()
		defer publicConn.Close()
		defer dataConn.Close()

		// Give proxy time to start
		time.Sleep(200 * time.Millisecond)

		// Test data forwarding: external → client
		testData1 := []byte("Hello from external client!")
		errCh := make(chan error, 2)

		go func() {
			_, err := publicConn.Write(testData1)
			errCh <- err
		}()

		go func() {
			buffer := make([]byte, len(testData1))
			if _, err := io.ReadFull(dataConn, buffer); err != nil {
				errCh <- fmt.Errorf("failed to read: %v", err)
				return
			}
			if string(buffer) != string(testData1) {
				errCh <- fmt.Errorf("data mismatch: got %q, want %q", string(buffer), string(testData1))
				return
			}
			errCh <- nil
		}()

		for i := 0; i < 2; i++ {
			if err := <-errCh; err != nil {
				t.Fatal(err)
			}
		}

		// Test data forwarding: client → external
		testData2 := []byte("Hello from client!")

		go func() {
			_, err := dataConn.Write(testData2)
			errCh <- err
		}()

		go func() {
			buffer := make([]byte, len(testData2))
			if _, err := io.ReadFull(publicConn, buffer); err != nil {
				errCh <- fmt.Errorf("failed to read: %v", err)
				return
			}
			if string(buffer) != string(testData2) {
				errCh <- fmt.Errorf("data mismatch: got %q, want %q", string(buffer), string(testData2))
				return
			}
			errCh <- nil
		}()

		for i := 0; i < 2; i++ {
			if err := <-errCh; err != nil {
				t.Fatal(err)
			}
		}
	})

	t.Run("connection cleanup on close", func(t *testing.T) {
		publicConn, dataConn, uuid := client.EstablishStream()

		// Verify stream exists in session
		env.Server.clientsMu.RLock()
		var session *ClientSession
		for _, s := range env.Server.clients {
			session = s
			break
		}
		env.Server.clientsMu.RUnlock()

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
		_, err := publicConn.Read(make([]byte, 1))
		if err == nil {
			t.Error("public connection should be closed after cleanup")
		}
		publicConn.Close()
	})
}

func TestCleanupPendingStream(t *testing.T) {
	server := NewServer(0, "")
	pair := NewMockConnPair(t)
	defer pair.Close()

	testUUID := "test-uuid-12345678-1234-1234-1234"
	pending := &PendingStream{
		externalConn: pair.ServerConn,
		sessionID:    "test-session",
		createdAt:    time.Now(),
		timeout:      time.NewTimer(1 * time.Hour),
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
	_, err := pair.ClientConn.Read(make([]byte, 1))
	if err == nil {
		t.Error("external connection should be closed after cleanup")
	}

	// Cleanup again should not panic
	server.cleanupPendingStream(testUUID)
}

// ============================================================================
// Large Data and Concurrent Stream Tests
// ============================================================================

func TestBidirectionalProxyLargeData(t *testing.T) {
	env := NewTestEnvironment(t, nil)
	defer env.Stop()

	client := env.ConnectClient("")
	defer client.Close()

	publicConn, dataConn, _ := client.EstablishStream()
	defer publicConn.Close()
	defer dataConn.Close()

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
	_, err := io.ReadFull(dataConn, receivedData)
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

func TestBidirectionalProxyMultipleConcurrentStreams(t *testing.T) {
	env := NewTestEnvironment(t, nil)
	defer env.Stop()

	client := env.ConnectClient("")
	defer client.Close()

	numStreams := 3
	type streamPair struct {
		publicConn net.Conn
		dataConn   net.Conn
		uuid       string
	}
	streams := make([]streamPair, numStreams)

	// Create multiple streams
	for i := 0; i < numStreams; i++ {
		publicConn, dataConn, uuid := client.EstablishStream()
		streams[i] = streamPair{
			publicConn: publicConn,
			dataConn:   dataConn,
			uuid:       uuid,
		}
	}

	// Test data isolation - send unique data on each stream
	var wg sync.WaitGroup
	testErrCh := make(chan error, numStreams*2)

	for i, stream := range streams {
		i := i
		stream := stream
		wg.Add(2)

		// Send unique data from external to client
		go func() {
			defer wg.Done()
			testData := []byte(fmt.Sprintf("Stream %d data from external", i))
			_, err := stream.publicConn.Write(testData)
			if err != nil {
				testErrCh <- err
			}
		}()

		// Receive and verify on data connection
		go func() {
			defer wg.Done()
			expectedData := []byte(fmt.Sprintf("Stream %d data from external", i))
			buffer := make([]byte, len(expectedData))
			_, err := io.ReadFull(stream.dataConn, buffer)
			if err != nil {
				testErrCh <- fmt.Errorf("stream %d: read error: %v", i, err)
				return
			}
			if string(buffer) != string(expectedData) {
				testErrCh <- fmt.Errorf("stream %d: data mismatch: got %q, want %q", i, string(buffer), string(expectedData))
			}
		}()
	}

	wg.Wait()
	close(testErrCh)

	for err := range testErrCh {
		if err != nil {
			t.Error(err)
		}
	}

	// Cleanup
	for _, stream := range streams {
		stream.publicConn.Close()
		stream.dataConn.Close()
	}
}

func TestBidirectionalProxyExternalCloses(t *testing.T) {
	env := NewTestEnvironment(t, nil)
	defer env.Stop()

	client := env.ConnectClient("")
	defer client.Close()

	publicConn, dataConn, _ := client.EstablishStream()

	// Close external connection first
	publicConn.Close()

	// Data connection should also close
	time.Sleep(200 * time.Millisecond)
	_, err := dataConn.Read(make([]byte, 1))
	if err == nil {
		t.Error("data connection should be closed when external closes")
	}
	dataConn.Close()
}
