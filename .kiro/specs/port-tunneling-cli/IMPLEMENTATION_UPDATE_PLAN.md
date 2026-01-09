# Implementation Update Plan

## Overview

This document outlines the specific code changes needed to update the existing implementation from the multiplexed approach to the connection-per-stream approach.

---

## Status of Completed Tasks

### ✅ Task 1: Protocol Foundation - **NEEDS MINOR UPDATES**

**Current State:** Working, but uses old message types

**Required Changes:**
1. Update message type constants
2. Add new payload types
3. Keep existing encode/decode functions (still needed for control messages)

### ✅ Task 2: Port Allocator - **NO CHANGES NEEDED**

**Current State:** Fully compatible with new design

**Status:** ✅ Complete, no updates required

### ✅ Task 3: Server Structure - **NEEDS UPDATES**

**Current State:** Basic structure exists but missing new fields

**Required Changes:**
1. Add `pendingStreams` map to Server struct
2. Add `PendingStream` struct definition
3. Update `ClientSession` to use UUID-based streams map
4. Rename `Stream` to `ServerStream` and update fields

### ✅ Task 4: Handshake and Authentication - **NO CHANGES NEEDED**

**Current State:** Fully compatible with new design

**Status:** ✅ Complete, no updates required

### ✅ Task 5: Public Port Listener - **NEEDS SIGNIFICANT UPDATES**

**Current State:** Uses stream IDs, no UUID support, no pending streams

**Required Changes:**
1. Replace stream ID generation with UUID generation
2. Add pending streams tracking
3. Add timeout mechanism
4. Update `sendConnect` to use `ConnectPayload`

---

## Detailed Update Instructions

### 1. Update Protocol Package

#### File: `internal/protocol/message.go`

**Changes:**

```go
// OLD message types (remove MessageTypeData and MessageTypeDisconnect)
const (
    MessageTypeHandshake  = 0x01
    MessageTypeAccept     = 0x02
    MessageTypeReject     = 0x03
    MessageTypeData       = 0x04  // REMOVE
    MessageTypeConnect    = 0x05
    MessageTypeDisconnect = 0x06  // REMOVE
)

// NEW message types
const (
    MessageTypeHandshake       = 0x01  // Client → Server: Initial handshake with auth
    MessageTypeAccept          = 0x02  // Server → Client: Handshake accepted, includes public port
    MessageTypeReject          = 0x03  // Server → Client: Handshake rejected
    MessageTypeConnect         = 0x04  // Server → Client: New external connection (includes UUID)
    MessageTypeStreamHandshake = 0x05  // Client → Server: Identify which stream (includes UUID)
    MessageTypeStreamAccept    = 0x06  // Server → Client: Stream matched, ready for data
    MessageTypeStreamReject    = 0x07  // Server → Client: UUID not found or expired
)
```

**Note:** Keep the `Message` struct as-is for now. The `StreamID` field can be renamed to `Reserved` or kept for backward compatibility, but document that it's unused for new message types.

#### File: `internal/protocol/payloads.go`

**Add new payload types:**

```go
// ConnectPayload represents the connect message payload
type ConnectPayload struct {
    UUID string  // 36-character UUID string
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
    UUID string  // 36-character UUID string
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
```

### 2. Update Server Package

#### File: `internal/server/server.go`

**Step 1: Update imports**

```go
import (
    "fmt"
    "net"
    "sync"
    "time"
    
    "github.com/google/uuid"  // ADD THIS
    "github.com/viveksb007/go-bore/internal/protocol"
)
```

**Step 2: Update Server struct**

```go
// Server manages client connections and port tunneling
type Server struct {
    listenPort     int
    secret         string
    clients        map[string]*ClientSession
    clientsMu      sync.RWMutex
    portAllocator  *PortAllocator
    listener       net.Listener
    pendingStreams map[string]*PendingStream  // ADD THIS
    pendingMu      sync.RWMutex               // ADD THIS
}
```

**Step 3: Add PendingStream struct**

```go
// PendingStream represents an external connection waiting for client to open data connection
type PendingStream struct {
    externalConn net.Conn
    sessionID    string
    createdAt    time.Time
    timeout      *time.Timer
}
```

**Step 4: Update ClientSession struct**

```go
// ClientSession represents an active client connection
type ClientSession struct {
    id          string
    conn        net.Conn                      // Control connection
    publicPort  int
    listener    net.Listener
    streams     map[string]*ServerStream      // CHANGE: UUID -> ServerStream
    streamsMu   sync.RWMutex
}
```

**Step 5: Rename and update Stream struct**

```go
// ServerStream represents a proxied connection on the server side
type ServerStream struct {
    uuid         string
    dataConn     net.Conn      // Data connection to client
    externalConn net.Conn      // External client connection
    closeCh      chan struct{}
    createdAt    time.Time
}
```

**Step 6: Update NewServer**

```go
func NewServer(listenPort int, secret string) *Server {
    if listenPort == 0 {
        listenPort = 7835
    }

    return &Server{
        listenPort:     listenPort,
        secret:         secret,
        clients:        make(map[string]*ClientSession),
        portAllocator:  NewPortAllocator(10000, 60000),
        pendingStreams: make(map[string]*PendingStream),  // ADD THIS
    }
}
```

**Step 7: Update createSession**

```go
func (s *Server) createSession(conn net.Conn) *ClientSession {
    s.clientsMu.Lock()
    defer s.clientsMu.Unlock()

    sessionID := conn.RemoteAddr().String()

    session := &ClientSession{
        id:      sessionID,
        conn:    conn,
        streams: make(map[string]*ServerStream),  // CHANGE: UUID-based map
    }

    s.clients[sessionID] = session
    return session
}
```

**Step 8: Update acceptPublicConnections**

```go
func (s *Server) acceptPublicConnections(session *ClientSession) {
    defer func() {
        s.removeSession(session.id)
    }()

    for {
        conn, err := session.listener.Accept()
        if err != nil {
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
    }
}
```

**Step 9: Update sendConnect**

```go
func (s *Server) sendConnect(session *ClientSession, uuid string) error {
    connectPayload := &protocol.ConnectPayload{
        UUID: uuid,
    }

    payloadBytes, err := protocol.EncodeConnect(connectPayload)
    if err != nil {
        return fmt.Errorf("failed to encode connect payload: %w", err)
    }

    msg := &protocol.Message{
        Type:     protocol.MessageTypeConnect,
        StreamID: 0,  // Reserved/unused
        Payload:  payloadBytes,
    }

    if err := protocol.WriteMessage(session.conn, msg); err != nil {
        return fmt.Errorf("failed to write connect message: %w", err)
    }

    return nil
}
```

**Step 10: Add cleanupPendingStream**

```go
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
```

**Step 11: Remove obsolete functions**

- Remove `assignStreamID()` - no longer needed
- Update `Stream.close()` to `ServerStream.close()`

**Step 12: Update cleanup function**

```go
func (cs *ClientSession) cleanup(portAllocator *PortAllocator) {
    // Close all streams
    cs.streamsMu.Lock()
    for _, stream := range cs.streams {
        stream.close()
    }
    cs.streams = make(map[string]*ServerStream)  // CHANGE: UUID-based map
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
```

**Step 13: Update ServerStream.close()**

```go
func (s *ServerStream) close() {
    if s.dataConn != nil {
        s.dataConn.Close()
    }
    if s.externalConn != nil {
        s.externalConn.Close()
    }
    select {
    case <-s.closeCh:
        // Already closed
    default:
        close(s.closeCh)
    }
}
```

### 3. Update Tests

#### File: `internal/server/server_test.go`

**Changes needed:**

1. Update tests to use UUIDs instead of stream IDs
2. Update `TestSendConnect` to verify UUID payload
3. Update `TestAcceptPublicConnections` to expect UUID in connect message
4. Update `TestAcceptMultiplePublicConnections` to verify unique UUIDs
5. Add new test: `TestPendingStreamTimeout` to verify 30-second timeout
6. Add new test: `TestCleanupPendingStream`

#### File: `internal/protocol/payloads_test.go`

**Add new tests:**

1. `TestEncodeDecodeConnect` - Test ConnectPayload encoding/decoding
2. `TestEncodeDecodeStreamHandshake` - Test StreamHandshakePayload encoding/decoding
3. `TestConnectPayloadInvalidUUID` - Test validation of UUID length

---

## Migration Checklist

### Phase 1: Protocol Updates
- [ ] Update message type constants in `message.go`
- [ ] Add `ConnectPayload` to `payloads.go`
- [ ] Add `StreamHandshakePayload` to `payloads.go`
- [ ] Add encode/decode functions for new payloads
- [ ] Write tests for new payload types
- [ ] Run protocol tests: `go test ./internal/protocol`

### Phase 2: Server Updates
- [ ] Add `pendingStreams` map to Server struct
- [ ] Add `PendingStream` struct
- [ ] Update `ClientSession` to use UUID-based streams
- [ ] Rename `Stream` to `ServerStream` and update fields
- [ ] Update `NewServer` to initialize pendingStreams
- [ ] Update `createSession` for UUID-based map
- [ ] Rewrite `acceptPublicConnections` with UUID generation
- [ ] Update `sendConnect` to use `ConnectPayload`
- [ ] Add `cleanupPendingStream` function
- [ ] Remove `assignStreamID` function
- [ ] Update `ServerStream.close()` method
- [ ] Update `cleanup` function

### Phase 3: Test Updates
- [ ] Update existing tests for UUID-based approach
- [ ] Add test for pending stream timeout
- [ ] Add test for cleanup pending stream
- [ ] Add test for UUID validation
- [ ] Run server tests: `go test ./internal/server`

### Phase 4: Verification
- [ ] All protocol tests pass
- [ ] All server tests pass
- [ ] Manual testing: Start server, verify it accepts connections
- [ ] Verify pending streams timeout after 30 seconds
- [ ] Verify UUID format is correct (36 characters)

---

## Dependencies

### External Package Required

```bash
go get github.com/google/uuid
```

This package provides standard UUID v4 generation with proper randomness.

---

## Backward Compatibility Notes

**Breaking Changes:**
- Message type constants have changed
- Stream identification changed from uint32 to string (UUID)
- `MessageTypeData` and `MessageTypeDisconnect` removed

**Migration Path:**
- This is a protocol-level change
- Old clients will not work with new servers
- Recommend version bump to indicate breaking change

---

## Testing Strategy

### Unit Tests
1. Test UUID generation and validation
2. Test pending stream timeout mechanism
3. Test cleanup of expired pending streams
4. Test ConnectPayload encoding/decoding

### Integration Tests
1. Test full flow: external connect → UUID generation → pending stream → timeout
2. Test multiple concurrent connections with unique UUIDs
3. Test cleanup when client never opens data connection

---

## Estimated Effort

- Protocol updates: 1-2 hours
- Server updates: 2-3 hours
- Test updates: 2-3 hours
- Testing and verification: 1-2 hours

**Total: 6-10 hours**

---

## Next Steps After Updates

Once these updates are complete:

1. Mark Task 5 as fully updated
2. Proceed with Task 6 (data connection handling)
3. Implement connection type detection
4. Implement stream matching and raw TCP proxy

The foundation will be solid for the connection-per-stream architecture.
