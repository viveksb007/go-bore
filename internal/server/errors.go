package server

import (
	"errors"
	"fmt"
	"net"
	"strings"
)

// Exit codes for server errors
const (
	ExitCodeSuccess           = 0
	ExitCodePortInUse         = 1
	ExitCodePortAllocation    = 2
	ExitCodeProtocolError     = 3
	ExitCodeResourceExhausted = 4
	ExitCodeGenericError      = 5
)

// Error types for server operations
var (
	// ErrPortInUse indicates the server port is already in use
	ErrPortInUse = errors.New("port already in use")

	// ErrPortAllocationFailed indicates no ports are available for allocation
	ErrPortAllocationFailed = errors.New("port allocation failed")

	// ErrProtocolViolation indicates a client sent invalid protocol messages
	ErrProtocolViolation = errors.New("protocol violation")

	// ErrResourceExhausted indicates the server has run out of resources
	ErrResourceExhausted = errors.New("resource exhausted")

	// ErrMaxClientsReached indicates the maximum number of clients has been reached
	ErrMaxClientsReached = errors.New("maximum clients reached")
)

// ServerError wraps errors with additional context and exit codes
type ServerError struct {
	Op       string // Operation that failed
	Err      error  // Underlying error
	ExitCode int    // Suggested exit code
	Message  string // Human-readable message
}

func (e *ServerError) Error() string {
	if e.Message != "" {
		return e.Message
	}
	if e.Op != "" {
		return fmt.Sprintf("%s: %v", e.Op, e.Err)
	}
	return e.Err.Error()
}

func (e *ServerError) Unwrap() error {
	return e.Err
}

// NewPortInUseError creates an error for port already in use scenarios
func NewPortInUseError(port int, err error) *ServerError {
	return &ServerError{
		Op:       "listen",
		Err:      ErrPortInUse,
		ExitCode: ExitCodePortInUse,
		Message:  fmt.Sprintf("port %d is already in use", port),
	}
}

// NewPortAllocationError creates an error for port allocation failures
func NewPortAllocationError() *ServerError {
	return &ServerError{
		Op:       "allocate_port",
		Err:      ErrPortAllocationFailed,
		ExitCode: ExitCodePortAllocation,
		Message:  "no available ports for allocation",
	}
}

// NewProtocolViolationError creates an error for protocol violations
func NewProtocolViolationError(detail string) *ServerError {
	return &ServerError{
		Op:       "protocol",
		Err:      ErrProtocolViolation,
		ExitCode: ExitCodeProtocolError,
		Message:  fmt.Sprintf("protocol violation: %s", detail),
	}
}

// NewResourceExhaustedError creates an error for resource exhaustion
func NewResourceExhaustedError(resource string) *ServerError {
	return &ServerError{
		Op:       "resource",
		Err:      ErrResourceExhausted,
		ExitCode: ExitCodeResourceExhausted,
		Message:  fmt.Sprintf("resource exhausted: %s", resource),
	}
}

// IsPortInUse checks if the error is a port in use error
func IsPortInUse(err error) bool {
	if err == nil {
		return false
	}

	// Check for our custom error type
	var serverErr *ServerError
	if errors.As(err, &serverErr) {
		return errors.Is(serverErr.Err, ErrPortInUse)
	}

	// Check for network-level address in use
	var opErr *net.OpError
	if errors.As(err, &opErr) {
		return strings.Contains(opErr.Err.Error(), "address already in use")
	}

	return strings.Contains(err.Error(), "address already in use")
}

// IsPortAllocationFailed checks if the error is a port allocation failure
func IsPortAllocationFailed(err error) bool {
	if err == nil {
		return false
	}

	var serverErr *ServerError
	if errors.As(err, &serverErr) {
		return errors.Is(serverErr.Err, ErrPortAllocationFailed)
	}

	return errors.Is(err, ErrPortAllocationFailed)
}

// GetExitCode returns the appropriate exit code for an error
func GetExitCode(err error) int {
	if err == nil {
		return ExitCodeSuccess
	}

	var serverErr *ServerError
	if errors.As(err, &serverErr) {
		return serverErr.ExitCode
	}

	if IsPortInUse(err) {
		return ExitCodePortInUse
	}
	if IsPortAllocationFailed(err) {
		return ExitCodePortAllocation
	}

	return ExitCodeGenericError
}
