package protocol

import (
	"strings"
	"testing"
)

func TestHandshakePayloadEncodeDecode(t *testing.T) {
	tests := []struct {
		name    string
		payload *HandshakePayload
	}{
		{
			name: "with secret",
			payload: &HandshakePayload{
				Version: ProtocolVersion,
				Secret:  "my-secret-token",
			},
		},
		{
			name: "without secret",
			payload: &HandshakePayload{
				Version: ProtocolVersion,
				Secret:  "",
			},
		},
		{
			name: "with long secret",
			payload: &HandshakePayload{
				Version: ProtocolVersion,
				Secret:  strings.Repeat("x", 1000),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Encode
			data, err := EncodeHandshake(tt.payload)
			if err != nil {
				t.Fatalf("EncodeHandshake failed: %v", err)
			}

			// Decode
			decoded, err := DecodeHandshake(data)
			if err != nil {
				t.Fatalf("DecodeHandshake failed: %v", err)
			}

			// Verify
			if decoded.Version != tt.payload.Version {
				t.Errorf("Version mismatch: got %d, want %d", decoded.Version, tt.payload.Version)
			}

			if decoded.Secret != tt.payload.Secret {
				t.Errorf("Secret mismatch: got %q, want %q", decoded.Secret, tt.payload.Secret)
			}
		})
	}
}

func TestDecodeHandshakeTooShort(t *testing.T) {
	tests := []struct {
		name string
		data []byte
	}{
		{
			name: "empty",
			data: []byte{},
		},
		{
			name: "only version",
			data: []byte{1},
		},
		{
			name: "version and partial length",
			data: []byte{1, 0},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := DecodeHandshake(tt.data)
			if err == nil {
				t.Fatal("Expected error for short payload, got nil")
			}

			if !strings.Contains(err.Error(), "too short") {
				t.Errorf("Expected 'too short' error, got: %v", err)
			}
		})
	}
}

func TestDecodeHandshakeInvalidLength(t *testing.T) {
	// Version=1, SecretLen=10, but only 5 bytes of secret
	data := []byte{1, 0, 10, 'a', 'b', 'c', 'd', 'e'}

	_, err := DecodeHandshake(data)
	if err == nil {
		t.Fatal("Expected error for invalid length, got nil")
	}

	if !strings.Contains(err.Error(), "invalid handshake payload") {
		t.Errorf("Expected 'invalid handshake payload' error, got: %v", err)
	}
}

func TestAcceptPayloadEncodeDecode(t *testing.T) {
	tests := []struct {
		name    string
		payload *AcceptPayload
	}{
		{
			name: "standard port",
			payload: &AcceptPayload{
				PublicPort: 12345,
			},
		},
		{
			name: "high port",
			payload: &AcceptPayload{
				PublicPort: 8080,
			},
		},
		{
			name: "max port",
			payload: &AcceptPayload{
				PublicPort: 65535,
			},
		},
		{
			name: "low port",
			payload: &AcceptPayload{
				PublicPort: 443,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Encode
			data, err := EncodeAccept(tt.payload)
			if err != nil {
				t.Fatalf("EncodeAccept failed: %v", err)
			}

			// Decode
			decoded, err := DecodeAccept(data)
			if err != nil {
				t.Fatalf("DecodeAccept failed: %v", err)
			}

			// Verify
			if decoded.PublicPort != tt.payload.PublicPort {
				t.Errorf("PublicPort mismatch: got %d, want %d", decoded.PublicPort, tt.payload.PublicPort)
			}
		})
	}
}

func TestDecodeAcceptTooShort(t *testing.T) {
	tests := []struct {
		name string
		data []byte
	}{
		{
			name: "empty",
			data: []byte{},
		},
		{
			name: "only one byte",
			data: []byte{0},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := DecodeAccept(tt.data)
			if err == nil {
				t.Fatal("Expected error for short payload, got nil")
			}

			if !strings.Contains(err.Error(), "too short") {
				t.Errorf("Expected 'too short' error, got: %v", err)
			}
		})
	}
}

func TestHandshakePayloadRoundTrip(t *testing.T) {
	original := &HandshakePayload{
		Version: ProtocolVersion,
		Secret:  "test-secret-123",
	}

	// Encode
	encoded, err := EncodeHandshake(original)
	if err != nil {
		t.Fatalf("Encode failed: %v", err)
	}

	// Decode
	decoded, err := DecodeHandshake(encoded)
	if err != nil {
		t.Fatalf("Decode failed: %v", err)
	}

	// Verify exact match
	if decoded.Version != original.Version || decoded.Secret != original.Secret {
		t.Errorf("Round trip failed: got %+v, want %+v", decoded, original)
	}
}

func TestAcceptPayloadRoundTrip(t *testing.T) {
	original := &AcceptPayload{
		PublicPort: 54321,
	}

	// Encode
	encoded, err := EncodeAccept(original)
	if err != nil {
		t.Fatalf("Encode failed: %v", err)
	}

	// Decode
	decoded, err := DecodeAccept(encoded)
	if err != nil {
		t.Fatalf("Decode failed: %v", err)
	}

	// Verify exact match
	if decoded.PublicPort != original.PublicPort {
		t.Errorf("Round trip failed: got %+v, want %+v", decoded, original)
	}
}

func TestConnectPayloadEncodeDecode(t *testing.T) {
	tests := []struct {
		name    string
		payload *ConnectPayload
	}{
		{
			name: "valid UUID",
			payload: &ConnectPayload{
				UUID: "550e8400-e29b-41d4-a716-446655440000",
			},
		},
		{
			name: "another valid UUID",
			payload: &ConnectPayload{
				UUID: "123e4567-e89b-12d3-a456-426614174000",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Encode
			data, err := EncodeConnect(tt.payload)
			if err != nil {
				t.Fatalf("EncodeConnect failed: %v", err)
			}

			// Verify length
			if len(data) != 36 {
				t.Errorf("Encoded length = %d, want 36", len(data))
			}

			// Decode
			decoded, err := DecodeConnect(data)
			if err != nil {
				t.Fatalf("DecodeConnect failed: %v", err)
			}

			// Verify
			if decoded.UUID != tt.payload.UUID {
				t.Errorf("UUID mismatch: got %q, want %q", decoded.UUID, tt.payload.UUID)
			}
		})
	}
}

func TestEncodeConnectInvalidUUID(t *testing.T) {
	tests := []struct {
		name string
		uuid string
	}{
		{
			name: "too short",
			uuid: "550e8400-e29b-41d4-a716",
		},
		{
			name: "too long",
			uuid: "550e8400-e29b-41d4-a716-446655440000-extra",
		},
		{
			name: "empty",
			uuid: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			payload := &ConnectPayload{UUID: tt.uuid}
			_, err := EncodeConnect(payload)
			if err == nil {
				t.Fatal("Expected error for invalid UUID length, got nil")
			}

			if !strings.Contains(err.Error(), "invalid UUID length") {
				t.Errorf("Expected 'invalid UUID length' error, got: %v", err)
			}
		})
	}
}

func TestDecodeConnectInvalidLength(t *testing.T) {
	tests := []struct {
		name string
		data []byte
	}{
		{
			name: "too short",
			data: []byte("550e8400-e29b-41d4"),
		},
		{
			name: "too long",
			data: []byte("550e8400-e29b-41d4-a716-446655440000-extra"),
		},
		{
			name: "empty",
			data: []byte{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := DecodeConnect(tt.data)
			if err == nil {
				t.Fatal("Expected error for invalid length, got nil")
			}

			if !strings.Contains(err.Error(), "invalid connect payload length") {
				t.Errorf("Expected 'invalid connect payload length' error, got: %v", err)
			}
		})
	}
}

func TestStreamHandshakePayloadEncodeDecode(t *testing.T) {
	tests := []struct {
		name    string
		payload *StreamHandshakePayload
	}{
		{
			name: "valid UUID",
			payload: &StreamHandshakePayload{
				UUID: "550e8400-e29b-41d4-a716-446655440000",
			},
		},
		{
			name: "another valid UUID",
			payload: &StreamHandshakePayload{
				UUID: "123e4567-e89b-12d3-a456-426614174000",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Encode
			data, err := EncodeStreamHandshake(tt.payload)
			if err != nil {
				t.Fatalf("EncodeStreamHandshake failed: %v", err)
			}

			// Verify length
			if len(data) != 36 {
				t.Errorf("Encoded length = %d, want 36", len(data))
			}

			// Decode
			decoded, err := DecodeStreamHandshake(data)
			if err != nil {
				t.Fatalf("DecodeStreamHandshake failed: %v", err)
			}

			// Verify
			if decoded.UUID != tt.payload.UUID {
				t.Errorf("UUID mismatch: got %q, want %q", decoded.UUID, tt.payload.UUID)
			}
		})
	}
}

func TestEncodeStreamHandshakeInvalidUUID(t *testing.T) {
	tests := []struct {
		name string
		uuid string
	}{
		{
			name: "too short",
			uuid: "550e8400-e29b-41d4-a716",
		},
		{
			name: "too long",
			uuid: "550e8400-e29b-41d4-a716-446655440000-extra",
		},
		{
			name: "empty",
			uuid: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			payload := &StreamHandshakePayload{UUID: tt.uuid}
			_, err := EncodeStreamHandshake(payload)
			if err == nil {
				t.Fatal("Expected error for invalid UUID length, got nil")
			}

			if !strings.Contains(err.Error(), "invalid UUID length") {
				t.Errorf("Expected 'invalid UUID length' error, got: %v", err)
			}
		})
	}
}

func TestDecodeStreamHandshakeInvalidLength(t *testing.T) {
	tests := []struct {
		name string
		data []byte
	}{
		{
			name: "too short",
			data: []byte("550e8400-e29b-41d4"),
		},
		{
			name: "too long",
			data: []byte("550e8400-e29b-41d4-a716-446655440000-extra"),
		},
		{
			name: "empty",
			data: []byte{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := DecodeStreamHandshake(tt.data)
			if err == nil {
				t.Fatal("Expected error for invalid length, got nil")
			}

			if !strings.Contains(err.Error(), "invalid stream handshake payload length") {
				t.Errorf("Expected 'invalid stream handshake payload length' error, got: %v", err)
			}
		})
	}
}

func TestConnectPayloadRoundTrip(t *testing.T) {
	original := &ConnectPayload{
		UUID: "550e8400-e29b-41d4-a716-446655440000",
	}

	// Encode
	encoded, err := EncodeConnect(original)
	if err != nil {
		t.Fatalf("Encode failed: %v", err)
	}

	// Decode
	decoded, err := DecodeConnect(encoded)
	if err != nil {
		t.Fatalf("Decode failed: %v", err)
	}

	// Verify exact match
	if decoded.UUID != original.UUID {
		t.Errorf("Round trip failed: got %+v, want %+v", decoded, original)
	}
}

func TestStreamHandshakePayloadRoundTrip(t *testing.T) {
	original := &StreamHandshakePayload{
		UUID: "123e4567-e89b-12d3-a456-426614174000",
	}

	// Encode
	encoded, err := EncodeStreamHandshake(original)
	if err != nil {
		t.Fatalf("Encode failed: %v", err)
	}

	// Decode
	decoded, err := DecodeStreamHandshake(encoded)
	if err != nil {
		t.Fatalf("Decode failed: %v", err)
	}

	// Verify exact match
	if decoded.UUID != original.UUID {
		t.Errorf("Round trip failed: got %+v, want %+v", decoded, original)
	}
}
