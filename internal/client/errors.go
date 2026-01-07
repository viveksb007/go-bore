package client

import (
	"errors"
	"fmt"
	"net"
	"strings"
)

// Exit codes for client errors
const (
	ExitCodeSuccess           = 0
	ExitCodeConnectionRefused = 1
	ExitCodeAuthFailed        = 2
	ExitCodeProtocolError     = 3
	ExitCodeLocalServiceError = 4
	ExitCodeGenericError      = 5
)

// Error types for client operations
var (
	// ErrConnectionRefused indicates the server is not reachable
	ErrConnectionRefused = errors.New("connection refused")

	// ErrAuthFailed indicates authentication with the server failed
	ErrAuthFailed = errors.New("authentication failed")

	// ErrProtocolError indicates a protocol-level error (malformed messages, unexpected types)
	ErrProtocolError = errors.New("protocol error")

	// ErrLocalServiceUnavailable indicates the local service is not reachable
	ErrLocalServiceUnavailable = errors.New("local service unavailable")

	// ErrNotConnected indicates the client is not connected
	ErrNotConnected = errors.New("client not connected")

	// ErrServerClosed indicates the server closed the connection
	ErrServerClosed = errors.New("server closed connection")
)

// ClientError wraps errors with additional context and exit codes
type ClientError struct {
	Op       string // Operation that failed
	Err      error  // Underlying error
	ExitCode int    // Suggested exit code
	Message  string // Human-readable message
}

func (e *ClientError) Error() string {
	if e.Message != "" {
		return e.Message
	}
	if e.Op != "" {
		return fmt.Sprintf("%s: %v", e.Op, e.Err)
	}
	return e.Err.Error()
}

func (e *ClientError) Unwrap() error {
	return e.Err
}

// NewConnectionRefusedError creates an error for connection refused scenarios
func NewConnectionRefusedError(serverAddr string, err error) *ClientError {
	return &ClientError{
		Op:       "connect",
		Err:      ErrConnectionRefused,
		ExitCode: ExitCodeConnectionRefused,
		Message:  fmt.Sprintf("could not connect to server at %s: connection refused", serverAddr),
	}
}

// NewAuthFailedError creates an error for authentication failures
func NewAuthFailedError() *ClientError {
	return &ClientError{
		Op:       "authenticate",
		Err:      ErrAuthFailed,
		ExitCode: ExitCodeAuthFailed,
		Message:  "authentication failed: server rejected the provided secret",
	}
}

// NewProtocolError creates an error for protocol-level issues
func NewProtocolError(op string, detail string) *ClientError {
	return &ClientError{
		Op:       op,
		Err:      ErrProtocolError,
		ExitCode: ExitCodeProtocolError,
		Message:  fmt.Sprintf("protocol error during %s: %s", op, detail),
	}
}

// NewLocalServiceError creates an error for local service connection issues
func NewLocalServiceError(localAddr string, err error) *ClientError {
	return &ClientError{
		Op:       "connect_local",
		Err:      ErrLocalServiceUnavailable,
		ExitCode: ExitCodeLocalServiceError,
		Message:  fmt.Sprintf("could not connect to local service at %s: %v", localAddr, err),
	}
}

// NewServerClosedError creates an error when server closes the connection
func NewServerClosedError() *ClientError {
	return &ClientError{
		Op:       "read",
		Err:      ErrServerClosed,
		ExitCode: ExitCodeGenericError,
		Message:  "server closed the connection",
	}
}

// IsConnectionRefused checks if the error is a connection refused error
func IsConnectionRefused(err error) bool {
	if err == nil {
		return false
	}

	// Check for our custom error type
	var clientErr *ClientError
	if errors.As(err, &clientErr) {
		return errors.Is(clientErr.Err, ErrConnectionRefused)
	}

	// Check for network-level connection refused
	var opErr *net.OpError
	if errors.As(err, &opErr) {
		return opErr.Op == "dial" && isConnectionRefusedSyscall(opErr.Err)
	}

	return false
}

// IsAuthFailed checks if the error is an authentication failure
func IsAuthFailed(err error) bool {
	if err == nil {
		return false
	}

	var clientErr *ClientError
	if errors.As(err, &clientErr) {
		return errors.Is(clientErr.Err, ErrAuthFailed)
	}

	return errors.Is(err, ErrAuthFailed)
}

// IsProtocolError checks if the error is a protocol error
func IsProtocolError(err error) bool {
	if err == nil {
		return false
	}

	var clientErr *ClientError
	if errors.As(err, &clientErr) {
		return errors.Is(clientErr.Err, ErrProtocolError)
	}

	return errors.Is(err, ErrProtocolError)
}

// GetExitCode returns the appropriate exit code for an error
func GetExitCode(err error) int {
	if err == nil {
		return ExitCodeSuccess
	}

	var clientErr *ClientError
	if errors.As(err, &clientErr) {
		return clientErr.ExitCode
	}

	// Check for specific error types
	if IsConnectionRefused(err) {
		return ExitCodeConnectionRefused
	}
	if IsAuthFailed(err) {
		return ExitCodeAuthFailed
	}
	if IsProtocolError(err) {
		return ExitCodeProtocolError
	}

	return ExitCodeGenericError
}

// isConnectionRefusedSyscall checks if the underlying error is a connection refused syscall error
func isConnectionRefusedSyscall(err error) bool {
	if err == nil {
		return false
	}
	// Check error message for "connection refused" as a fallback
	return strings.Contains(err.Error(), "connection refused")
}
