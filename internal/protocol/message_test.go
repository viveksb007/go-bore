package protocol

import (
	"bytes"
	"strings"
	"testing"
)

func TestWriteReadMessage(t *testing.T) {
	tests := []struct {
		name    string
		message *Message
	}{
		{
			name: "handshake message with payload",
			message: &Message{
				Type:    MessageTypeHandshake,
				Payload: []byte("test payload"),
			},
		},
		{
			name: "connect message with UUID payload",
			message: &Message{
				Type:    MessageTypeConnect,
				Payload: []byte("550e8400-e29b-41d4-a716-446655440000"),
			},
		},
		{
			name: "stream handshake message",
			message: &Message{
				Type:    MessageTypeStreamHandshake,
				Payload: []byte("123e4567-e89b-12d3-a456-426614174000"),
			},
		},
		{
			name: "stream accept message without payload",
			message: &Message{
				Type:    MessageTypeStreamAccept,
				Payload: nil,
			},
		},
		{
			name: "message with large payload",
			message: &Message{
				Type:    MessageTypeAccept,
				Payload: bytes.Repeat([]byte("x"), 1024*1024), // 1MB
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer

			// Write message
			err := WriteMessage(&buf, tt.message)
			if err != nil {
				t.Fatalf("WriteMessage failed: %v", err)
			}

			// Read message
			readMsg, err := ReadMessage(&buf)
			if err != nil {
				t.Fatalf("ReadMessage failed: %v", err)
			}

			// Verify message type
			if readMsg.Type != tt.message.Type {
				t.Errorf("Type mismatch: got %d, want %d", readMsg.Type, tt.message.Type)
			}

			// Verify payload
			if !bytes.Equal(readMsg.Payload, tt.message.Payload) {
				t.Errorf("Payload mismatch: got %v, want %v", readMsg.Payload, tt.message.Payload)
			}
		})
	}
}

func TestWriteMessagePayloadTooLarge(t *testing.T) {
	msg := &Message{
		Type:    MessageTypeConnect,
		Payload: make([]byte, MaxPayloadSize+1),
	}

	var buf bytes.Buffer
	err := WriteMessage(&buf, msg)
	if err == nil {
		t.Fatal("Expected error for payload exceeding max size, got nil")
	}

	if !strings.Contains(err.Error(), "exceeds maximum") {
		t.Errorf("Expected error about exceeding maximum, got: %v", err)
	}
}

func TestReadMessagePayloadTooLarge(t *testing.T) {
	var buf bytes.Buffer

	// Manually construct a message with invalid payload length
	buf.WriteByte(MessageTypeConnect)         // Type
	buf.Write([]byte{0xFF, 0xFF, 0xFF, 0xFF}) // Length = max uint32

	_, err := ReadMessage(&buf)
	if err == nil {
		t.Fatal("Expected error for payload exceeding max size, got nil")
	}

	if !strings.Contains(err.Error(), "exceeds maximum") {
		t.Errorf("Expected error about exceeding maximum, got: %v", err)
	}
}

func TestReadMessageIncompleteFrame(t *testing.T) {
	tests := []struct {
		name string
		data []byte
	}{
		{
			name: "empty buffer",
			data: []byte{},
		},
		{
			name: "only type byte",
			data: []byte{MessageTypeHandshake},
		},
		{
			name: "type and partial stream ID",
			data: []byte{MessageTypeHandshake, 0, 0},
		},
		{
			name: "type and partial length",
			data: []byte{MessageTypeHandshake, 0, 0},
		},
		{
			name: "complete header but incomplete payload",
			data: []byte{
				MessageTypeConnect, // Type
				0, 0, 0, 10,        // Length = 10
				1, 2, 3, // Only 3 bytes of payload
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			buf := bytes.NewReader(tt.data)
			_, err := ReadMessage(buf)
			if err == nil {
				t.Fatal("Expected error for incomplete frame, got nil")
			}
		})
	}
}

func TestMessageFrameFormat(t *testing.T) {
	msg := &Message{
		Type:    MessageTypeHandshake,
		Payload: []byte{0xAA, 0xBB, 0xCC},
	}

	var buf bytes.Buffer
	err := WriteMessage(&buf, msg)
	if err != nil {
		t.Fatalf("WriteMessage failed: %v", err)
	}

	data := buf.Bytes()

	// Verify frame format
	// [Type:1][Length:4][Payload:3] = 8 bytes total
	expectedLen := 1 + 4 + 3
	if len(data) != expectedLen {
		t.Errorf("Frame length mismatch: got %d, want %d", len(data), expectedLen)
	}

	// Verify type byte
	if data[0] != MessageTypeHandshake {
		t.Errorf("Type byte mismatch: got 0x%02X, want 0x%02X", data[0], MessageTypeHandshake)
	}

	// Verify length (big endian)
	length := uint32(data[1])<<24 | uint32(data[2])<<16 | uint32(data[3])<<8 | uint32(data[4])
	if length != 3 {
		t.Errorf("Length mismatch: got %d, want 3", length)
	}

	// Verify payload
	if !bytes.Equal(data[5:], []byte{0xAA, 0xBB, 0xCC}) {
		t.Errorf("Payload mismatch: got %v, want [0xAA 0xBB 0xCC]", data[5:])
	}
}

func TestMultipleMessages(t *testing.T) {
	messages := []*Message{
		{Type: MessageTypeHandshake, Payload: []byte("first")},
		{Type: MessageTypeAccept, Payload: []byte("second")},
		{Type: MessageTypeConnect, Payload: []byte("550e8400-e29b-41d4-a716-446655440000")},
		{Type: MessageTypeStreamAccept, Payload: nil},
	}

	var buf bytes.Buffer

	// Write all messages
	for _, msg := range messages {
		if err := WriteMessage(&buf, msg); err != nil {
			t.Fatalf("WriteMessage failed: %v", err)
		}
	}

	// Read all messages
	for i, expected := range messages {
		msg, err := ReadMessage(&buf)
		if err != nil {
			t.Fatalf("ReadMessage %d failed: %v", i, err)
		}

		if msg.Type != expected.Type {
			t.Errorf("Message %d type mismatch: got %d, want %d", i, msg.Type, expected.Type)
		}

		if !bytes.Equal(msg.Payload, expected.Payload) {
			t.Errorf("Message %d payload mismatch: got %v, want %v", i, msg.Payload, expected.Payload)
		}
	}

	// Verify buffer is empty
	if buf.Len() != 0 {
		t.Errorf("Buffer not empty after reading all messages: %d bytes remaining", buf.Len())
	}
}

func TestReadMessageEOF(t *testing.T) {
	var buf bytes.Buffer
	_, err := ReadMessage(&buf)
	if err == nil {
		t.Fatal("Expected error when reading from empty buffer, got nil")
	}
	// The error should contain EOF (it's wrapped in our error message)
	if !strings.Contains(err.Error(), "EOF") {
		t.Errorf("Expected EOF error, got: %v", err)
	}
}
