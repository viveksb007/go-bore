# Requirements Document

## Introduction

This feature implements a CLI tool called `gobore` that enables users to expose local ports to remote servers through secure tunneling. The system consists of two components: a client that runs on the local machine where the service is hosted, and a server that receives external traffic and tunnels it back to the client. This allows users to make locally-running services accessible from the internet without complex network configuration or port forwarding.

## Requirements

### Requirement 1: Client CLI Interface

**User Story:** As a developer, I want to run a simple command to expose my local port, so that I can quickly share my local service with others.

#### Acceptance Criteria

1. WHEN the user runs `gobore local <port> --to <server>` THEN the system SHALL establish a connection to the specified remote server
2. WHEN the user specifies a local port THEN the system SHALL validate that the port number is between 1 and 65535
3. WHEN the user provides the `--to` flag THEN the system SHALL accept either a domain name or IP address with optional port
4. WHEN the connection is established THEN the system SHALL display the public URL where the service is accessible
5. IF the local port is not listening THEN the system SHALL display an error message and exit gracefully
6. WHEN the user presses Ctrl+C THEN the system SHALL cleanly close the tunnel and exit

### Requirement 2: Server Component

**User Story:** As a system administrator, I want to run a server that accepts tunnel connections, so that I can provide tunneling services to clients.

#### Acceptance Criteria

1. WHEN the user runs `gobore server --port <port>` THEN the system SHALL start listening for client connections on the specified port
2. WHEN a client connects THEN the system SHALL authenticate the connection and allocate a public port for the tunnel
3. WHEN external traffic arrives on the allocated port THEN the system SHALL forward it through the tunnel to the connected client
4. WHEN a client disconnects THEN the system SHALL release the allocated port and clean up resources
5. IF the server port is already in use THEN the system SHALL display an error message and exit
6. WHEN multiple clients connect THEN the system SHALL handle each tunnel independently without interference

### Requirement 3: Tunnel Protocol

**User Story:** As a user, I want my data to be transmitted reliably through the tunnel, so that my application works correctly over the connection.

#### Acceptance Criteria

1. WHEN data is sent from the remote server to the client THEN the system SHALL preserve the exact byte sequence
2. WHEN data is sent from the client to the remote server THEN the system SHALL preserve the exact byte sequence
3. WHEN either end of the tunnel closes the connection THEN the system SHALL propagate the close to the other end
4. IF network errors occur on the control connection THEN the system SHALL attempt to reconnect with exponential backoff
5. WHEN the tunnel is established THEN the system SHALL use TCP for reliable data transmission
6. WHEN an external client connects THEN the system SHALL create a dedicated TCP connection for that stream (i.e., each external client connection to the public port gets its own dedicated TCP connection between server and client for data proxying)

### Requirement 4: Connection Management

**User Story:** As a user, I want the tunnel to handle multiple concurrent connections, so that my service can serve multiple clients simultaneously.

#### Acceptance Criteria

1. WHEN multiple external clients connect to the public port THEN the system SHALL create separate tunneled connections for each
2. WHEN a tunneled connection closes THEN the system SHALL not affect other active connections
3. WHEN the client receives a connection THEN the system SHALL forward it to the local port
4. IF the local service is slow to respond THEN the system SHALL buffer data appropriately without blocking other connections
5. WHEN connection limits are reached THEN the system SHALL reject new connections gracefully

### Requirement 5: Error Handling and Logging

**User Story:** As a user, I want clear error messages and status updates, so that I can troubleshoot issues quickly.

#### Acceptance Criteria

1. WHEN an error occurs THEN the system SHALL display a human-readable error message
2. WHEN the tunnel is established THEN the system SHALL log the public URL and connection status
3. WHEN verbose mode is enabled with `--verbose` flag THEN the system SHALL log detailed connection and data transfer information
4. IF authentication fails THEN the system SHALL display the reason and exit
5. WHEN connections are established or closed THEN the system SHALL log these events in verbose mode

### Requirement 6: Configuration Options

**User Story:** As a user, I want to configure tunnel behavior, so that I can customize it for my specific needs.

#### Acceptance Criteria

1. WHEN the user specifies `--secret <token>` THEN the system SHALL use the token for authentication
2. WHEN the server is started with `--port <port>` THEN the system SHALL listen on the specified port (default: 7835)
3. WHEN the client specifies `--local-host <host>` THEN the system SHALL connect to that host instead of localhost
4. IF no configuration is provided THEN the system SHALL use sensible defaults
5. WHEN the user runs `gobore --help` THEN the system SHALL display usage information for all commands and flags

### Requirement 7: Security

**User Story:** As a security-conscious user, I want basic authentication for tunnels, so that unauthorized users cannot create tunnels on my server.

#### Acceptance Criteria

1. WHEN the server is configured with a secret (e.g., `gobore server --secret mytoken123`) THEN the system SHALL reject clients without matching secrets
2. WHEN the client provides a secret (e.g., `gobore local 5000 --to example.com --secret mytoken123`) THEN the system SHALL send it during the initial handshake for authentication
3. WHEN authentication fails THEN the system SHALL close the connection immediately and display "Authentication failed" to the client
4. WHEN transmitting the secret THEN the system SHALL not log it in plain text
5. IF no secret is configured on the server THEN the system SHALL allow all connections (open mode)
6. WHEN secrets match THEN the system SHALL proceed with tunnel establishment

**Example Usage:**
```bash
# Server with authentication
gobore server --port 7835 --secret mytoken123

# Client connecting with matching secret
gobore local 5000 --to example.com:7835 --secret mytoken123
```
