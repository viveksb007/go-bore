package server

import (
	"fmt"
	"math/rand"
	"sync"
)

// PortAllocator manages allocation and release of ports within a specified range.
type PortAllocator struct {
	minPort   int
	maxPort   int
	allocated map[int]bool
	mu        sync.Mutex
}

// NewPortAllocator creates a new PortAllocator with the specified port range.
func NewPortAllocator(minPort, maxPort int) *PortAllocator {
	return &PortAllocator{
		minPort:   minPort,
		maxPort:   maxPort,
		allocated: make(map[int]bool),
	}
}

// Allocate finds and allocates an available port in the configured range.
// Returns an error if no ports are available.
func (pa *PortAllocator) Allocate() (int, error) {
	pa.mu.Lock()
	defer pa.mu.Unlock()

	// Calculate total available ports
	totalPorts := pa.maxPort - pa.minPort + 1
	if len(pa.allocated) >= totalPorts {
		return 0, fmt.Errorf("no available ports in range %d-%d", pa.minPort, pa.maxPort)
	}

	// Try random ports to avoid sequential allocation patterns
	maxAttempts := 100
	for i := 0; i < maxAttempts; i++ {
		port := pa.minPort + rand.Intn(totalPorts)
		if !pa.allocated[port] {
			pa.allocated[port] = true
			return port, nil
		}
	}

	// Fallback: linear search for available port
	for port := pa.minPort; port <= pa.maxPort; port++ {
		if !pa.allocated[port] {
			pa.allocated[port] = true
			return port, nil
		}
	}

	return 0, fmt.Errorf("no available ports in range %d-%d", pa.minPort, pa.maxPort)
}

// Release frees a previously allocated port.
func (pa *PortAllocator) Release(port int) {
	pa.mu.Lock()
	defer pa.mu.Unlock()
	delete(pa.allocated, port)
}
