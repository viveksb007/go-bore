package protocol

import (
	"encoding/binary"
	"fmt"
	"io"
)

// Message types
const (
	MessageTypeHandshake       = 0x01 // Client → Server: Initial handshake with auth
	MessageTypeAccept          = 0x02 // Server → Client: Handshake accepted, includes public port
	MessageTypeReject          = 0x03 // Server → Client: Handshake rejected
	MessageTypeConnect         = 0x04 // Server → Client: New external connection (includes UUID)
	MessageTypeStreamHandshake = 0x05 // Client → Server: Identify which stream (includes UUID)
	MessageTypeStreamAccept    = 0x06 // Server → Client: Stream matched, ready for data
	MessageTypeStreamReject    = 0x07 // Server → Client: UUID not found or expired
)

// Protocol version
const ProtocolVersion = 1

// Maximum payload size (10MB)
const MaxPayloadSize = 10 * 1024 * 1024

// Message represents a protocol message
type Message struct {
	Type    uint8
	Payload []byte
}

// WriteMessage writes a message to the writer
// Frame format: [Type:1][Length:4][Payload:variable]
func WriteMessage(w io.Writer, msg *Message) error {
	if len(msg.Payload) > MaxPayloadSize {
		return fmt.Errorf("payload size %d exceeds maximum %d", len(msg.Payload), MaxPayloadSize)
	}

	// Write message type (1 byte)
	if err := binary.Write(w, binary.BigEndian, msg.Type); err != nil {
		return fmt.Errorf("failed to write message type: %w", err)
	}

	// Write payload length (4 bytes)
	payloadLen := uint32(len(msg.Payload))
	if err := binary.Write(w, binary.BigEndian, payloadLen); err != nil {
		return fmt.Errorf("failed to write payload length: %w", err)
	}

	// Write payload
	if payloadLen > 0 {
		if _, err := w.Write(msg.Payload); err != nil {
			return fmt.Errorf("failed to write payload: %w", err)
		}
	}

	return nil
}

// ReadMessage reads a message from the reader
func ReadMessage(r io.Reader) (*Message, error) {
	msg := &Message{}

	// Read message type (1 byte)
	if err := binary.Read(r, binary.BigEndian, &msg.Type); err != nil {
		return nil, fmt.Errorf("failed to read message type: %w", err)
	}

	// Read payload length (4 bytes)
	var payloadLen uint32
	if err := binary.Read(r, binary.BigEndian, &payloadLen); err != nil {
		return nil, fmt.Errorf("failed to read payload length: %w", err)
	}

	// Validate payload length
	if payloadLen > MaxPayloadSize {
		return nil, fmt.Errorf("payload size %d exceeds maximum %d", payloadLen, MaxPayloadSize)
	}

	// Read payload
	if payloadLen > 0 {
		msg.Payload = make([]byte, payloadLen)
		if _, err := io.ReadFull(r, msg.Payload); err != nil {
			return nil, fmt.Errorf("failed to read payload: %w", err)
		}
	}

	return msg, nil
}
