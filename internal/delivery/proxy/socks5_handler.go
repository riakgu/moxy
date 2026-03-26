package proxy

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"strconv"
	"sync"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/riakgu/moxy/internal/model"
	"github.com/riakgu/moxy/internal/usecase"
)

type Socks5Handler struct {
	Log         *logrus.Logger
	ProxyUC     *usecase.ProxyUseCase
	sem         chan struct{}
	idleTimeout time.Duration
	ln          net.Listener
	wg          sync.WaitGroup
	closeCh     chan struct{}
}

func NewSocks5Handler(log *logrus.Logger, proxyUC *usecase.ProxyUseCase, sem chan struct{}, idleTimeout time.Duration) *Socks5Handler {
	return &Socks5Handler{
		Log:         log,
		ProxyUC:     proxyUC,
		sem:         sem,
		idleTimeout: idleTimeout,
		closeCh:     make(chan struct{}),
	}
}

func (h *Socks5Handler) ListenAndServe(addr string) error {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("socks5 listen: %w", err)
	}
	h.ln = ln
	h.Log.Infof("SOCKS5 proxy listening on %s", addr)

	for {
		conn, err := ln.Accept()
		if err != nil {
			select {
			case <-h.closeCh:
				return nil
			default:
			}
			h.Log.WithError(err).Error("socks5 accept failed")
			continue
		}

		select {
		case h.sem <- struct{}{}:
			h.wg.Add(1)
			go func() {
				defer h.wg.Done()
				defer func() { <-h.sem }()
				h.handleConnection(conn)
			}()
		default:
			h.Log.Warn("socks5: connection rejected — too many concurrent connections")
			conn.Close()
		}
	}
}

func (h *Socks5Handler) handleConnection(conn net.Conn) {
	defer conn.Close()

	// Set handshake deadline — 30 seconds for the entire auth+connect phase
	conn.SetDeadline(time.Now().Add(30 * time.Second))

	// 1. Greeting: client sends version + auth methods
	buf := make([]byte, 258)
	n, err := conn.Read(buf)
	if err != nil || n < 3 || buf[0] != 0x05 {
		return
	}
	// Require username/password auth (0x02)
	hasUserPassAuth := false
	nmethods := int(buf[1])
	for i := 0; i < nmethods && i+2 < n; i++ {
		if buf[2+i] == 0x02 {
			hasUserPassAuth = true
			break
		}
	}

	if !hasUserPassAuth {
		conn.Write([]byte{0x05, 0xFF}) // no acceptable methods
		return
	}

	// Accept username/password auth
	conn.Write([]byte{0x05, 0x02})

	// 2. Username/Password auth (RFC 1929)
	n, err = conn.Read(buf)
	if err != nil || n < 4 || buf[0] != 0x01 {
		return
	}

	ulen := int(buf[1])
	if 2+ulen+1 > n {
		return
	}
	username := string(buf[2 : 2+ulen])

	plen := int(buf[2+ulen])
	if 2+ulen+1+plen > n {
		return
	}
	password := string(buf[2+ulen+1 : 2+ulen+1+plen])

	authReq := model.ParseProxyAuth(username, password)
	slot, err := h.ProxyUC.Authenticate(authReq)
	if err != nil {
		h.Log.WithError(err).Warn("socks5 auth failed")
		if errors.Is(err, model.ErrNoSlotsAvailable) {
			// Auth succeeded but no slots — accept auth, fail on connect below
			conn.Write([]byte{0x01, 0x00})
			// Read the CONNECT request then reply with general failure
			n, _ = conn.Read(buf)
			reply := []byte{0x05, 0x01, 0x00, 0x01, 0, 0, 0, 0, 0, 0}
			conn.Write(reply)
			return
		}
		conn.Write([]byte{0x01, 0x01}) // auth failure
		return
	}
	conn.Write([]byte{0x01, 0x00}) // auth success

	// 3. CONNECT request
	n, err = conn.Read(buf)
	if err != nil || n < 7 || buf[0] != 0x05 || buf[1] != 0x01 {
		// Only support CONNECT (0x01)
		if n >= 4 {
			reply := []byte{0x05, 0x07, 0x00, 0x01, 0, 0, 0, 0, 0, 0} // command not supported
			conn.Write(reply)
		}
		return
	}

	// Parse target address
	var targetAddr string
	switch buf[3] {
	case 0x01: // IPv4
		if n < 10 {
			return
		}
		ip := net.IP(buf[4:8])
		port := binary.BigEndian.Uint16(buf[8:10])
		targetAddr = net.JoinHostPort(ip.String(), strconv.Itoa(int(port)))
	case 0x03: // Domain name
		domainLen := int(buf[4])
		if 5+domainLen+2 > n {
			return
		}
		domain := string(buf[5 : 5+domainLen])
		port := binary.BigEndian.Uint16(buf[5+domainLen : 5+domainLen+2])
		targetAddr = net.JoinHostPort(domain, strconv.Itoa(int(port)))
	case 0x04: // IPv6
		if n < 22 {
			return
		}
		ip := net.IP(buf[4:20])
		port := binary.BigEndian.Uint16(buf[20:22])
		targetAddr = net.JoinHostPort(ip.String(), strconv.Itoa(int(port)))
	default:
		reply := []byte{0x05, 0x08, 0x00, 0x01, 0, 0, 0, 0, 0, 0} // address type not supported
		conn.Write(reply)
		return
	}

	// 4. Dial target via namespace
	remote, err := h.ProxyUC.Connect(slot.Name, targetAddr)
	if err != nil {
		h.Log.WithError(err).Warnf("socks5 dial failed: %s via %s", targetAddr, slot.Name)
		reply := []byte{0x05, 0x05, 0x00, 0x01, 0, 0, 0, 0, 0, 0} // connection refused
		conn.Write(reply)
		return
	}
	defer remote.Close()

	/// 5. Success reply
	reply := []byte{0x05, 0x00, 0x00, 0x01, 0, 0, 0, 0, 0, 0}
	conn.Write(reply)

	// 6. Clear handshake deadline before bridge
	conn.SetDeadline(time.Time{})

	// 7. Bridge with idle timeout
	BridgeWithTimeout(conn, remote, h.idleTimeout)
}

// Shutdown stops accepting new connections and waits for active ones to drain.
// It blocks until all connections complete or the context is cancelled.
func (h *Socks5Handler) Shutdown(ctx context.Context) error {
	close(h.closeCh)
	if h.ln != nil {
		h.ln.Close()
	}

	// Wait for active connections to drain or context to expire
	done := make(chan struct{})
	go func() {
		h.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
