//go:build linux

package proxy

import (
	"context"
	"fmt"
	"io"
	"net"
	"sync"
	"log/slog"
)

// MuxHandler auto-detects SOCKS5 vs HTTP by peeking at the first byte.
type MuxHandler struct {
	Log    *slog.Logger
	socks5 *Socks5Handler
	http   *HttpProxyHandler
	ln     net.Listener
	wg     sync.WaitGroup
	ctx    context.Context
	cancel context.CancelFunc
}

func NewMuxHandler(log *slog.Logger, connect ConnectFunc) *MuxHandler {
	ctx, cancel := context.WithCancel(context.Background())
	return &MuxHandler{
		Log:    log,
		socks5: NewSocks5Handler(log, connect),
		http:   NewHttpProxyHandler(log, connect),
		ctx:    ctx,
		cancel: cancel,
	}
}

// Call Serve() in a goroutine after Listen succeeds.
func (m *MuxHandler) Listen(addr string) error {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("mux listen: %w", err)
	}
	m.ln = ln
	return nil
}

// Must call Listen() first.
func (m *MuxHandler) Serve() error {
	if m.ln == nil {
		return fmt.Errorf("mux serve: listener not initialized — call Listen() first")
	}
	for {
		conn, err := m.ln.Accept()
		if err != nil {
			select {
			case <-m.ctx.Done():
				return nil
			default:
			}
			m.Log.Error("accept failed", "error", err)
			continue
		}

		m.wg.Add(1)
		go func() {
			defer m.wg.Done()
			m.handleConn(conn)
		}()
	}
}

func (m *MuxHandler) ListenAndServe(addr string) error {
	if err := m.Listen(addr); err != nil {
		return err
	}
	return m.Serve()
}

func (m *MuxHandler) handleConn(conn net.Conn) {
	buf := make([]byte, 1)
	if _, err := io.ReadFull(conn, buf); err != nil {
		_ = conn.Close()
		return
	}

	pc := &prefixConn{Conn: conn, prefix: buf, prefixRead: false}

	if buf[0] == 0x05 {
		m.socks5.ServeConn(pc)
	} else {
		m.http.ServeConn(pc)
	}
}

func (m *MuxHandler) Shutdown(ctx context.Context) error {
	m.cancel()
	if m.ln != nil {
		_ = m.ln.Close()
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
