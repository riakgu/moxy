package test

import (
	"io"
	"net"
	"testing"
	"time"

	"github.com/riakgu/moxy/internal/delivery/proxy"
)

func TestBridgeWithTimeout_IdleTimeoutClosesConnection(t *testing.T) {
	// Create an in-memory connection pair for the "client" side
	clientConn, clientEnd := net.Pipe()

	// Create an in-memory pipe for the "remote" side
	remoteRead, remoteWrite := io.Pipe()
	remote := &pipeRWC{reader: remoteRead, writer: remoteWrite}

	done := make(chan struct{})
	go func() {
		proxy.BridgeWithTimeout(clientConn, remote, 500*time.Millisecond)
		close(done)
	}()

	// Don't send any data — wait for idle timeout
	select {
	case <-done:
		// Bridge returned — idle timeout worked
	case <-time.After(3 * time.Second):
		t.Fatal("bridge did not return after idle timeout")
	}

	// Verify client connection was closed
	_, err := clientEnd.Write([]byte("test"))
	if err == nil {
		t.Fatal("expected client connection to be closed after idle timeout")
	}
}

func TestBridgeWithTimeout_DataResetsTimer(t *testing.T) {
	clientConn, clientEnd := net.Pipe()
	remoteRead, remoteWrite := io.Pipe()
	remote := &pipeRWC{reader: remoteRead, writer: remoteWrite}

	done := make(chan struct{})
	go func() {
		proxy.BridgeWithTimeout(clientConn, remote, 500*time.Millisecond)
		close(done)
	}()

	// Send data every 300ms for 4 rounds (total: 1.2s > 500ms idle timeout)
	// If timer resets on data, bridge should stay alive
	go func() {
		for i := 0; i < 4; i++ {
			time.Sleep(300 * time.Millisecond)
			clientEnd.Write([]byte("ping"))
		}
		// After last write, stop sending — idle timeout should fire after 500ms
	}()

	// Drain the remote side so writes don't block
	go func() {
		buf := make([]byte, 1024)
		for {
			_, err := remoteRead.Read(buf)
			if err != nil {
				return
			}
		}
	}()

	select {
	case <-done:
		// Bridge returned
	case <-time.After(5 * time.Second):
		t.Fatal("bridge did not return after activity stopped")
	}
}

func TestBridgeWithTimeout_ZeroTimeoutNoLimit(t *testing.T) {
	clientConn, clientEnd := net.Pipe()
	remoteRead, remoteWrite := io.Pipe()
	remote := &pipeRWC{reader: remoteRead, writer: remoteWrite}

	done := make(chan struct{})
	go func() {
		proxy.BridgeWithTimeout(clientConn, remote, 0)
		close(done)
	}()

	// Wait 500ms — bridge should NOT close (no timeout)
	time.Sleep(500 * time.Millisecond)

	select {
	case <-done:
		t.Fatal("bridge should not have returned with zero timeout")
	default:
		// Expected: bridge still running
	}

	// Close one side to end the bridge
	clientEnd.Close()

	select {
	case <-done:
		// Bridge returned after close
	case <-time.After(2 * time.Second):
		t.Fatal("bridge did not return after close")
	}
}

// pipeRWC wraps io.PipeReader and io.PipeWriter into an io.ReadWriteCloser
type pipeRWC struct {
	reader *io.PipeReader
	writer *io.PipeWriter
}

func (p *pipeRWC) Read(data []byte) (int, error)  { return p.reader.Read(data) }
func (p *pipeRWC) Write(data []byte) (int, error) { return p.writer.Write(data) }
func (p *pipeRWC) Close() error {
	p.reader.Close()
	return p.writer.Close()
}
