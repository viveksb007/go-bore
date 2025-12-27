package client

import (
	"net"
	"testing"
	"time"

	"github.com/viveksb007/go-bore/internal/protocol"
)

func TestNewClient(t *testing.T) {
	tests := []struct {
		name       string
		serverAddr string
		localPort  int
		localHost  string
		secret     string
		wantHost   string
	}{
		{
			name:       "with all parameters",
			serverAddr: "example.com:7835",
			localPort:  8080,
			localHost:  "192.168.1.100",
			secret:     "mysecret",
			wantHost:   "192.168.1.100",
		},
		{
			name:       "with empty local host defaults to localhost",
			serverAddr: "example.com:7835",
			localPort:  3000,
			localHost:  "",
			secret:     "",
			wantHost:   "localhost",
		},
		{
			name:       "with localhost explicitly set",
			serverAddr: "127.0.0.1:7835",
			localPort:  5000,
			localHost:  "localhost",
			secret:     "token123",
			wantHost:   "localhost",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewClient(tt.serverAddr, tt.localPort, tt.localHost, tt.secret)

			if client.ServerAddr() != tt.serverAddr {
				t.Errorf("ServerAddr() = %v, want %v", client.ServerAddr(), tt.serverAddr)
			}
			if client.LocalPort() != tt.localPort {
				t.Errorf("LocalPort() = %v, want %v", client.LocalPort(), tt.localPort)
			}
			if client.LocalHost() != tt.wantHost {
				t.Errorf("LocalHost() = %v, want %v", client.LocalHost(), tt.wantHost)
			}
			if client.secret != tt.secret {
				t.Errorf("secret = %v, want %v", client.secret, tt.secret)
			}
			if client.streams == nil {
				t.Error("streams map should be initialized")
			}
			if client.controlConn != nil {
				t.Error("controlConn should be nil before Connect()")
			}
		})
	}
}

func TestClientConnectSuccess(t *testing.T) {
	// Start a mock server
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to create listener: %v", err)
	}
	defer listener.Close()

	serverAddr := listener.Addr().String()
	expectedPort := uint16(12345)
	expectedHost := "test-server.example.com"

	// Handle server side in goroutine
	serverDone := make(chan error, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			serverDone <- err
			return
		}
		defer conn.Close()

		// Read handshake message
		msg, err := protocol.ReadMessage(conn)
		if err != nil {
			serverDone <- err
			return
		}

		if msg.Type != protocol.MessageTypeHandshake {
			serverDone <- err
			return
		}

		// Decode and verify handshake
		handshake, err := protocol.DecodeHandshake(msg.Payload)
		if err != nil {
			serverDone <- err
			return
		}

		if handshake.Version != protocol.ProtocolVersion {
			serverDone <- err
			return
		}

		if handshake.Secret != "testsecret" {
			serverDone <- err
			return
		}

		// Send accept message
		acceptPayload := &protocol.AcceptPayload{
			PublicPort: expectedPort,
			ServerHost: expectedHost,
		}
		payloadBytes, _ := protocol.EncodeAccept(acceptPayload)
		acceptMsg := &protocol.Message{
			Type:    protocol.MessageTypeAccept,
			Payload: payloadBytes,
		}
		protocol.WriteMessage(conn, acceptMsg)

		serverDone <- nil
	}()

	// Create client and connect
	client := NewClient(serverAddr, 8080, "localhost", "testsecret")
	err = client.Connect()
	if err != nil {
		t.Fatalf("Connect() failed: %v", err)
	}
	defer client.Close()

	// Wait for server to complete
	select {
	case err := <-serverDone:
		if err != nil {
			t.Fatalf("Server error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Server timed out")
	}

	// Verify client state
	if client.PublicPort() != expectedPort {
		t.Errorf("PublicPort() = %v, want %v", client.PublicPort(), expectedPort)
	}
	if client.ServerHost() != expectedHost {
		t.Errorf("ServerHost() = %v, want %v", client.ServerHost(), expectedHost)
	}
	if client.controlConn == nil {
		t.Error("controlConn should not be nil after successful Connect()")
	}
}

func TestClientConnectAuthFailure(t *testing.T) {
	// Start a mock server that rejects authentication
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to create listener: %v", err)
	}
	defer listener.Close()

	serverAddr := listener.Addr().String()

	// Handle server side in goroutine
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()

		// Read handshake message
		_, err = protocol.ReadMessage(conn)
		if err != nil {
			return
		}

		// Send reject message
		rejectMsg := &protocol.Message{
			Type:    protocol.MessageTypeReject,
			Payload: nil,
		}
		protocol.WriteMessage(conn, rejectMsg)
	}()

	// Create client and connect
	client := NewClient(serverAddr, 8080, "localhost", "wrongsecret")
	err = client.Connect()

	if err == nil {
		t.Fatal("Connect() should have failed with authentication error")
	}

	if client.controlConn != nil {
		t.Error("controlConn should be nil after failed Connect()")
	}
}

func TestClientConnectServerUnreachable(t *testing.T) {
	// Try to connect to a non-existent server
	client := NewClient("127.0.0.1:59999", 8080, "localhost", "secret")
	err := client.Connect()

	if err == nil {
		t.Fatal("Connect() should have failed when server is unreachable")
	}

	if client.controlConn != nil {
		t.Error("controlConn should be nil after failed Connect()")
	}
}

func TestClientClose(t *testing.T) {
	// Start a mock server
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to create listener: %v", err)
	}
	defer listener.Close()

	serverAddr := listener.Addr().String()

	// Handle server side in goroutine
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()

		// Read handshake message
		protocol.ReadMessage(conn)

		// Send accept message
		acceptPayload := &protocol.AcceptPayload{
			PublicPort: 12345,
			ServerHost: "localhost",
		}
		payloadBytes, _ := protocol.EncodeAccept(acceptPayload)
		acceptMsg := &protocol.Message{
			Type:    protocol.MessageTypeAccept,
			Payload: payloadBytes,
		}
		protocol.WriteMessage(conn, acceptMsg)

		// Keep connection open until client closes
		buf := make([]byte, 1)
		conn.Read(buf)
	}()

	// Create client and connect
	client := NewClient(serverAddr, 8080, "localhost", "")
	err = client.Connect()
	if err != nil {
		t.Fatalf("Connect() failed: %v", err)
	}

	// Verify connection is established
	if client.controlConn == nil {
		t.Fatal("controlConn should not be nil after Connect()")
	}

	// Close client
	err = client.Close()
	if err != nil {
		t.Errorf("Close() returned error: %v", err)
	}

	// Verify connection is closed
	if client.controlConn != nil {
		t.Error("controlConn should be nil after Close()")
	}
}

func TestClientConnectNoSecret(t *testing.T) {
	// Start a mock server that accepts connections without secret (open mode)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to create listener: %v", err)
	}
	defer listener.Close()

	serverAddr := listener.Addr().String()

	// Handle server side in goroutine
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()

		// Read handshake message
		msg, err := protocol.ReadMessage(conn)
		if err != nil {
			return
		}

		// Verify empty secret
		handshake, _ := protocol.DecodeHandshake(msg.Payload)
		if handshake.Secret != "" {
			return
		}

		// Send accept message
		acceptPayload := &protocol.AcceptPayload{
			PublicPort: 54321,
			ServerHost: "open-server",
		}
		payloadBytes, _ := protocol.EncodeAccept(acceptPayload)
		acceptMsg := &protocol.Message{
			Type:    protocol.MessageTypeAccept,
			Payload: payloadBytes,
		}
		protocol.WriteMessage(conn, acceptMsg)
	}()

	// Create client without secret and connect
	client := NewClient(serverAddr, 3000, "", "")
	err = client.Connect()
	if err != nil {
		t.Fatalf("Connect() failed: %v", err)
	}
	defer client.Close()

	if client.PublicPort() != 54321 {
		t.Errorf("PublicPort() = %v, want 54321", client.PublicPort())
	}
}
