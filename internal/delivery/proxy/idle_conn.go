//go:build linux

package proxy

import (
	"net"
	"time"
)

// idleTimeoutConn wraps a net.Conn with an idle timeout.
// Every successful Read or Write resets the deadline,
// so the connection is closed if no data flows for the timeout duration.
type idleTimeoutConn struct {
	net.Conn
	timeout time.Duration
}

func newIdleTimeoutConn(conn net.Conn, timeout time.Duration) *idleTimeoutConn {
	return &idleTimeoutConn{
		Conn:    conn,
		timeout: timeout,
	}
}

func (c *idleTimeoutConn) Read(b []byte) (n int, err error) {
	if c.timeout > 0 {
		c.Conn.SetDeadline(time.Now().Add(c.timeout))
	}
	return c.Conn.Read(b)
}

func (c *idleTimeoutConn) Write(b []byte) (n int, err error) {
	if c.timeout > 0 {
		c.Conn.SetDeadline(time.Now().Add(c.timeout))
	}
	return c.Conn.Write(b)
}
