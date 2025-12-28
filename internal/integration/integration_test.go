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

// TestEndToEndHTTPTunneling tests complete HTTP tunneling through bore.
// Requirements: 3.1, 3.2, 4.3
func TestEndToEndHTTPTunneling(t *testing.T) {
	// Step 1: Start a local HTTP test server
	localServer := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/plain")
			w.WriteHeader(http.StatusOK)
			fmt.Fprintf(w, "Hello from local server! Path: %s", r.URL.Path)
		}),
	}

	localListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to create local listener: %v", err)
	}
	localPort := localListener.Addr().(*net.TCPAddr).Port

	go localServer.Serve(localListener)
	defer localServer.Close()

	// Step 2: Start bore server
	boreServer := server.NewServer(0, "")
	serverErrCh := make(chan error, 1)
	go func() {
		serverErrCh <- boreServer.Run()
	}()
	defer boreServer.Close()

	// Wait for server to start
	time.Sleep(100 * time.Millisecond)

	serverAddr := boreServer.Addr()
	if serverAddr == "" {
		t.Fatal("Server address is empty")
	}

	// Step 3: Start bore client
	boreClient := client.NewClient(serverAddr, localPort, "127.0.0.1", "")
	if err := boreClient.Connect(); err != nil {
		t.Fatalf("Client failed to connect: %v", err)
	}
	defer boreClient.Close()

	// Start client message loop in goroutine
	clientErrCh := make(chan error, 1)
	go func() {
		clientErrCh <- boreClient.Run()
	}()

	publicPort := boreClient.PublicPort()
	if publicPort == 0 {
		t.Fatal("Public port is 0")
	}

	// Give time for everything to be ready
	time.Sleep(100 * time.Millisecond)

	// Step 4: Make HTTP request through tunnel
	tunnelURL := fmt.Sprintf("http://127.0.0.1:%d/test-path", publicPort)
	resp, err := http.Get(tunnelURL)
	if err != nil {
		t.Fatalf("HTTP request through tunnel failed: %v", err)
	}
	defer resp.Body.Close()

	// Step 5: Verify response
	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("Failed to read response body: %v", err)
	}

	expectedBody := "Hello from local server! Path: /test-path"
	if string(body) != expectedBody {
		t.Errorf("Response body mismatch.\nExpected: %s\nGot: %s", expectedBody, string(body))
	}

	// Verify direct connection gives same result
	directURL := fmt.Sprintf("http://127.0.0.1:%d/test-path", localPort)
	directResp, err := http.Get(directURL)
	if err != nil {
		t.Fatalf("Direct HTTP request failed: %v", err)
	}
	defer directResp.Body.Close()

	directBody, _ := io.ReadAll(directResp.Body)
	if string(body) != string(directBody) {
		t.Errorf("Tunnel response differs from direct response.\nTunnel: %s\nDirect: %s", body, directBody)
	}
}

// TestEndToEndTCPDataIntegrity tests that data is preserved exactly through the tunnel.
// Requirements: 3.1, 3.2
func TestEndToEndTCPDataIntegrity(t *testing.T) {
	// Start a local TCP echo server
	localListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to create local listener: %v", err)
	}
	defer localListener.Close()
	localPort := localListener.Addr().(*net.TCPAddr).Port

	// Echo server goroutine
	go func() {
		for {
			conn, err := localListener.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				io.Copy(c, c) // Echo back everything
			}(conn)
		}
	}()

	// Start bore server
	boreServer := server.NewServer(0, "")
	go boreServer.Run()
	defer boreServer.Close()
	time.Sleep(100 * time.Millisecond)

	// Start bore client
	boreClient := client.NewClient(boreServer.Addr(), localPort, "127.0.0.1", "")
	if err := boreClient.Connect(); err != nil {
		t.Fatalf("Client failed to connect: %v", err)
	}
	defer boreClient.Close()

	go boreClient.Run()
	time.Sleep(100 * time.Millisecond)

	publicPort := boreClient.PublicPort()

	// Connect through tunnel and test data integrity
	conn, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", publicPort))
	if err != nil {
		t.Fatalf("Failed to connect through tunnel: %v", err)
	}
	defer conn.Close()

	// Test various data patterns
	testCases := [][]byte{
		[]byte("Hello, World!"),
		[]byte("Binary data: \x00\x01\x02\x03\xff\xfe\xfd"),
		make([]byte, 1024), // 1KB of zeros
		make([]byte, 8192), // 8KB of data
	}

	// Fill larger test cases with pattern
	for i := range testCases[3] {
		testCases[3][i] = byte(i % 256)
	}

	for i, testData := range testCases {
		// Send data
		_, err := conn.Write(testData)
		if err != nil {
			t.Fatalf("Test case %d: Failed to write: %v", i, err)
		}

		// Read response
		response := make([]byte, len(testData))
		conn.SetReadDeadline(time.Now().Add(5 * time.Second))
		_, err = io.ReadFull(conn, response)
		if err != nil {
			t.Fatalf("Test case %d: Failed to read: %v", i, err)
		}

		// Verify data integrity
		for j := range testData {
			if testData[j] != response[j] {
				t.Errorf("Test case %d: Data mismatch at byte %d. Expected %d, got %d",
					i, j, testData[j], response[j])
				break
			}
		}
	}
}

// TestEndToEndBidirectionalData tests bidirectional data flow.
// Requirements: 3.1, 3.2, 4.3
func TestEndToEndBidirectionalData(t *testing.T) {
	// Start a local server that sends data and receives response
	localListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to create local listener: %v", err)
	}
	defer localListener.Close()
	localPort := localListener.Addr().(*net.TCPAddr).Port

	serverReceived := make(chan []byte, 1)
	serverToSend := []byte("Server says hello!")

	go func() {
		conn, err := localListener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()

		// Send data to client
		conn.Write(serverToSend)

		// Read data from client
		buf := make([]byte, 1024)
		n, _ := conn.Read(buf)
		serverReceived <- buf[:n]
	}()

	// Start bore server and client
	boreServer := server.NewServer(0, "")
	go boreServer.Run()
	defer boreServer.Close()
	time.Sleep(100 * time.Millisecond)

	boreClient := client.NewClient(boreServer.Addr(), localPort, "127.0.0.1", "")
	if err := boreClient.Connect(); err != nil {
		t.Fatalf("Client failed to connect: %v", err)
	}
	defer boreClient.Close()

	go boreClient.Run()
	time.Sleep(100 * time.Millisecond)

	// Connect through tunnel
	conn, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", boreClient.PublicPort()))
	if err != nil {
		t.Fatalf("Failed to connect through tunnel: %v", err)
	}
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

	// Verify server received it
	select {
	case received := <-serverReceived:
		if string(received) != string(clientToSend) {
			t.Errorf("Client→Server data mismatch. Expected: %s, Got: %s", clientToSend, received)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Timeout waiting for server to receive data")
	}
}

// TestMultipleConcurrentConnections tests that multiple external connections work simultaneously.
// Requirements: 4.1, 4.2
func TestMultipleConcurrentConnections(t *testing.T) {
	// Start a local server that handles multiple connections
	localListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to create local listener: %v", err)
	}
	defer localListener.Close()
	localPort := localListener.Addr().(*net.TCPAddr).Port

	// Local server that echoes with connection ID
	var connCounter int
	var counterMu sync.Mutex
	go func() {
		for {
			conn, err := localListener.Accept()
			if err != nil {
				return
			}
			counterMu.Lock()
			connCounter++
			connID := connCounter
			counterMu.Unlock()

			go func(c net.Conn, id int) {
				defer c.Close()
				buf := make([]byte, 1024)
				for {
					n, err := c.Read(buf)
					if err != nil {
						return
					}
					// Echo back with connection ID prefix
					response := fmt.Sprintf("conn%d:%s", id, buf[:n])
					c.Write([]byte(response))
				}
			}(conn, connID)
		}
	}()

	// Start bore server and client
	boreServer := server.NewServer(0, "")
	go boreServer.Run()
	defer boreServer.Close()
	time.Sleep(100 * time.Millisecond)

	boreClient := client.NewClient(boreServer.Addr(), localPort, "127.0.0.1", "")
	if err := boreClient.Connect(); err != nil {
		t.Fatalf("Client failed to connect: %v", err)
	}
	defer boreClient.Close()

	go boreClient.Run()
	time.Sleep(100 * time.Millisecond)

	publicPort := boreClient.PublicPort()

	// Create multiple concurrent connections
	numConnections := 5
	var wg sync.WaitGroup
	errors := make(chan error, numConnections)
	results := make(chan string, numConnections)

	for i := 0; i < numConnections; i++ {
		wg.Add(1)
		go func(connNum int) {
			defer wg.Done()

			conn, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", publicPort))
			if err != nil {
				errors <- fmt.Errorf("connection %d: failed to connect: %v", connNum, err)
				return
			}
			defer conn.Close()

			// Send unique data
			msg := fmt.Sprintf("hello%d", connNum)
			_, err = conn.Write([]byte(msg))
			if err != nil {
				errors <- fmt.Errorf("connection %d: failed to write: %v", connNum, err)
				return
			}

			// Read response
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

	// Check for errors
	for err := range errors {
		t.Error(err)
	}

	// Verify all connections got responses
	responseCount := 0
	for result := range results {
		responseCount++
		// Each response should contain "conn" prefix and the original message
		if len(result) < 5 || result[:4] != "conn" {
			t.Errorf("Unexpected response format: %s", result)
		}
	}

	if responseCount != numConnections {
		t.Errorf("Expected %d responses, got %d", numConnections, responseCount)
	}
}

// TestConcurrentConnectionsDataIsolation tests that data doesn't leak between streams.
// Requirements: 4.1, 4.2
func TestConcurrentConnectionsDataIsolation(t *testing.T) {
	// Start a local server that sends unique data per connection
	localListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to create local listener: %v", err)
	}
	defer localListener.Close()
	localPort := localListener.Addr().(*net.TCPAddr).Port

	var connCounter int
	var counterMu sync.Mutex
	go func() {
		for {
			conn, err := localListener.Accept()
			if err != nil {
				return
			}
			counterMu.Lock()
			connCounter++
			connID := connCounter
			counterMu.Unlock()

			go func(c net.Conn, id int) {
				defer c.Close()
				// Read client's identifier
				buf := make([]byte, 1024)
				n, err := c.Read(buf)
				if err != nil {
					return
				}
				clientID := string(buf[:n])

				// Send back unique response with both IDs
				response := fmt.Sprintf("server_conn_%d_client_%s", id, clientID)
				c.Write([]byte(response))
			}(conn, connID)
		}
	}()

	// Start bore server and client
	boreServer := server.NewServer(0, "")
	go boreServer.Run()
	defer boreServer.Close()
	time.Sleep(100 * time.Millisecond)

	boreClient := client.NewClient(boreServer.Addr(), localPort, "127.0.0.1", "")
	if err := boreClient.Connect(); err != nil {
		t.Fatalf("Client failed to connect: %v", err)
	}
	defer boreClient.Close()

	go boreClient.Run()
	time.Sleep(100 * time.Millisecond)

	publicPort := boreClient.PublicPort()

	// Create concurrent connections with unique identifiers
	numConnections := 10
	var wg sync.WaitGroup
	responses := make(map[string]string)
	var responsesMu sync.Mutex

	for i := 0; i < numConnections; i++ {
		wg.Add(1)
		go func(connNum int) {
			defer wg.Done()

			conn, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", publicPort))
			if err != nil {
				t.Errorf("Connection %d: failed to connect: %v", connNum, err)
				return
			}
			defer conn.Close()

			// Send unique identifier
			clientID := fmt.Sprintf("client_%d", connNum)
			conn.Write([]byte(clientID))

			// Read response
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

	// Verify each client got its own response (no data leakage)
	for clientID, response := range responses {
		// Response should contain the client's ID
		if !containsString(response, clientID) {
			t.Errorf("Data isolation failure: client %s got response %s (doesn't contain its ID)",
				clientID, response)
		}
	}

	if len(responses) != numConnections {
		t.Errorf("Expected %d responses, got %d", numConnections, len(responses))
	}
}

// TestConcurrentConnectionsWithLargeData tests concurrent connections with larger data transfers.
// Requirements: 4.1, 4.2
func TestConcurrentConnectionsWithLargeData(t *testing.T) {
	// Start a local echo server
	localListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to create local listener: %v", err)
	}
	defer localListener.Close()
	localPort := localListener.Addr().(*net.TCPAddr).Port

	go func() {
		for {
			conn, err := localListener.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				io.Copy(c, c) // Echo
			}(conn)
		}
	}()

	// Start bore server and client
	boreServer := server.NewServer(0, "")
	go boreServer.Run()
	defer boreServer.Close()
	time.Sleep(100 * time.Millisecond)

	boreClient := client.NewClient(boreServer.Addr(), localPort, "127.0.0.1", "")
	if err := boreClient.Connect(); err != nil {
		t.Fatalf("Client failed to connect: %v", err)
	}
	defer boreClient.Close()

	go boreClient.Run()
	time.Sleep(100 * time.Millisecond)

	publicPort := boreClient.PublicPort()

	// Create concurrent connections with large data
	numConnections := 3
	dataSize := 64 * 1024 // 64KB per connection
	var wg sync.WaitGroup
	errors := make(chan error, numConnections)

	for i := 0; i < numConnections; i++ {
		wg.Add(1)
		go func(connNum int) {
			defer wg.Done()

			conn, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", publicPort))
			if err != nil {
				errors <- fmt.Errorf("connection %d: failed to connect: %v", connNum, err)
				return
			}
			defer conn.Close()

			// Generate unique data pattern for this connection
			data := make([]byte, dataSize)
			for j := range data {
				data[j] = byte((connNum*256 + j) % 256)
			}

			// Send data
			_, err = conn.Write(data)
			if err != nil {
				errors <- fmt.Errorf("connection %d: failed to write: %v", connNum, err)
				return
			}

			// Read response
			response := make([]byte, dataSize)
			conn.SetReadDeadline(time.Now().Add(10 * time.Second))
			_, err = io.ReadFull(conn, response)
			if err != nil {
				errors <- fmt.Errorf("connection %d: failed to read: %v", connNum, err)
				return
			}

			// Verify data integrity
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

// containsString checks if s contains substr
func containsString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// TestClientGracefulShutdown tests that client shuts down cleanly.
// Requirements: 3.3, 4.2
func TestClientGracefulShutdown(t *testing.T) {
	// Start a local echo server
	localListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to create local listener: %v", err)
	}
	defer localListener.Close()
	localPort := localListener.Addr().(*net.TCPAddr).Port

	localConnClosed := make(chan struct{}, 1)
	go func() {
		for {
			conn, err := localListener.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				io.Copy(c, c)
				localConnClosed <- struct{}{}
			}(conn)
		}
	}()

	// Start bore server
	boreServer := server.NewServer(0, "")
	go boreServer.Run()
	defer boreServer.Close()
	time.Sleep(100 * time.Millisecond)

	// Start bore client
	boreClient := client.NewClient(boreServer.Addr(), localPort, "127.0.0.1", "")
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

	// Send some data to establish the stream
	externalConn.Write([]byte("hello"))
	buf := make([]byte, 5)
	externalConn.SetReadDeadline(time.Now().Add(5 * time.Second))
	externalConn.Read(buf)

	// Now close the client gracefully
	boreClient.Close()

	// Wait for client to finish
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
// Requirements: 3.3, 4.2
func TestServerGracefulShutdown(t *testing.T) {
	// Start a local echo server
	localListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to create local listener: %v", err)
	}
	defer localListener.Close()
	localPort := localListener.Addr().(*net.TCPAddr).Port

	go func() {
		for {
			conn, err := localListener.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				io.Copy(c, c)
			}(conn)
		}
	}()

	// Start bore server
	boreServer := server.NewServer(0, "")
	serverDone := make(chan error, 1)
	go func() {
		serverDone <- boreServer.Run()
	}()
	time.Sleep(100 * time.Millisecond)

	serverAddr := boreServer.Addr()

	// Start bore client
	boreClient := client.NewClient(serverAddr, localPort, "127.0.0.1", "")
	if err := boreClient.Connect(); err != nil {
		t.Fatalf("Client failed to connect: %v", err)
	}
	defer boreClient.Close()

	go boreClient.Run()
	time.Sleep(100 * time.Millisecond)

	publicPort := boreClient.PublicPort()

	// Create an active connection through the tunnel
	externalConn, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", publicPort))
	if err != nil {
		t.Fatalf("Failed to connect through tunnel: %v", err)
	}
	defer externalConn.Close()

	// Send some data
	externalConn.Write([]byte("hello"))
	buf := make([]byte, 5)
	externalConn.SetReadDeadline(time.Now().Add(5 * time.Second))
	externalConn.Read(buf)

	// Now close the server gracefully
	boreServer.Close()

	// Wait for server to finish
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
// Requirements: 3.3, 4.2
func TestAbruptClientDisconnection(t *testing.T) {
	// Start a local server that tracks connections
	localListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to create local listener: %v", err)
	}
	defer localListener.Close()
	localPort := localListener.Addr().(*net.TCPAddr).Port

	localConnClosed := make(chan struct{}, 1)
	go func() {
		for {
			conn, err := localListener.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				buf := make([]byte, 1024)
				for {
					_, err := c.Read(buf)
					if err != nil {
						localConnClosed <- struct{}{}
						return
					}
				}
			}(conn)
		}
	}()

	// Start bore server
	boreServer := server.NewServer(0, "")
	go boreServer.Run()
	defer boreServer.Close()
	time.Sleep(100 * time.Millisecond)

	// Start bore client
	boreClient := client.NewClient(boreServer.Addr(), localPort, "127.0.0.1", "")
	if err := boreClient.Connect(); err != nil {
		t.Fatalf("Client failed to connect: %v", err)
	}

	go boreClient.Run()
	time.Sleep(100 * time.Millisecond)

	publicPort := boreClient.PublicPort()

	// Create an active connection through the tunnel
	externalConn, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", publicPort))
	if err != nil {
		t.Fatalf("Failed to connect through tunnel: %v", err)
	}

	// Send some data to establish the stream
	externalConn.Write([]byte("hello"))
	time.Sleep(100 * time.Millisecond)

	// Abruptly close the client (simulating crash)
	boreClient.Close()

	// Verify local connection gets closed
	select {
	case <-localConnClosed:
		// Expected - local connection should be closed when client disconnects
	case <-time.After(5 * time.Second):
		t.Error("Local connection was not closed after client disconnection")
	}

	externalConn.Close()
}

// TestAbruptExternalDisconnection tests handling of sudden external client disconnection.
// Requirements: 3.3, 4.2
func TestAbruptExternalDisconnection(t *testing.T) {
	// Start a local server that tracks connections
	localListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to create local listener: %v", err)
	}
	defer localListener.Close()
	localPort := localListener.Addr().(*net.TCPAddr).Port

	localConnClosed := make(chan struct{}, 1)
	go func() {
		for {
			conn, err := localListener.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				buf := make([]byte, 1024)
				for {
					_, err := c.Read(buf)
					if err != nil {
						localConnClosed <- struct{}{}
						return
					}
				}
			}(conn)
		}
	}()

	// Start bore server and client
	boreServer := server.NewServer(0, "")
	go boreServer.Run()
	defer boreServer.Close()
	time.Sleep(100 * time.Millisecond)

	boreClient := client.NewClient(boreServer.Addr(), localPort, "127.0.0.1", "")
	if err := boreClient.Connect(); err != nil {
		t.Fatalf("Client failed to connect: %v", err)
	}
	defer boreClient.Close()

	go boreClient.Run()
	time.Sleep(100 * time.Millisecond)

	publicPort := boreClient.PublicPort()

	// Create an active connection through the tunnel
	externalConn, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", publicPort))
	if err != nil {
		t.Fatalf("Failed to connect through tunnel: %v", err)
	}

	// Send some data to establish the stream
	externalConn.Write([]byte("hello"))
	time.Sleep(100 * time.Millisecond)

	// Abruptly close the external connection
	externalConn.Close()

	// Verify local connection gets closed (connection close should propagate)
	select {
	case <-localConnClosed:
		// Expected - local connection should be closed when external disconnects
	case <-time.After(5 * time.Second):
		t.Error("Local connection was not closed after external disconnection")
	}
}

// TestConnectionClosePropagatesToOtherEnd tests that closing one end propagates to the other.
// Requirements: 3.3
func TestConnectionClosePropagatesToOtherEnd(t *testing.T) {
	// Start a local server that closes after receiving data
	localListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to create local listener: %v", err)
	}
	defer localListener.Close()
	localPort := localListener.Addr().(*net.TCPAddr).Port

	go func() {
		for {
			conn, err := localListener.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				// Read one message then close
				buf := make([]byte, 1024)
				c.Read(buf)
				c.Write([]byte("goodbye"))
				c.Close()
			}(conn)
		}
	}()

	// Start bore server and client
	boreServer := server.NewServer(0, "")
	go boreServer.Run()
	defer boreServer.Close()
	time.Sleep(100 * time.Millisecond)

	boreClient := client.NewClient(boreServer.Addr(), localPort, "127.0.0.1", "")
	if err := boreClient.Connect(); err != nil {
		t.Fatalf("Client failed to connect: %v", err)
	}
	defer boreClient.Close()

	go boreClient.Run()
	time.Sleep(100 * time.Millisecond)

	publicPort := boreClient.PublicPort()

	// Connect through tunnel
	externalConn, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", publicPort))
	if err != nil {
		t.Fatalf("Failed to connect through tunnel: %v", err)
	}
	defer externalConn.Close()

	// Send data
	externalConn.Write([]byte("hello"))

	// Read response
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
		t.Error("Expected EOF after local server closed, but read succeeded")
	}
}

// TestAuthenticationWithMatchingSecrets tests successful authentication.
// Requirements: 7.1, 7.2, 7.3, 7.6
func TestAuthenticationWithMatchingSecrets(t *testing.T) {
	// Start a local echo server
	localListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to create local listener: %v", err)
	}
	defer localListener.Close()
	localPort := localListener.Addr().(*net.TCPAddr).Port

	go func() {
		for {
			conn, err := localListener.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				io.Copy(c, c)
			}(conn)
		}
	}()

	// Start bore server with secret
	secret := "my-super-secret-token-123"
	boreServer := server.NewServer(0, secret)
	go boreServer.Run()
	defer boreServer.Close()
	time.Sleep(100 * time.Millisecond)

	// Start bore client with matching secret
	boreClient := client.NewClient(boreServer.Addr(), localPort, "127.0.0.1", secret)
	if err := boreClient.Connect(); err != nil {
		t.Fatalf("Client with matching secret should connect successfully: %v", err)
	}
	defer boreClient.Close()

	go boreClient.Run()
	time.Sleep(100 * time.Millisecond)

	publicPort := boreClient.PublicPort()
	if publicPort == 0 {
		t.Fatal("Public port should be allocated after successful authentication")
	}

	// Verify tunnel works
	conn, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", publicPort))
	if err != nil {
		t.Fatalf("Failed to connect through tunnel: %v", err)
	}
	defer conn.Close()

	testData := []byte("authenticated tunnel test")
	conn.Write(testData)

	buf := make([]byte, len(testData))
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	_, err = io.ReadFull(conn, buf)
	if err != nil {
		t.Fatalf("Failed to read through tunnel: %v", err)
	}

	if string(buf) != string(testData) {
		t.Errorf("Data mismatch. Expected: %s, Got: %s", testData, buf)
	}
}

// TestAuthenticationWithWrongSecret tests failed authentication.
// Requirements: 7.1, 7.2, 7.3
func TestAuthenticationWithWrongSecret(t *testing.T) {
	// Start bore server with secret
	serverSecret := "correct-secret"
	boreServer := server.NewServer(0, serverSecret)
	go boreServer.Run()
	defer boreServer.Close()
	time.Sleep(100 * time.Millisecond)

	// Try to connect with wrong secret
	wrongSecret := "wrong-secret"
	boreClient := client.NewClient(boreServer.Addr(), 8080, "127.0.0.1", wrongSecret)
	err := boreClient.Connect()

	if err == nil {
		boreClient.Close()
		t.Fatal("Client with wrong secret should fail to connect")
	}

	// Verify error message indicates authentication failure
	if !containsString(err.Error(), "authentication") && !containsString(err.Error(), "failed") {
		t.Errorf("Error should indicate authentication failure, got: %v", err)
	}
}

// TestAuthenticationWithEmptySecretAgainstProtectedServer tests that empty secret fails against protected server.
// Requirements: 7.1, 7.3
func TestAuthenticationWithEmptySecretAgainstProtectedServer(t *testing.T) {
	// Start bore server with secret
	serverSecret := "server-requires-auth"
	boreServer := server.NewServer(0, serverSecret)
	go boreServer.Run()
	defer boreServer.Close()
	time.Sleep(100 * time.Millisecond)

	// Try to connect without secret
	boreClient := client.NewClient(boreServer.Addr(), 8080, "127.0.0.1", "")
	err := boreClient.Connect()

	if err == nil {
		boreClient.Close()
		t.Fatal("Client without secret should fail to connect to protected server")
	}
}

// TestOpenModeNoSecret tests that server without secret accepts all connections.
// Requirements: 7.5
func TestOpenModeNoSecret(t *testing.T) {
	// Start a local echo server
	localListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to create local listener: %v", err)
	}
	defer localListener.Close()
	localPort := localListener.Addr().(*net.TCPAddr).Port

	go func() {
		for {
			conn, err := localListener.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				io.Copy(c, c)
			}(conn)
		}
	}()

	// Start bore server without secret (open mode)
	boreServer := server.NewServer(0, "")
	go boreServer.Run()
	defer boreServer.Close()
	time.Sleep(100 * time.Millisecond)

	// Connect without secret
	boreClient := client.NewClient(boreServer.Addr(), localPort, "127.0.0.1", "")
	if err := boreClient.Connect(); err != nil {
		t.Fatalf("Client should connect to open server: %v", err)
	}
	defer boreClient.Close()

	go boreClient.Run()
	time.Sleep(100 * time.Millisecond)

	// Verify tunnel works
	conn, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", boreClient.PublicPort()))
	if err != nil {
		t.Fatalf("Failed to connect through tunnel: %v", err)
	}
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
// Requirements: 7.5
func TestOpenModeAcceptsClientWithSecret(t *testing.T) {
	// Start a local echo server
	localListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to create local listener: %v", err)
	}
	defer localListener.Close()
	localPort := localListener.Addr().(*net.TCPAddr).Port

	go func() {
		for {
			conn, err := localListener.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				io.Copy(c, c)
			}(conn)
		}
	}()

	// Start bore server without secret (open mode)
	boreServer := server.NewServer(0, "")
	go boreServer.Run()
	defer boreServer.Close()
	time.Sleep(100 * time.Millisecond)

	// Connect WITH a secret (server should still accept since it's in open mode)
	boreClient := client.NewClient(boreServer.Addr(), localPort, "127.0.0.1", "some-secret")
	if err := boreClient.Connect(); err != nil {
		t.Fatalf("Open server should accept client with secret: %v", err)
	}
	defer boreClient.Close()

	go boreClient.Run()
	time.Sleep(100 * time.Millisecond)

	// Verify tunnel works
	conn, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", boreClient.PublicPort()))
	if err != nil {
		t.Fatalf("Failed to connect through tunnel: %v", err)
	}
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
// Requirements: 7.1, 7.6
func TestMultipleClientsWithSameSecret(t *testing.T) {
	// Start local echo servers
	localListeners := make([]net.Listener, 3)
	localPorts := make([]int, 3)

	for i := 0; i < 3; i++ {
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatalf("Failed to create local listener %d: %v", i, err)
		}
		defer listener.Close()
		localListeners[i] = listener
		localPorts[i] = listener.Addr().(*net.TCPAddr).Port

		go func(l net.Listener) {
			for {
				conn, err := l.Accept()
				if err != nil {
					return
				}
				go func(c net.Conn) {
					defer c.Close()
					io.Copy(c, c)
				}(conn)
			}
		}(listener)
	}

	// Start bore server with secret
	secret := "shared-secret"
	boreServer := server.NewServer(0, secret)
	go boreServer.Run()
	defer boreServer.Close()
	time.Sleep(100 * time.Millisecond)

	// Connect multiple clients with the same secret
	clients := make([]*client.Client, 3)
	for i := 0; i < 3; i++ {
		c := client.NewClient(boreServer.Addr(), localPorts[i], "127.0.0.1", secret)
		if err := c.Connect(); err != nil {
			t.Fatalf("Client %d failed to connect: %v", i, err)
		}
		defer c.Close()
		clients[i] = c

		go c.Run()
	}

	time.Sleep(100 * time.Millisecond)

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
		conn, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", c.PublicPort()))
		if err != nil {
			t.Errorf("Failed to connect to client %d tunnel: %v", i, err)
			continue
		}

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
