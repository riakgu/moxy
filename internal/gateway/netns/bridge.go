package netns

import (
	"context"
	"io"
	"net"
	"sync"
	"time"
)

// BridgeWithTimeout copies data bidirectionally between client and remote.
// The bridge ends when:
//   - one side closes the connection
//   - no data flows for idleTimeout (if > 0)
//   - the context is cancelled (external kill: shutdown, slot teardown)
//
// Returns bytes sent (client→remote) and bytes received (remote→client).
func BridgeWithTimeout(ctx context.Context, client net.Conn, remote io.ReadWriteCloser, idleTimeout time.Duration) (sent int64, received int64) {
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

	done := make(chan struct{}, 2)

	// Context cancellation — kill relay when ctx is cancelled
	go func() {
		select {
		case <-ctx.Done():
			once.Do(closeAll)
		case <-done:
			// Bridge finished naturally, exit this goroutine
		}
	}()

	// client → remote (sent/upload)
	go func() {
		n, _ := copyWithCallback(remote, client, resetTimer)
		sent = n
		done <- struct{}{}
	}()

	// remote → client (received/download)
	go func() {
		n, _ := copyWithCallback(client, remote, resetTimer)
		received = n
		done <- struct{}{}
	}()

	// Wait for first copy to finish
	<-done

	// Stop timer and close both sides
	if timer != nil {
		timer.Stop()
	}
	once.Do(closeAll)

	// Wait for second copy to finish
	<-done

	return sent, received
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
