package proxy

import (
	"io"
	"net"
	"sync"
	"time"
)

// bridgeWithTimeout copies data bidirectionally between client and remote.
// If no data is transferred in either direction for idleTimeout duration,
// both sides are closed to free resources.
// An idleTimeout of 0 disables the idle timeout (bridge runs until one side closes).
func BridgeWithTimeout(client net.Conn, remote io.ReadWriteCloser, idleTimeout time.Duration) {
	var timer *time.Timer
	var once sync.Once
	closeAll := func() {
		client.Close()
		remote.Close()
	}

	if idleTimeout > 0 {
		timer = time.AfterFunc(idleTimeout, func() {
			once.Do(closeAll)
		})
	}

	resetTimer := func() {
		if timer != nil {
			timer.Reset(idleTimeout)
		}
	}

	errc := make(chan error, 2)

	// client → remote
	go func() {
		_, err := copyWithCallback(remote, client, resetTimer)
		errc <- err
	}()

	// remote → client
	go func() {
		_, err := copyWithCallback(client, remote, resetTimer)
		errc <- err
	}()

	// Wait for first copy to finish
	<-errc

	// Stop timer and close both sides
	if timer != nil {
		timer.Stop()
	}
	once.Do(closeAll)
}

// copyWithCallback is like io.Copy but calls onData after each successful read/write.
func copyWithCallback(dst io.Writer, src io.Reader, onData func()) (int64, error) {
	buf := make([]byte, 32*1024)
	var total int64

	for {
		nr, readErr := src.Read(buf)
		if nr > 0 {
			nw, writeErr := dst.Write(buf[:nr])
			if nw > 0 {
				total += int64(nw)
				onData()
			}
			if writeErr != nil {
				return total, writeErr
			}
			if nr != nw {
				return total, io.ErrShortWrite
			}
		}
		if readErr != nil {
			return total, readErr
		}
	}
}
