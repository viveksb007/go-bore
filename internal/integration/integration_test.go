// Package integration contains integration tests for the bore tunneling system.
// These tests verify end-to-end functionality by starting actual servers and clients.
package integration

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/viveksb007/go-bore/internal/client"
	"github.com/viveksb007/go-bore/internal/server"
)

// IntegrationEnv provides a reusable test environment for integration tests.
type IntegrationEnv struct {
	Server     *server.Server
	t          *testing.T
	serverDone chan error
}

// IntegrationEnvConfig holds configuration for the integration test environment.
type IntegrationEnvConfig struct {
	Port   int
	Secret string
}

// NewIntegrationEnv creates and starts a new integration test environment.
func NewIntegrationEnv(t *testing.T, cfg *IntegrationEnvConfig) *IntegrationEnv {
	t.Helper()

	if cfg == nil {
		cfg = &IntegrationEnvConfig{Port: 8081}
	}
	if cfg.Port == 0 {
		cfg.Port = 8081
	}

	srv := server.NewServer(cfg.Port, cfg.Secret)
	env := &IntegrationEnv{
		Server:     srv,
		t:          t,
		serverDone: make(chan error, 1),
	}

	go func() {
		env.serverDone <- srv.Run()
	}()

	time.Sleep(100 * time.Millisecond)

	if srv.Addr() == "" {
		t.Fatal("Server failed to start")
	}

	return env
}

// Stop shuts down the integration test environment.
func (env *IntegrationEnv) Stop() {
	env.Server.Close()
}

// ServerAddr returns the server's listen address.
func (env *IntegrationEnv) ServerAddr() string {
	return env.Server.Addr()
}

// ConnectClient creates and connects a bore client to the server.
func (env *IntegrationEnv) ConnectClient(localPort int, secret string) *ConnectedBoreClient {
	env.t.Helper()

	c := client.NewClient(env.ServerAddr(), localPort, "127.0.0.1", secret)
	if err := c.Connect(); err != nil {
		env.t.Fatalf("Client failed to connect: %v", err)
	}

	clientDone := make(chan error, 1)
	go func() {
		clientDone <- c.Run()
	}()

	time.Sleep(100 * time.Millisecond)

	return &ConnectedBoreClient{
		Client:     c,
		clientDone: clientDone,
		env:        env,
	}
}

// ConnectedBoreClient represents a connected bore client.
type ConnectedBoreClient struct {
	Client     *client.Client
	clientDone chan error
	env        *IntegrationEnv
}

// Close closes the bore client.
func (c *ConnectedBoreClient) Close() {
	c.Client.Close()
}

// PublicPort returns the client's public port.
func (c *ConnectedBoreClient) PublicPort() uint16 {
	return c.Client.PublicPort()
}

// ConnectThroughTunnel connects to the public port and returns the connection.
func (c *ConnectedBoreClient) ConnectThroughTunnel() net.Conn {
	c.env.t.Helper()

	conn, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", c.PublicPort()))
	if err != nil {
		c.env.t.Fatalf("Failed to connect through tunnel: %v", err)
	}
	return conn
}

// LocalEchoServer represents a local TCP echo server for testing.
type LocalEchoServer struct {
	Listener net.Listener
	Port     int
}

// NewLocalEchoServer creates and starts a local echo server.
func NewLocalEchoServer(t *testing.T) *LocalEchoServer {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to create local listener: %v", err)
	}

	srv := &LocalEchoServer{
		Listener: listener,
		Port:     listener.Addr().(*net.TCPAddr).Port,
	}

	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				io.Copy(c, c)
			}(conn)
		}
	}()

	return srv
}

// Close closes the echo server.
func (s *LocalEchoServer) Close() {
	s.Listener.Close()
}

// LocalHTTPServer represents a local HTTP server for testing.
type LocalHTTPServer struct {
	Server   *http.Server
	Listener net.Listener
	Port     int
}

// NewLocalHTTPServer creates and starts a local HTTP server.
func NewLocalHTTPServer(t *testing.T, handler http.Handler) *LocalHTTPServer {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to create local listener: %v", err)
	}

	srv := &LocalHTTPServer{
		Server:   &http.Server{Handler: handler},
		Listener: listener,
		Port:     listener.Addr().(*net.TCPAddr).Port,
	}

	go srv.Server.Serve(listener)

	return srv
}

// Close closes the HTTP server.
func (s *LocalHTTPServer) Close() {
	s.Server.Close()
}

// LocalCustomServer represents a local server with custom connection handling.
type LocalCustomServer struct {
	Listener net.Listener
	Port     int
}

// NewLocalCustomServer creates a local server with custom handler.
func NewLocalCustomServer(t *testing.T, handler func(net.Conn)) *LocalCustomServer {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to create local listener: %v", err)
	}

	srv := &LocalCustomServer{
		Listener: listener,
		Port:     listener.Addr().(*net.TCPAddr).Port,
	}

	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go handler(conn)
		}
	}()

	return srv
}

// Close closes the custom server.
func (s *LocalCustomServer) Close() {
	s.Listener.Close()
}

// containsString checks if s contains substr.
func containsString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// ============================================================================
// End-to-End Tests
// ============================================================================

// TestEndToEndHTTPTunneling tests complete HTTP tunneling through bore.
func TestEndToEndHTTPTunneling(t *testing.T) {
	localHTTP := NewLocalHTTPServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "Hello from local server! Path: %s", r.URL.Path)
	}))
	defer localHTTP.Close()

	env := NewIntegrationEnv(t, nil)
	defer env.Stop()

	client := env.ConnectClient(localHTTP.Port, "")
	defer client.Close()

	if client.PublicPort() == 0 {
		t.Fatal("Public port is 0")
	}

	// Make HTTP request through tunnel
	tunnelURL := fmt.Sprintf("http://127.0.0.1:%d/test-path", client.PublicPort())
	resp, err := http.Get(tunnelURL)
	if err != nil {
		t.Fatalf("HTTP request through tunnel failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	expectedBody := "Hello from local server! Path: /test-path"
	if string(body) != expectedBody {
		t.Errorf("Response body mismatch.\nExpected: %s\nGot: %s", expectedBody, string(body))
	}

	// Verify direct connection gives same result
	directURL := fmt.Sprintf("http://127.0.0.1:%d/test-path", localHTTP.Port)
	directResp, _ := http.Get(directURL)
	defer directResp.Body.Close()
	directBody, _ := io.ReadAll(directResp.Body)

	if string(body) != string(directBody) {
		t.Errorf("Tunnel response differs from direct response")
	}
}

// TestEndToEndTCPDataIntegrity tests that data is preserved exactly through the tunnel.
func TestEndToEndTCPDataIntegrity(t *testing.T) {
	localEcho := NewLocalEchoServer(t)
	defer localEcho.Close()

	env := NewIntegrationEnv(t, nil)
	defer env.Stop()

	client := env.ConnectClient(localEcho.Port, "")
	defer client.Close()

	conn := client.ConnectThroughTunnel()
	defer conn.Close()

	// Test various data patterns
	testCases := [][]byte{
		[]byte("Hello, World!"),
		[]byte("Binary data: \x00\x01\x02\x03\xff\xfe\xfd"),
		make([]byte, 1024), // 1KB of zeros
		make([]byte, 8192), // 8KB of data
	}

	// Fill larger test case with pattern
	for i := range testCases[3] {
		testCases[3][i] = byte(i % 256)
	}

	for i, testData := range testCases {
		_, err := conn.Write(testData)
		if err != nil {
			t.Fatalf("Test case %d: Failed to write: %v", i, err)
		}

		response := make([]byte, len(testData))
		conn.SetReadDeadline(time.Now().Add(5 * time.Second))
		_, err = io.ReadFull(conn, response)
		if err != nil {
			t.Fatalf("Test case %d: Failed to read: %v", i, err)
		}

		for j := range testData {
			if testData[j] != response[j] {
				t.Errorf("Test case %d: Data mismatch at byte %d", i, j)
				break
			}
		}
	}
}

// TestEndToEndBidirectionalData tests bidirectional data flow.
func TestEndToEndBidirectionalData(t *testing.T) {
	serverReceived := make(chan []byte, 1)
	serverToSend := []byte("Server says hello!")

	localSrv := NewLocalCustomServer(t, func(conn net.Conn) {
		defer conn.Close()
		conn.Write(serverToSend)
		buf := make([]byte, 1024)
		n, _ := conn.Read(buf)
		serverReceived <- buf[:n]
	})
	defer localSrv.Close()

	env := NewIntegrationEnv(t, nil)
	defer env.Stop()

	client := env.ConnectClient(localSrv.Port, "")
	defer client.Close()

	conn := client.ConnectThroughTunnel()
	defer conn.Close()

	// Read data from server
	buf := make([]byte, 1024)
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	n, err := conn.Read(buf)
	if err != nil {
		t.Fatalf("Failed to read from tunnel: %v", err)
	}

	if string(buf[:n]) != string(serverToSend) {
		t.Errorf("Server→Client data mismatch. Expected: %s, Got: %s", serverToSend, buf[:n])
	}

	// Send data to server
	clientToSend := []byte("Client says hi!")
	conn.Write(clientToSend)

	select {
	case received := <-serverReceived:
		if string(received) != string(clientToSend) {
			t.Errorf("Client→Server data mismatch. Expected: %s, Got: %s", clientToSend, received)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Timeout waiting for server to receive data")
	}
}

// ============================================================================
// Concurrent Connection Tests
// ============================================================================

// TestMultipleConcurrentConnections tests that multiple external connections work simultaneously.
func TestMultipleConcurrentConnections(t *testing.T) {
	var connCounter int
	var counterMu sync.Mutex

	localSrv := NewLocalCustomServer(t, func(conn net.Conn) {
		defer conn.Close()
		counterMu.Lock()
		connCounter++
		connID := connCounter
		counterMu.Unlock()

		buf := make([]byte, 1024)
		for {
			n, err := conn.Read(buf)
			if err != nil {
				return
			}
			response := fmt.Sprintf("conn%d:%s", connID, buf[:n])
			conn.Write([]byte(response))
		}
	})
	defer localSrv.Close()

	env := NewIntegrationEnv(t, nil)
	defer env.Stop()

	client := env.ConnectClient(localSrv.Port, "")
	defer client.Close()

	numConnections := 5
	var wg sync.WaitGroup
	errors := make(chan error, numConnections)
	results := make(chan string, numConnections)

	for i := 0; i < numConnections; i++ {
		wg.Add(1)
		go func(connNum int) {
			defer wg.Done()

			conn, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", client.PublicPort()))
			if err != nil {
				errors <- fmt.Errorf("connection %d: failed to connect: %v", connNum, err)
				return
			}
			defer conn.Close()

			msg := fmt.Sprintf("hello%d", connNum)
			conn.Write([]byte(msg))

			buf := make([]byte, 1024)
			conn.SetReadDeadline(time.Now().Add(5 * time.Second))
			n, err := conn.Read(buf)
			if err != nil {
				errors <- fmt.Errorf("connection %d: failed to read: %v", connNum, err)
				return
			}

			results <- string(buf[:n])
		}(i)
	}

	wg.Wait()
	close(errors)
	close(results)

	for err := range errors {
		t.Error(err)
	}

	responseCount := 0
	for result := range results {
		responseCount++
		if len(result) < 5 || result[:4] != "conn" {
			t.Errorf("Unexpected response format: %s", result)
		}
	}

	if responseCount != numConnections {
		t.Errorf("Expected %d responses, got %d", numConnections, responseCount)
	}
}

// TestConcurrentConnectionsDataIsolation tests that data doesn't leak between streams.
func TestConcurrentConnectionsDataIsolation(t *testing.T) {
	var connCounter int
	var counterMu sync.Mutex

	localSrv := NewLocalCustomServer(t, func(conn net.Conn) {
		defer conn.Close()
		counterMu.Lock()
		connCounter++
		connID := connCounter
		counterMu.Unlock()

		buf := make([]byte, 1024)
		n, err := conn.Read(buf)
		if err != nil {
			return
		}
		clientID := string(buf[:n])
		response := fmt.Sprintf("server_conn_%d_client_%s", connID, clientID)
		conn.Write([]byte(response))
	})
	defer localSrv.Close()

	env := NewIntegrationEnv(t, nil)
	defer env.Stop()

	client := env.ConnectClient(localSrv.Port, "")
	defer client.Close()

	numConnections := 10
	var wg sync.WaitGroup
	responses := make(map[string]string)
	var responsesMu sync.Mutex

	for i := 0; i < numConnections; i++ {
		wg.Add(1)
		go func(connNum int) {
			defer wg.Done()

			conn, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", client.PublicPort()))
			if err != nil {
				t.Errorf("Connection %d: failed to connect: %v", connNum, err)
				return
			}
			defer conn.Close()

			clientID := fmt.Sprintf("client_%d", connNum)
			conn.Write([]byte(clientID))

			buf := make([]byte, 1024)
			conn.SetReadDeadline(time.Now().Add(5 * time.Second))
			n, err := conn.Read(buf)
			if err != nil {
				t.Errorf("Connection %d: failed to read: %v", connNum, err)
				return
			}

			responsesMu.Lock()
			responses[clientID] = string(buf[:n])
			responsesMu.Unlock()
		}(i)
	}

	wg.Wait()

	for clientID, response := range responses {
		if !containsString(response, clientID) {
			t.Errorf("Data isolation failure: client %s got response %s", clientID, response)
		}
	}

	if len(responses) != numConnections {
		t.Errorf("Expected %d responses, got %d", numConnections, len(responses))
	}
}

// TestConcurrentConnectionsWithLargeData tests concurrent connections with larger data transfers.
func TestConcurrentConnectionsWithLargeData(t *testing.T) {
	localEcho := NewLocalEchoServer(t)
	defer localEcho.Close()

	env := NewIntegrationEnv(t, nil)
	defer env.Stop()

	client := env.ConnectClient(localEcho.Port, "")
	defer client.Close()

	numConnections := 3
	dataSize := 64 * 1024 // 64KB per connection
	var wg sync.WaitGroup
	errors := make(chan error, numConnections)

	for i := 0; i < numConnections; i++ {
		wg.Add(1)
		go func(connNum int) {
			defer wg.Done()

			conn, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", client.PublicPort()))
			if err != nil {
				errors <- fmt.Errorf("connection %d: failed to connect: %v", connNum, err)
				return
			}
			defer conn.Close()

			data := make([]byte, dataSize)
			for j := range data {
				data[j] = byte((connNum*256 + j) % 256)
			}

			_, err = conn.Write(data)
			if err != nil {
				errors <- fmt.Errorf("connection %d: failed to write: %v", connNum, err)
				return
			}

			response := make([]byte, dataSize)
			conn.SetReadDeadline(time.Now().Add(10 * time.Second))
			_, err = io.ReadFull(conn, response)
			if err != nil {
				errors <- fmt.Errorf("connection %d: failed to read: %v", connNum, err)
				return
			}

			for j := range data {
				if data[j] != response[j] {
					errors <- fmt.Errorf("connection %d: data mismatch at byte %d", connNum, j)
					return
				}
			}
		}(i)
	}

	wg.Wait()
	close(errors)

	for err := range errors {
		t.Error(err)
	}
}

// ============================================================================
// Shutdown and Disconnection Tests
// ============================================================================

// TestClientGracefulShutdown tests that client shuts down cleanly.
func TestClientGracefulShutdown(t *testing.T) {
	localEcho := NewLocalEchoServer(t)
	defer localEcho.Close()

	env := NewIntegrationEnv(t, nil)
	defer env.Stop()

	boreClient := client.NewClient(env.ServerAddr(), localEcho.Port, "127.0.0.1", "")
	if err := boreClient.Connect(); err != nil {
		t.Fatalf("Client failed to connect: %v", err)
	}

	clientDone := make(chan error, 1)
	go func() {
		clientDone <- boreClient.Run()
	}()
	time.Sleep(100 * time.Millisecond)

	publicPort := boreClient.PublicPort()

	// Create an active connection through the tunnel
	externalConn, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", publicPort))
	if err != nil {
		t.Fatalf("Failed to connect through tunnel: %v", err)
	}

	externalConn.Write([]byte("hello"))
	buf := make([]byte, 5)
	externalConn.SetReadDeadline(time.Now().Add(5 * time.Second))
	externalConn.Read(buf)

	// Close the client gracefully
	boreClient.Close()

	select {
	case <-clientDone:
		// Expected
	case <-time.After(5 * time.Second):
		t.Fatal("Client did not shut down in time")
	}

	// Verify external connection is closed
	externalConn.SetReadDeadline(time.Now().Add(1 * time.Second))
	_, err = externalConn.Read(buf)
	if err == nil {
		t.Error("External connection should be closed after client shutdown")
	}
	externalConn.Close()
}

// TestServerGracefulShutdown tests that server shuts down cleanly.
func TestServerGracefulShutdown(t *testing.T) {
	localEcho := NewLocalEchoServer(t)
	defer localEcho.Close()

	boreServer := server.NewServer(8081, "")
	serverDone := make(chan error, 1)
	go func() {
		serverDone <- boreServer.Run()
	}()
	time.Sleep(100 * time.Millisecond)

	serverAddr := boreServer.Addr()

	boreClient := client.NewClient(serverAddr, localEcho.Port, "127.0.0.1", "")
	if err := boreClient.Connect(); err != nil {
		t.Fatalf("Client failed to connect: %v", err)
	}
	defer boreClient.Close()

	go boreClient.Run()
	time.Sleep(100 * time.Millisecond)

	publicPort := boreClient.PublicPort()

	externalConn, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", publicPort))
	if err != nil {
		t.Fatalf("Failed to connect through tunnel: %v", err)
	}
	defer externalConn.Close()

	externalConn.Write([]byte("hello"))
	buf := make([]byte, 5)
	externalConn.SetReadDeadline(time.Now().Add(5 * time.Second))
	externalConn.Read(buf)

	// Close the server gracefully
	boreServer.Close()

	select {
	case <-serverDone:
		// Expected
	case <-time.After(5 * time.Second):
		t.Fatal("Server did not shut down in time")
	}

	// Verify we can't connect to the server anymore
	_, err = net.DialTimeout("tcp", serverAddr, 1*time.Second)
	if err == nil {
		t.Error("Should not be able to connect to server after shutdown")
	}
}

// TestAbruptClientDisconnection tests handling of sudden client disconnection.
func TestAbruptClientDisconnection(t *testing.T) {
	localConnClosed := make(chan struct{}, 1)
	localSrv := NewLocalCustomServer(t, func(conn net.Conn) {
		defer conn.Close()
		buf := make([]byte, 1024)
		for {
			_, err := conn.Read(buf)
			if err != nil {
				localConnClosed <- struct{}{}
				return
			}
		}
	})
	defer localSrv.Close()

	env := NewIntegrationEnv(t, nil)
	defer env.Stop()

	boreClient := client.NewClient(env.ServerAddr(), localSrv.Port, "127.0.0.1", "")
	if err := boreClient.Connect(); err != nil {
		t.Fatalf("Client failed to connect: %v", err)
	}

	go boreClient.Run()
	time.Sleep(100 * time.Millisecond)

	publicPort := boreClient.PublicPort()

	externalConn, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", publicPort))
	if err != nil {
		t.Fatalf("Failed to connect through tunnel: %v", err)
	}

	externalConn.Write([]byte("hello"))
	time.Sleep(100 * time.Millisecond)

	// Abruptly close the client
	boreClient.Close()

	select {
	case <-localConnClosed:
		// Expected
	case <-time.After(5 * time.Second):
		t.Error("Local connection was not closed after client disconnection")
	}

	externalConn.Close()
}

// TestAbruptExternalDisconnection tests handling of sudden external client disconnection.
func TestAbruptExternalDisconnection(t *testing.T) {
	localConnClosed := make(chan struct{}, 1)
	localSrv := NewLocalCustomServer(t, func(conn net.Conn) {
		defer conn.Close()
		buf := make([]byte, 1024)
		for {
			_, err := conn.Read(buf)
			if err != nil {
				localConnClosed <- struct{}{}
				return
			}
		}
	})
	defer localSrv.Close()

	env := NewIntegrationEnv(t, nil)
	defer env.Stop()

	client := env.ConnectClient(localSrv.Port, "")
	defer client.Close()

	externalConn, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", client.PublicPort()))
	if err != nil {
		t.Fatalf("Failed to connect through tunnel: %v", err)
	}

	externalConn.Write([]byte("hello"))
	time.Sleep(100 * time.Millisecond)

	// Abruptly close the external connection
	externalConn.Close()

	select {
	case <-localConnClosed:
		// Expected
	case <-time.After(5 * time.Second):
		t.Error("Local connection was not closed after external disconnection")
	}
}

// TestConnectionClosePropagatesToOtherEnd tests that closing one end propagates to the other.
func TestConnectionClosePropagatesToOtherEnd(t *testing.T) {
	localSrv := NewLocalCustomServer(t, func(conn net.Conn) {
		buf := make([]byte, 1024)
		conn.Read(buf)
		conn.Write([]byte("goodbye"))
		conn.Close()
	})
	defer localSrv.Close()

	env := NewIntegrationEnv(t, nil)
	defer env.Stop()

	client := env.ConnectClient(localSrv.Port, "")
	defer client.Close()

	externalConn := client.ConnectThroughTunnel()
	defer externalConn.Close()

	externalConn.Write([]byte("hello"))

	buf := make([]byte, 1024)
	externalConn.SetReadDeadline(time.Now().Add(5 * time.Second))
	n, err := externalConn.Read(buf)
	if err != nil {
		t.Fatalf("Failed to read response: %v", err)
	}

	if string(buf[:n]) != "goodbye" {
		t.Errorf("Expected 'goodbye', got '%s'", buf[:n])
	}

	// Try to read again - should get EOF since local server closed
	externalConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, err = externalConn.Read(buf)
	if err == nil {
		t.Error("Expected EOF after local server closed")
	}
}

// ============================================================================
// Authentication Tests
// ============================================================================

// TestAuthenticationWithMatchingSecrets tests successful authentication.
func TestAuthenticationWithMatchingSecrets(t *testing.T) {
	localEcho := NewLocalEchoServer(t)
	defer localEcho.Close()

	secret := "my-super-secret-token-123"
	env := NewIntegrationEnv(t, &IntegrationEnvConfig{Port: 8081, Secret: secret})
	defer env.Stop()

	client := env.ConnectClient(localEcho.Port, secret)
	defer client.Close()

	if client.PublicPort() == 0 {
		t.Fatal("Public port should be allocated after successful authentication")
	}

	conn := client.ConnectThroughTunnel()
	defer conn.Close()

	testData := []byte("authenticated tunnel test")
	conn.Write(testData)

	buf := make([]byte, len(testData))
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	_, err := io.ReadFull(conn, buf)
	if err != nil {
		t.Fatalf("Failed to read through tunnel: %v", err)
	}

	if string(buf) != string(testData) {
		t.Errorf("Data mismatch. Expected: %s, Got: %s", testData, buf)
	}
}

// TestAuthenticationWithWrongSecret tests failed authentication.
func TestAuthenticationWithWrongSecret(t *testing.T) {
	serverSecret := "correct-secret"
	env := NewIntegrationEnv(t, &IntegrationEnvConfig{Port: 8081, Secret: serverSecret})
	defer env.Stop()

	wrongSecret := "wrong-secret"
	boreClient := client.NewClient(env.ServerAddr(), 8080, "127.0.0.1", wrongSecret)
	err := boreClient.Connect()

	if err == nil {
		boreClient.Close()
		t.Fatal("Client with wrong secret should fail to connect")
	}

	if !containsString(err.Error(), "authentication") && !containsString(err.Error(), "failed") {
		t.Errorf("Error should indicate authentication failure, got: %v", err)
	}
}

// TestAuthenticationWithEmptySecretAgainstProtectedServer tests that empty secret fails.
func TestAuthenticationWithEmptySecretAgainstProtectedServer(t *testing.T) {
	serverSecret := "server-requires-auth"
	env := NewIntegrationEnv(t, &IntegrationEnvConfig{Port: 8081, Secret: serverSecret})
	defer env.Stop()

	boreClient := client.NewClient(env.ServerAddr(), 8080, "127.0.0.1", "")
	err := boreClient.Connect()

	if err == nil {
		boreClient.Close()
		t.Fatal("Client without secret should fail to connect to protected server")
	}
}

// TestOpenModeNoSecret tests that server without secret accepts all connections.
func TestOpenModeNoSecret(t *testing.T) {
	localEcho := NewLocalEchoServer(t)
	defer localEcho.Close()

	env := NewIntegrationEnv(t, nil) // No secret = open mode
	defer env.Stop()

	client := env.ConnectClient(localEcho.Port, "")
	defer client.Close()

	conn := client.ConnectThroughTunnel()
	defer conn.Close()

	testData := []byte("open mode test")
	conn.Write(testData)

	buf := make([]byte, len(testData))
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	io.ReadFull(conn, buf)

	if string(buf) != string(testData) {
		t.Errorf("Data mismatch in open mode")
	}
}

// TestOpenModeAcceptsClientWithSecret tests that open server accepts clients that send a secret.
func TestOpenModeAcceptsClientWithSecret(t *testing.T) {
	localEcho := NewLocalEchoServer(t)
	defer localEcho.Close()

	env := NewIntegrationEnv(t, nil) // No secret = open mode
	defer env.Stop()

	// Connect WITH a secret (server should still accept since it's in open mode)
	client := env.ConnectClient(localEcho.Port, "some-secret")
	defer client.Close()

	conn := client.ConnectThroughTunnel()
	defer conn.Close()

	testData := []byte("open mode with client secret")
	conn.Write(testData)

	buf := make([]byte, len(testData))
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	io.ReadFull(conn, buf)

	if string(buf) != string(testData) {
		t.Errorf("Data mismatch")
	}
}

// TestMultipleClientsWithSameSecret tests that multiple clients can connect with the same secret.
func TestMultipleClientsWithSameSecret(t *testing.T) {
	// Start local echo servers
	localServers := make([]*LocalEchoServer, 3)
	for i := 0; i < 3; i++ {
		localServers[i] = NewLocalEchoServer(t)
		defer localServers[i].Close()
	}

	secret := "shared-secret"
	env := NewIntegrationEnv(t, &IntegrationEnvConfig{Port: 8081, Secret: secret})
	defer env.Stop()

	// Connect multiple clients with the same secret
	clients := make([]*ConnectedBoreClient, 3)
	for i := 0; i < 3; i++ {
		clients[i] = env.ConnectClient(localServers[i].Port, secret)
		defer clients[i].Close()
	}

	// Verify all clients got different public ports
	ports := make(map[uint16]bool)
	for i, c := range clients {
		port := c.PublicPort()
		if port == 0 {
			t.Errorf("Client %d got port 0", i)
		}
		if ports[port] {
			t.Errorf("Client %d got duplicate port %d", i, port)
		}
		ports[port] = true
	}

	// Verify all tunnels work
	for i, c := range clients {
		conn := c.ConnectThroughTunnel()

		testData := []byte(fmt.Sprintf("client%d", i))
		conn.Write(testData)

		buf := make([]byte, len(testData))
		conn.SetReadDeadline(time.Now().Add(5 * time.Second))
		io.ReadFull(conn, buf)

		if string(buf) != string(testData) {
			t.Errorf("Client %d tunnel data mismatch", i)
		}
		conn.Close()
	}
}
