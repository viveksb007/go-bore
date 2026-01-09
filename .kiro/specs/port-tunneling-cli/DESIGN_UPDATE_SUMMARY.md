# Design Update Summary

## Date: 2025-11-17

## Change: Multiplexed to Connection-Per-Stream Architecture

### Overview

The design has been updated from a **multiplexed single-connection** approach to a **connection-per-stream** approach. This change simplifies the protocol, improves stream isolation, and eliminates head-of-line blocking issues.

---

## Key Architectural Changes

### Before (Multiplexed)
```
External Client → Server → [Single Control Connection] → Client → Local Service
                           All streams multiplexed with stream IDs
```

### After (Connection-Per-Stream)
```
External Client → Server → [Control Connection (signaling)] → Client → Local Service
                       ↓
                  [Data Connection (per stream)] → Client
                       ↑
                  One dedicated TCP connection per external client
```

---

## Protocol Changes

### Message Types

**Removed:**
- `MessageTypeData` (0x04) - No longer needed, raw TCP used instead
- `MessageTypeDisconnect` (0x06) - TCP FIN/RST handles this naturally

**Modified:**
- `MessageTypeConnect` (0x04) - Now includes UUID instead of stream ID

**Added:**
- `MessageTypeStreamHandshake` (0x05) - Client identifies which stream
- `MessageTypeStreamAccept` (0x06) - Server confirms UUID match
- `MessageTypeStreamReject` (0x07) - Server rejects invalid/expired UUID

### Data Flow

**Before:**
```
External → Server → MessageTypeData(streamID, payload) → Client → Local
```

**After:**
```
External → Server → Raw TCP bytes → Client → Local
                    (no framing after handshake)
```

---

## Implementation Changes

### Server Side

**New Components:**
- `pendingStreams map[string]*PendingStream` - Tracks waiting external connections
- `PendingStream` struct with timeout (30 seconds)
- Connection type detection (control vs data)

**Modified Flow:**
1. External client connects → Generate UUID
2. Store external connection in `pendingStreams`
3. Send `MessageTypeConnect(UUID)` via control connection
4. Wait for client to open data connection with matching UUID
5. Match UUID, start raw TCP proxy with `io.Copy`

### Client Side

**Modified Flow:**
1. Receive `MessageTypeConnect(UUID)` on control connection
2. Spawn goroutine to handle new stream
3. Open new TCP connection to server (data connection)
4. Send `MessageTypeStreamHandshake(UUID)`
5. Wait for `MessageTypeStreamAccept`
6. Dial local service
7. Start raw TCP proxy with `io.Copy`

---

## Benefits of New Approach

### 1. Simplicity
- No multiplexing/demultiplexing logic needed
- Raw TCP proxying after handshake
- Simpler error handling

### 2. Better Isolation
- Each stream has independent TCP connection
- No head-of-line blocking
- TCP flow control works naturally per stream

### 3. Easier Debugging
- Can inspect individual connections with standard tools
- Each stream visible in netstat/tcpdump
- Clearer connection lifecycle

### 4. Better Performance for Long-Lived Connections
- Full TCP window per stream
- Independent congestion control
- No shared buffer contention

---

## Trade-offs

### Advantages
✅ No head-of-line blocking  
✅ Simpler protocol (raw TCP after handshake)  
✅ Better stream isolation  
✅ Easier to debug  
✅ Natural TCP flow control per stream  

### Disadvantages
❌ More TCP connections (N external = N+1 server-client connections)  
❌ Higher connection overhead (TCP handshake per stream)  
❌ More file descriptors used  
❌ Slightly higher latency for first byte (new TCP handshake)  

---

## Files Updated

### Design Documents
- ✅ `.kiro/specs/port-tunneling-cli/design.md` - Updated architecture, added alternative approaches section
- ✅ `.kiro/specs/port-tunneling-cli/requirements.md` - Updated requirement 3.6
- ✅ `.kiro/specs/port-tunneling-cli/tasks.md` - Updated tasks 5, 6, 9, 10, 15

### Implementation Files
- ⏳ `internal/protocol/message.go` - Needs update for new message types
- ⏳ `internal/protocol/payload.go` - Needs new payload types (ConnectPayload, StreamHandshakePayload)
- ⏳ `internal/server/server.go` - Needs major refactoring for connection-per-stream
- ⏳ `internal/client/client.go` - Needs implementation (not yet created)

---

## Next Steps

### Immediate Actions Required

1. **Update Protocol Package**
   - Add new message type constants
   - Implement ConnectPayload encoding/decoding
   - Implement StreamHandshakePayload encoding/decoding
   - Remove MessageTypeData and MessageTypeDisconnect

2. **Refactor Server Implementation**
   - Add pendingStreams map and PendingStream struct
   - Implement connection type detection
   - Refactor acceptPublicConnections to use UUIDs
   - Implement handleDataConnection for stream matching
   - Replace multiplexing with raw TCP proxy (io.Copy)

3. **Update Tests**
   - Modify existing tests for new protocol
   - Add tests for UUID matching
   - Add tests for timeout scenarios
   - Add tests for connection type detection

4. **Implement Client**
   - Follow new connection-per-stream approach
   - Implement handleNewStream(UUID)
   - Implement stream handshake
   - Implement raw TCP proxy

---

## Alternative Approach (Documented)

The multiplexed single-connection approach has been documented in the design document under "Alternative Approaches Considered" section. This provides:

- Complete description of the multiplexed approach
- Advantages and disadvantages comparison
- Performance comparison table
- Guidance on when each approach is better
- Rationale for choosing connection-per-stream

This documentation ensures the design decision is well-understood and can be revisited if requirements change.

---

## Migration Notes

### For Task 5 (Already Implemented)
The current implementation needs to be updated:
- Replace stream ID generation with UUID generation
- Add pendingStreams map
- Add timeout mechanism (30 seconds)
- Update tests to use UUIDs

### For Future Tasks
- Task 6: Completely new approach (connection type detection + data connection handler)
- Task 9: Simplified (control connection only receives signals)
- Task 10: New approach (open data connection, handshake, raw proxy)

---

## Questions & Decisions

### Q: Why 30-second timeout for pending streams?
**A:** Balances between giving client enough time to establish data connection and preventing memory leaks from abandoned connections.

### Q: What happens if UUID collision occurs?
**A:** Standard UUIDs (v4) have negligible collision probability. Implementation uses `crypto/rand` for generation.

### Q: Can we support both approaches?
**A:** Possible but adds complexity. Current design focuses on connection-per-stream for simplicity and better isolation.

### Q: Why separate Stream structs for client and server?
**A:** Better readability and type safety. Server manages `externalConn` (from public port) while client manages `localConn` (to local service). Separate structs make this distinction clear and prevent accidentally using wrong connection types.

### Q: What about connection limits?
**A:** Modern systems handle thousands of connections. For extreme cases (>10k concurrent), multiplexed approach would be better (documented as alternative).

---

## Conclusion

The connection-per-stream approach provides a simpler, more robust design that's easier to implement, debug, and maintain. The trade-off of additional TCP connections is acceptable for a general-purpose tunneling tool, and the benefits of stream isolation and simplified protocol outweigh the costs.

The multiplexed approach remains documented as a viable alternative for specific use cases (high connection rate, resource constraints, strict firewalls).
