package test

import (
	"fmt"
	"io"
	"net"
	"testing"
	"time"
)

func TestSemaphore_RejectsWhenFull(t *testing.T) {
	// Simulate the proxy handler's semaphore logic with max 1 connection
	sem := make(chan struct{}, 1)

	// Start listener on random port
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()

	// Run accept loop in background — mirrors the handler's ListenAndServe pattern
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			select {
			case sem <- struct{}{}:
				go func() {
					defer func() { <-sem }()
					// Hold connection open to keep semaphore occupied
					time.Sleep(2 * time.Second)
					conn.Close()
				}()
			default:
				conn.Close()
			}
		}
	}()
	defer ln.Close()

	// First connection should succeed (semaphore has capacity)
	conn1, err := net.DialTimeout("tcp", addr, time.Second)
	if err != nil {
		t.Fatalf("first connection should succeed: %v", err)
	}
	defer conn1.Close()

	// Give goroutine time to acquire semaphore
	time.Sleep(100 * time.Millisecond)

	// Second connection should be accepted but immediately closed (semaphore full)
	conn2, err := net.DialTimeout("tcp", addr, time.Second)
	if err != nil {
		t.Fatalf("dial should succeed (TCP accept), but server should close it: %v", err)
	}
	defer conn2.Close()

	// Give server time to reject
	time.Sleep(100 * time.Millisecond)

	// Try to read — should get EOF (server closed the connection)
	buf := make([]byte, 1)
	_, err = conn2.Read(buf)
	if err == nil {
		t.Fatal("expected connection to be closed by server, but read succeeded")
	}
	if err != io.EOF {
		// On some systems it may be a connection reset rather than EOF
		t.Logf("connection closed with: %v (expected EOF or reset)", err)
	}
}

func TestSharedSemaphore_LimitsTotal(t *testing.T) {
	// Verify that a shared semaphore with capacity 2 blocks after 2 acquires
	sem := make(chan struct{}, 2)

	// Acquire 2
	sem <- struct{}{}
	sem <- struct{}{}

	// Third acquire should fail (non-blocking)
	select {
	case sem <- struct{}{}:
		t.Fatal("expected semaphore to be full, but acquired successfully")
	default:
		// Expected: semaphore is full
	}

	// Release one
	<-sem

	// Now should succeed
	select {
	case sem <- struct{}{}:
		// Expected: acquired after release
	default:
		t.Fatal("expected semaphore to allow acquire after release")
	}

	// Drain
	<-sem
	<-sem
}

func TestSemaphore_DefaultCapacity(t *testing.T) {
	// Verify default capacity of 500 works
	maxConns := 500
	sem := make(chan struct{}, maxConns)

	for i := 0; i < maxConns; i++ {
		select {
		case sem <- struct{}{}:
			// OK
		default:
			t.Fatalf("semaphore full at %d, expected capacity %d", i, maxConns)
		}
	}

	// Next should fail
	select {
	case sem <- struct{}{}:
		t.Fatal("semaphore should be full")
	default:
		// Expected
	}

	// Drain
	for i := 0; i < maxConns; i++ {
		<-sem
	}

	_ = fmt.Sprintf("capacity %d verified", maxConns)
}
