# Implementation Plan

- [x] 1. Set up project structure and protocol foundation
  - Create Go module with proper directory structure (internal/protocol, internal/client, internal/server, cmd/gobore)
  - Define protocol message types and constants
  - Implement message frame encoding/decoding functions (WriteMessage, ReadMessage)
  - Write unit tests for message serialization/deserialization
  - _Requirements: 3.1, 3.2, 3.5_

- [x] 2. Implement port allocator
  - Create PortAllocator struct with allocation tracking
  - Implement Allocate() method to find available ports in range
  - Implement Release() method to free allocated ports
  - Write unit tests for allocation, release, and exhaustion scenarios
  - Write unit tests for concurrent allocation
  - _Requirements: 2.2, 2.4_

- [x] 3. Implement basic server structure
  - [x] 3.1 Create Server struct and initialization
    - Define Server struct with configuration fields
    - Implement NewServer() constructor with default values
    - Implement Run() method to start main listener
    - Write test for server initialization
    - _Requirements: 2.1, 6.2_
  
  - [x] 3.2 Implement client session management
    - Create ClientSession struct
    - Implement session creation and cleanup logic
    - Add session tracking in Server.clients map with mutex protection
    - Write tests for session lifecycle
    - _Requirements: 2.2, 2.4, 2.6_

- [x] 4. Implement server handshake and authentication
  - Implement handleClient() method to process incoming client connections
  - Implement handshake message reading and parsing
  - Implement authentication logic (compare secrets)
  - Send MessageTypeAccept or MessageTypeReject based on auth result
  - Write tests for successful and failed authentication
  - _Requirements: 2.2, 7.1, 7.2, 7.3, 7.5, 7.6_

- [x] 5. Implement server public port listener
  - Allocate public port after successful authentication
  - Create listener on allocated port
  - Implement acceptPublicConnections() goroutine to accept external connections
  - Generate UUID for each new external connection
  - Store pending connection in pendingStreams map with 30-second timeout
  - Send MessageTypeConnect(UUID) to client via control connection
  - Write tests for port allocation and listener creation
  - _Requirements: 2.2, 2.3, 4.1_

- [x] 6. Implement server data connection handling
  - [x] 6.1 Implement connection type detection
    - Modify server to detect connection type based on first message
    - Route MessageTypeHandshake to control connection handler
    - Route MessageTypeStreamHandshake to data connection handler
    - Write tests for connection type detection
    - _Requirements: 3.6, 4.1_
  
  - [x] 6.2 Implement data connection handler
    - Read MessageTypeStreamHandshake(UUID) from incoming connection
    - Look up UUID in pendingStreams map
    - If found: send MessageTypeStreamAccept, remove from pending, cancel timeout
    - If not found/expired: send MessageTypeStreamReject, close connection
    - Create Stream struct and register in ClientSession
    - Write tests for UUID matching and rejection scenarios
    - _Requirements: 3.6, 4.1, 4.2_
  
  - [x] 6.3 Implement bidirectional raw TCP proxy on server
    - After successful stream handshake, start two goroutines
    - Goroutine 1: io.Copy(dataConn, externalConn) - external → client
    - Goroutine 2: io.Copy(externalConn, dataConn) - client → external
    - Implement proper cleanup when either connection closes
    - Write tests for bidirectional data forwarding
    - _Requirements: 2.3, 3.1, 3.2, 3.3, 4.3, 4.4_

- [x] 7. Implement basic client structure
  - Create Client struct with configuration fields
  - Implement NewClient() constructor
  - Implement Connect() method to establish control connection to server
  - Write test for client initialization and connection
  - _Requirements: 1.1, 1.3_

- [x] 8. Implement client handshake
  - Implement sendHandshake() method to send MessageTypeHandshake
  - Implement receiveAccept() method to read MessageTypeAccept or MessageTypeReject
  - Parse public port from accept message
  - Display public URL to user
  - Handle authentication failure and exit gracefully
  - Write tests for handshake success and failure scenarios
  - _Requirements: 1.1, 1.4, 7.2, 7.3_

- [x] 9. Implement client message loop
  - Implement Run() method with message reading loop on control connection
  - Handle MessageTypeConnect(UUID): spawn goroutine to handle new stream
  - Keep control connection alive for receiving signals
  - Write tests for message handling
  - _Requirements: 3.1, 3.2, 3.3, 4.1, 4.3_

- [x] 10. Implement client stream management
  - [x] 10.1 Implement stream connection handler
    - Create handleNewStream(UUID) method spawned when MessageTypeConnect arrives
    - Open new TCP connection to server (data connection)
    - Send MessageTypeStreamHandshake(UUID)
    - Wait for MessageTypeStreamAccept or MessageTypeStreamReject
    - If rejected: log error and exit goroutine
    - Create Stream struct and register in client
    - Write tests for stream handshake success and failure
    - _Requirements: 3.6, 4.1, 4.2_
  
  - [x] 10.2 Implement bidirectional raw TCP proxy on client
    - After successful stream handshake, dial local service (localHost:localPort)
    - Start two goroutines for bidirectional proxy
    - Goroutine 1: io.Copy(dataConn, localConn) - local → server
    - Goroutine 2: io.Copy(localConn, dataConn) - server → local
    - Implement proper cleanup when either connection closes
    - Write tests for data forwarding and cleanup
    - _Requirements: 3.6, 4.1, 4.2, 4.3, 4.4_

- [x] 11. Implement CLI using cobra
  - [x] 11.1 Set up cobra command structure
    - Create root command with version and help
    - Create `server` subcommand
    - Create `local` subcommand
    - Add global `--verbose` flag
    - Write tests for command parsing
    - _Requirements: 1.1, 2.1, 6.5_
  
  - [x] 11.2 Implement server command flags and execution
    - Add `--port` flag with default 7835
    - Add `--secret` flag for authentication
    - Implement command execution to create and run server
    - Handle errors and display appropriate messages
    - Write tests for server command with various flag combinations
    - _Requirements: 2.1, 6.2, 7.1_
  
  - [x] 11.3 Implement local command flags and execution
    - Parse positional port argument
    - Add `--to` flag (required) for server address
    - Add `--secret` flag for authentication
    - Add `--local-host` flag with default "localhost"
    - Validate port number (1-65535)
    - Implement command execution to create and run client
    - Handle errors and display appropriate messages
    - Write tests for local command with various flag combinations
    - _Requirements: 1.1, 1.2, 1.3, 6.1, 6.3, 7.2_

- [x] 12. Implement logging system
  - Set up structured logging using log/slog
  - Implement log levels (INFO, DEBUG, ERROR)
  - Add verbose mode support that enables DEBUG level
  - Add logging for connection events (client connect/disconnect, stream create/close)
  - Add logging for authentication events
  - Ensure secrets are not logged in plain text
  - Write tests to verify logging behavior
  - _Requirements: 5.1, 5.2, 5.3, 5.4, 5.5, 7.4_

- [x] 13. Implement error handling and graceful shutdown
  - [x] 13.1 Implement client error handling
    - Handle connection refused errors
    - Handle authentication failures
    - Handle protocol errors (malformed messages)
    - Display human-readable error messages
    - Exit with appropriate exit codes
    - Write tests for error scenarios
    - _Requirements: 1.5, 5.1, 5.4_
  
  - [x] 13.2 Implement client graceful shutdown
    - Set up signal handling for Ctrl+C (SIGINT, SIGTERM)
    - Close all active streams
    - Close control connection
    - Ensure clean exit
    - Write tests for shutdown behavior
    - _Requirements: 1.6_
  
  - [x] 13.3 Implement server error handling
    - Handle port already in use errors
    - Handle port allocation failures
    - Handle client protocol violations
    - Handle resource exhaustion
    - Display appropriate error messages
    - Write tests for error scenarios
    - _Requirements: 2.5, 4.5, 5.1_
  
  - [x] 13.4 Implement server graceful shutdown
    - Set up signal handling for Ctrl+C
    - Close all client sessions
    - Release all allocated ports
    - Close main listener
    - Write tests for shutdown behavior
    - _Requirements: 2.4_

- [ ] 14. Implement reconnection logic with exponential backoff
  - Detect network errors during tunneling
  - Implement exponential backoff (1s, 2s, 4s, max 30s)
  - Attempt to reconnect to server
  - Re-establish handshake after reconnection
  - Log reconnection attempts
  - Write tests for reconnection behavior
  - _Requirements: 3.4_

- [ ] 15. Add input validation
  - Validate port numbers are in range 1-65535
  - Validate message lengths are within limits
  - Validate UUID format (36 characters, standard UUID format)
  - Validate server address format
  - Write tests for validation logic
  - _Requirements: 1.2, 1.3_

- [x] 16. Write integration tests
  - [x] 16.1 Test end-to-end tunneling
    - Start local HTTP test server
    - Start bore server
    - Start bore client
    - Make HTTP request through tunnel
    - Verify response matches direct connection
    - _Requirements: 3.1, 3.2, 4.3_
  
  - [x] 16.2 Test multiple concurrent connections
    - Create multiple external connections simultaneously
    - Verify data isolation between streams
    - Verify all connections work correctly
    - _Requirements: 4.1, 4.2_
  
  - [x] 16.3 Test connection lifecycle
    - Test graceful shutdown of client
    - Test graceful shutdown of server
    - Test abrupt disconnection handling
    - _Requirements: 3.3, 4.2_
  
  - [x] 16.4 Test authentication scenarios
    - Test successful authentication with matching secrets
    - Test failed authentication with wrong secret
    - Test open mode (no secret)
    - _Requirements: 7.1, 7.2, 7.3, 7.5, 7.6_

- [x] 17. Create README and documentation
  - Write README with installation instructions
  - Add usage examples for both server and client
  - Document all CLI flags and options
  - Add troubleshooting section
  - Include architecture diagram
  - _Requirements: 6.5_
