package protocol

import (
	"encoding/binary"
	"fmt"
)

// HandshakePayload represents the handshake message payload
type HandshakePayload struct {
	Version uint8
	Secret  string
}

// EncodeHandshake encodes a handshake payload
func EncodeHandshake(payload *HandshakePayload) ([]byte, error) {
	secretBytes := []byte(payload.Secret)
	secretLen := uint16(len(secretBytes))

	// Calculate total size: version(1) + secretLen(2) + secret(variable)
	buf := make([]byte, 1+2+len(secretBytes))

	// Write version
	buf[0] = payload.Version

	// Write secret length
	binary.BigEndian.PutUint16(buf[1:3], secretLen)

	// Write secret
	copy(buf[3:], secretBytes)

	return buf, nil
}

// DecodeHandshake decodes a handshake payload
func DecodeHandshake(data []byte) (*HandshakePayload, error) {
	if len(data) < 3 {
		return nil, fmt.Errorf("handshake payload too short: %d bytes", len(data))
	}

	payload := &HandshakePayload{}

	// Read version
	payload.Version = data[0]

	// Read secret length
	secretLen := binary.BigEndian.Uint16(data[1:3])

	// Validate length
	if len(data) < 3+int(secretLen) {
		return nil, fmt.Errorf("invalid handshake payload: expected %d bytes, got %d", 3+secretLen, len(data))
	}

	// Read secret
	if secretLen > 0 {
		payload.Secret = string(data[3 : 3+secretLen])
	}

	return payload, nil
}

// AcceptPayload represents the accept message payload
type AcceptPayload struct {
	PublicPort uint16
}

// EncodeAccept encodes an accept payload
func EncodeAccept(payload *AcceptPayload) ([]byte, error) {
	buf := make([]byte, 2)
	binary.BigEndian.PutUint16(buf[0:2], payload.PublicPort)
	return buf, nil
}

// DecodeAccept decodes an accept payload
func DecodeAccept(data []byte) (*AcceptPayload, error) {
	if len(data) < 2 {
		return nil, fmt.Errorf("accept payload too short: %d bytes", len(data))
	}

	payload := &AcceptPayload{}
	payload.PublicPort = binary.BigEndian.Uint16(data[0:2])
	return payload, nil
}

// ConnectPayload represents the connect message payload
type ConnectPayload struct {
	UUID string // 36-character UUID string
}

// EncodeConnect encodes a connect payload
func EncodeConnect(payload *ConnectPayload) ([]byte, error) {
	uuidBytes := []byte(payload.UUID)
	if len(uuidBytes) != 36 {
		return nil, fmt.Errorf("invalid UUID length: %d, expected 36", len(uuidBytes))
	}
	return uuidBytes, nil
}

// DecodeConnect decodes a connect payload
func DecodeConnect(data []byte) (*ConnectPayload, error) {
	if len(data) != 36 {
		return nil, fmt.Errorf("invalid connect payload length: %d, expected 36", len(data))
	}
	return &ConnectPayload{
		UUID: string(data),
	}, nil
}

// StreamHandshakePayload represents the stream handshake message payload
type StreamHandshakePayload struct {
	UUID string // 36-character UUID string
}

// EncodeStreamHandshake encodes a stream handshake payload
func EncodeStreamHandshake(payload *StreamHandshakePayload) ([]byte, error) {
	uuidBytes := []byte(payload.UUID)
	if len(uuidBytes) != 36 {
		return nil, fmt.Errorf("invalid UUID length: %d, expected 36", len(uuidBytes))
	}
	return uuidBytes, nil
}

// DecodeStreamHandshake decodes a stream handshake payload
func DecodeStreamHandshake(data []byte) (*StreamHandshakePayload, error) {
	if len(data) != 36 {
		return nil, fmt.Errorf("invalid stream handshake payload length: %d, expected 36", len(data))
	}
	return &StreamHandshakePayload{
		UUID: string(data),
	}, nil
}
