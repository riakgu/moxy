//go:build linux

package proxy

import (
	"context"
	"fmt"
	"io"
	"net"
	"sync"

	"github.com/sirupsen/logrus"
)

// MuxHandler auto-detects SOCKS5 vs HTTP by peeking at the first byte.
// SOCKS5 starts with 0x05 (version), HTTP starts with ASCII (CONNECT, GET...).
type MuxHandler struct {
	Log    *logrus.Logger
	socks5 *Socks5Handler
	http   *HttpProxyHandler
	ln     net.Listener
	wg     sync.WaitGroup
	ctx    context.Context
	cancel context.CancelFunc
}

// NewMuxHandler creates a mux handler that delegates to socks5/http based on first byte.
func NewMuxHandler(log *logrus.Logger, connect ConnectFunc) *MuxHandler {
	ctx, cancel := context.WithCancel(context.Background())
	return &MuxHandler{
		Log:    log,
		socks5: NewSocks5Handler(log, connect),
		http:   NewHttpProxyHandler(log, connect),
		ctx:    ctx,
		cancel: cancel,
	}
}

// ListenAndServe starts the mux listener on the given address.
func (m *MuxHandler) ListenAndServe(addr string) error {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("mux listen: %w", err)
	}
	m.ln = ln
	m.Log.Infof("mux proxy listening on %s (SOCKS5+HTTP)", addr)

	for {
		conn, err := ln.Accept()
		if err != nil {
			select {
			case <-m.ctx.Done():
				return nil
			default:
			}
			m.Log.WithError(err).Error("mux accept failed")
			continue
		}

		m.wg.Add(1)
		go func() {
			defer m.wg.Done()
			m.handleConn(conn)
		}()
	}
}

func (m *MuxHandler) handleConn(conn net.Conn) {
	// Peek first byte to determine protocol
	buf := make([]byte, 1)
	if _, err := io.ReadFull(conn, buf); err != nil {
		conn.Close()
		return
	}

	pc := &prefixConn{Conn: conn, prefix: buf, prefixRead: false}

	if buf[0] == 0x05 {
		// SOCKS5 version byte
		m.socks5.ServeConn(pc)
	} else {
		// HTTP method (CONNECT, GET, POST, etc.)
		m.http.ServeConn(pc)
	}
}

// Shutdown stops accepting and waits for connections to drain.
func (m *MuxHandler) Shutdown(ctx context.Context) error {
	m.cancel()
	if m.ln != nil {
		m.ln.Close()
	}

	done := make(chan struct{})
	go func() {
		m.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// prefixConn replays a peeked byte before reading from the real connection.
type prefixConn struct {
	net.Conn
	prefix     []byte
	prefixRead bool
}

func (c *prefixConn) Read(b []byte) (int, error) {
	if !c.prefixRead {
		c.prefixRead = true
		n := copy(b, c.prefix)
		if n < len(b) {
			m, err := c.Conn.Read(b[n:])
			return n + m, err
		}
		return n, nil
	}
	return c.Conn.Read(b)
}
