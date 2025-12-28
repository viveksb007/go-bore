# gobore

A TCP port tunneling tool written in Go, inspired by [bore](https://github.com/ekzhang/bore).

`gobore` allows you to expose local ports to remote servers through secure tunneling. This is useful for sharing local development servers, testing webhooks, or accessing services behind NAT/firewalls.

## Installation

### From Source

```bash
# Clone the repository
git clone https://github.com/viveksb007/go-bore.git
cd go-bore

# Build the binary
go build -o gobore ./cmd/bore
```

## Quick Start

### 1. Start the Server

On a publicly accessible machine:

```bash
gobore server --port 7835
```

### 2. Expose a Local Port

On your local machine:

```bash
gobore client 8080 --to your-server.com:7835
```

This exposes your local port 8080 through the server. You'll see output like:

```
Listening on your-server.com:12345
```

Now anyone can access your local service at `your-server.com:12345`.

## Usage

### Server Command

Start a bore server that accepts tunnel connections from clients.

```bash
gobore server [flags]
```

#### Flags

| Flag | Short | Default | Description |
|------|-------|---------|-------------|
| `--port` | `-p` | `7835` | Port to listen for client connections |
| `--secret` | `-s` | (none) | Secret for client authentication |
| `--verbose` | `-v` | `false` | Enable verbose output (DEBUG level logging) |

#### Examples

```bash
# Start server on default port (7835), open mode
gobore server

# Start server on custom port
gobore server --port 9000

# Start server with authentication
gobore server --secret mytoken123

# Start server with verbose logging
gobore server --verbose
```

### Client Command

Expose a local port through a tunnel to a remote bore server.

```bash
gobore client <port> [flags]
```

#### Arguments

| Argument | Required | Description |
|----------|----------|-------------|
| `port` | Yes | Local port to expose (1-65535) |

#### Flags

| Flag | Short | Default | Description |
|------|-------|---------|-------------|
| `--to` | `-t` | (required) | Server address (host:port) |
| `--secret` | `-s` | (none) | Secret for authentication |
| `--local-host` | `-l` | `localhost` | Local host to forward to |
| `--verbose` | `-v` | `false` | Enable verbose output (DEBUG level logging) |

#### Examples

```bash
# Expose local port 8080
gobore client 8080 --to example.com:7835

# Expose with authentication
gobore client 8080 --to example.com:7835 --secret mytoken123

# Forward to a different local host
gobore client 8080 --to example.com:7835 --local-host 192.168.1.100

# Expose with verbose logging
gobore client 8080 --to example.com:7835 --verbose
```

## Architecture


`gobore` uses a connection-per-stream architecture with two types of TCP connections:

1. **Control Connection**: A persistent connection between client and server for signaling (handshake, authentication, connection notifications)
2. **Data Connections**: One dedicated TCP connection per external client, established on-demand for raw data proxying

```
┌─────────────────┐     ┌─────────────────┐     ┌─────────────────┐     ┌─────────────────┐
│ External Client │────▶│   Bore Server   │────▶│   Bore Client   │────▶│  Local Service  │
│                 │     │  (public port)  │     │                 │     │  (e.g., :8080)  │
└─────────────────┘     └─────────────────┘     └─────────────────┘     └─────────────────┘
                              │                        │
                              │    Control Connection  │
                              │◀──────────────────────▶│
                              │    (signaling only)    │
                              │                        │
                              │    Data Connection(s)  │
                              │◀──────────────────────▶│
                              │    (raw TCP proxy)     │
```

### Connection Flow

1. Client establishes a control connection to the server
2. Client sends handshake with optional authentication secret
3. Server allocates a public port and starts listening
4. Server sends accept message with the allocated port
5. When an external client connects to the public port:
   - Server generates a UUID for the connection
   - Server sends a connect message to the client via control connection
   - Client opens a new data connection to the server
   - Client connects to the local service
   - Bidirectional TCP proxying begins

## Authentication

`gobore` supports optional shared-secret authentication to prevent unauthorized tunnel creation.

### Server with Authentication

```bash
gobore server --secret mytoken123
```

### Client with Authentication

```bash
gobore client 8080 --to example.com:7835 --secret mytoken123
```

If the secrets don't match, the connection is rejected with "Authentication failed".

### Open Mode

If no secret is configured on the server, all connections are allowed:

```bash
# Server in open mode
gobore server

# Client can connect without secret
gobore client 8080 --to example.com:7835
```

## Use Cases

### Share Local Development Server

```bash
# You're running a web app on localhost:3000
gobore client 3000 --to your-server.com:7835
# Share the public URL with teammates
```

### Test Webhooks

```bash
# Expose your webhook handler
gobore client 8080 --to your-server.com:7835
# Configure the webhook provider with the public URL
```

### Access Services Behind NAT

```bash
# Expose SSH on a machine behind NAT
gobore client 22 --to your-server.com:7835
# Connect via: ssh -p <public-port> user@your-server.com
```

### Forward to Docker Container

```bash
# Forward to a container on the Docker network
gobore client 80 --to your-server.com:7835 --local-host 172.17.0.2
```

## Acknowledgments

Inspired by [bore](https://github.com/ekzhang/bore) by Eric Zhang.
