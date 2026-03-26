package proxy_test

import (
	"io"
	"net"
	"testing"
	"time"

	"github.com/riakgu/moxy/internal/delivery/proxy"
)

func TestBridgeWithTimeout_IdleTimeoutClosesConnection(t *testing.T) {
	clientConn, clientEnd := net.Pipe()

	remoteRead, remoteWrite := io.Pipe()
	remote := &pipeRWC{reader: remoteRead, writer: remoteWrite}

	done := make(chan struct{})
	go func() {
		proxy.BridgeWithTimeout(clientConn, remote, 500*time.Millisecond)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("bridge did not return after idle timeout")
	}

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

	go func() {
		for i := 0; i < 4; i++ {
			time.Sleep(300 * time.Millisecond)
			clientEnd.Write([]byte("ping"))
		}
	}()

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

	time.Sleep(500 * time.Millisecond)

	select {
	case <-done:
		t.Fatal("bridge should not have returned with zero timeout")
	default:
	}

	clientEnd.Close()

	select {
	case <-done:
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
