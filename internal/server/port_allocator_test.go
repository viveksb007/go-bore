package server

import (
	"sync"
	"testing"
)

func TestPortAllocator_Allocate(t *testing.T) {
	pa := NewPortAllocator(10000, 10010)

	port, err := pa.Allocate()
	if err != nil {
		t.Fatalf("expected successful allocation, got error: %v", err)
	}

	if port < 10000 || port > 10010 {
		t.Errorf("allocated port %d is outside range 10000-10010", port)
	}

	// Verify port is marked as allocated
	if !pa.allocated[port] {
		t.Errorf("port %d should be marked as allocated", port)
	}
}

func TestPortAllocator_Release(t *testing.T) {
	pa := NewPortAllocator(10000, 10010)

	port, err := pa.Allocate()
	if err != nil {
		t.Fatalf("expected successful allocation, got error: %v", err)
	}

	// Release the port
	pa.Release(port)

	// Verify port is no longer allocated
	if pa.allocated[port] {
		t.Errorf("port %d should not be marked as allocated after release", port)
	}

	// Should be able to allocate the same port again
	port2, err := pa.Allocate()
	if err != nil {
		t.Fatalf("expected successful allocation after release, got error: %v", err)
	}

	// With a small range, we should eventually get the same port
	found := false
	if port2 == port {
		found = true
	} else {
		// Try a few more times since allocation is random
		for i := 0; i < 20; i++ {
			pa.Release(port2)
			port2, _ = pa.Allocate()
			if port2 == port {
				found = true
				break
			}
		}
	}

	if !found {
		t.Logf("note: released port %d was not reallocated (this is okay with random allocation)", port)
	}
}

func TestPortAllocator_Exhaustion(t *testing.T) {
	// Create allocator with small range
	pa := NewPortAllocator(10000, 10002) // Only 3 ports available

	// Allocate all ports
	ports := make([]int, 0, 3)
	for i := 0; i < 3; i++ {
		port, err := pa.Allocate()
		if err != nil {
			t.Fatalf("expected successful allocation %d, got error: %v", i+1, err)
		}
		ports = append(ports, port)
	}

	// Verify all ports are unique
	seen := make(map[int]bool)
	for _, port := range ports {
		if seen[port] {
			t.Errorf("duplicate port allocated: %d", port)
		}
		seen[port] = true
	}

	// Next allocation should fail
	_, err := pa.Allocate()
	if err == nil {
		t.Error("expected allocation to fail when all ports are exhausted")
	}

	// Release one port
	pa.Release(ports[0])

	// Should be able to allocate again
	port, err := pa.Allocate()
	if err != nil {
		t.Fatalf("expected successful allocation after release, got error: %v", err)
	}

	if port < 10000 || port > 10002 {
		t.Errorf("allocated port %d is outside range 10000-10002", port)
	}

	// Should fail again
	_, err = pa.Allocate()
	if err == nil {
		t.Error("expected allocation to fail when all ports are exhausted again")
	}
}

func TestPortAllocator_ConcurrentAllocation(t *testing.T) {
	pa := NewPortAllocator(10000, 10100) // 101 ports available

	numGoroutines := 50
	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	// Channel to collect allocated ports
	portsCh := make(chan int, numGoroutines)
	errorsCh := make(chan error, numGoroutines)

	// Allocate ports concurrently
	for i := 0; i < numGoroutines; i++ {
		go func() {
			defer wg.Done()
			port, err := pa.Allocate()
			if err != nil {
				errorsCh <- err
				return
			}
			portsCh <- port
		}()
	}

	wg.Wait()
	close(portsCh)
	close(errorsCh)

	// Check for errors
	for err := range errorsCh {
		t.Errorf("unexpected error during concurrent allocation: %v", err)
	}

	// Collect all allocated ports
	ports := make([]int, 0, numGoroutines)
	for port := range portsCh {
		ports = append(ports, port)
	}

	// Verify all ports are unique
	seen := make(map[int]bool)
	for _, port := range ports {
		if seen[port] {
			t.Errorf("duplicate port allocated during concurrent allocation: %d", port)
		}
		seen[port] = true

		if port < 10000 || port > 10100 {
			t.Errorf("allocated port %d is outside range 10000-10100", port)
		}
	}

	if len(ports) != numGoroutines {
		t.Errorf("expected %d unique ports, got %d", numGoroutines, len(ports))
	}
}

func TestPortAllocator_ConcurrentReleaseAndAllocate(t *testing.T) {
	pa := NewPortAllocator(10000, 10020) // 21 ports available

	numOperations := 100
	var wg sync.WaitGroup
	wg.Add(numOperations)

	// Pre-allocate some ports
	initialPorts := make([]int, 10)
	for i := 0; i < 10; i++ {
		port, err := pa.Allocate()
		if err != nil {
			t.Fatalf("failed to pre-allocate port: %v", err)
		}
		initialPorts[i] = port
	}

	// Concurrently allocate and release ports
	for i := 0; i < numOperations; i++ {
		go func(idx int) {
			defer wg.Done()

			if idx%2 == 0 {
				// Allocate
				_, err := pa.Allocate()
				if err != nil {
					// It's okay to fail if ports are exhausted
					return
				}
			} else {
				// Release one of the initial ports
				if idx/2 < len(initialPorts) {
					pa.Release(initialPorts[idx/2])
				}
			}
		}(i)
	}

	wg.Wait()

	// Verify allocator is still functional
	pa.mu.Lock()
	allocatedCount := len(pa.allocated)
	pa.mu.Unlock()

	if allocatedCount < 0 || allocatedCount > 21 {
		t.Errorf("invalid allocated count after concurrent operations: %d", allocatedCount)
	}
}

func TestPortAllocator_ReleaseUnallocatedPort(t *testing.T) {
	pa := NewPortAllocator(10000, 10010)

	// Release a port that was never allocated
	pa.Release(10005)

	// Should not cause any issues
	port, err := pa.Allocate()
	if err != nil {
		t.Fatalf("expected successful allocation, got error: %v", err)
	}

	if port < 10000 || port > 10010 {
		t.Errorf("allocated port %d is outside range 10000-10010", port)
	}
}

func TestPortAllocator_MultipleReleaseSamePort(t *testing.T) {
	pa := NewPortAllocator(10000, 10010)

	port, err := pa.Allocate()
	if err != nil {
		t.Fatalf("expected successful allocation, got error: %v", err)
	}

	// Release the same port multiple times
	pa.Release(port)
	pa.Release(port)
	pa.Release(port)

	// Should not cause any issues
	port2, err := pa.Allocate()
	if err != nil {
		t.Fatalf("expected successful allocation, got error: %v", err)
	}

	if port2 < 10000 || port2 > 10010 {
		t.Errorf("allocated port %d is outside range 10000-10010", port2)
	}
}
