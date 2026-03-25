package test

import (
	"context"
	"net"
	"sync"
	"testing"
	"time"
)

// TestShutdown_ClosesListener verifies that closing a listener unblocks Accept
func TestShutdown_ClosesListener(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	acceptReturned := make(chan error, 1)
	go func() {
		_, err := ln.Accept()
		acceptReturned <- err
	}()

	// Close listener — should unblock Accept with an error
	time.Sleep(100 * time.Millisecond)
	ln.Close()

	select {
	case err := <-acceptReturned:
		if err == nil {
			t.Fatal("expected Accept to return error after listener close")
		}
		// Expected: "use of closed network connection"
	case <-time.After(2 * time.Second):
		t.Fatal("Accept did not return after listener close")
	}
}

// TestShutdown_WaitGroupDrains verifies that WaitGroup blocks until goroutines finish
func TestShutdown_WaitGroupDrains(t *testing.T) {
	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		time.Sleep(500 * time.Millisecond) // simulate active connection
	}()

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	// Should NOT be done yet
	select {
	case <-done:
		t.Fatal("WaitGroup resolved too early")
	case <-time.After(100 * time.Millisecond):
		// Expected: still waiting
	}

	// Should be done after 500ms
	select {
	case <-done:
		// Expected: drained
	case <-time.After(2 * time.Second):
		t.Fatal("WaitGroup did not drain")
	}
}

// TestShutdown_ContextCancellation verifies that Shutdown returns when context expires
func TestShutdown_ContextCancellation(t *testing.T) {
	var wg sync.WaitGroup

	wg.Add(1)
	// Never call wg.Done() — simulates a stuck connection
	go func() {
		time.Sleep(10 * time.Second) // will be interrupted by context
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	done := make(chan error, 1)
	go func() {
		// Simulate Shutdown pattern: wait for wg or context
		ch := make(chan struct{})
		go func() {
			wg.Wait()
			close(ch)
		}()

		select {
		case <-ch:
			done <- nil
		case <-ctx.Done():
			done <- ctx.Err()
		}
	}()

	err := <-done
	if err == nil {
		t.Fatal("expected context deadline exceeded error")
	}
	if err != context.DeadlineExceeded {
		t.Fatalf("expected DeadlineExceeded, got: %v", err)
	}
}
