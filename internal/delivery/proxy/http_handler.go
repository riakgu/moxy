package proxy

import (
	"bufio"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"

	"github.com/sirupsen/logrus"

	"github.com/riakgu/moxy/internal/entity"
	"github.com/riakgu/moxy/internal/model"
	"github.com/riakgu/moxy/internal/usecase"
)

type HttpProxyHandler struct {
	Log     *logrus.Logger
	ProxyUC *usecase.ProxyUseCase
}

func NewHttpProxyHandler(log *logrus.Logger, proxyUC *usecase.ProxyUseCase) *HttpProxyHandler {
	return &HttpProxyHandler{
		Log:     log,
		ProxyUC: proxyUC,
	}
}

func (h *HttpProxyHandler) ListenAndServe(addr string) error {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("http proxy listen: %w", err)
	}
	h.Log.Infof("HTTP proxy listening on %s", addr)

	for {
		conn, err := ln.Accept()
		if err != nil {
			h.Log.WithError(err).Error("http proxy accept failed")
			continue
		}
		go h.handleConnection(conn)
	}
}

func (h *HttpProxyHandler) handleConnection(conn net.Conn) {
	defer conn.Close()

	reader := bufio.NewReader(conn)
	req, err := http.ReadRequest(reader)
	if err != nil {
		return
	}

	// Extract Proxy-Authorization
	username, password, ok := h.parseProxyAuth(req)
	if !ok {
		h.sendResponse(conn, http.StatusProxyAuthRequired, "Proxy-Authenticate", "Basic realm=\"moxy\"")
		return
	}

	authReq := model.ParseProxyAuth(username, password)
	slot, err := h.ProxyUC.Authenticate(authReq)
	if err != nil {
		h.Log.WithError(err).Warn("http proxy auth failed")
		if errors.Is(err, model.ErrNoSlotsAvailable) {
			h.sendResponse(conn, http.StatusServiceUnavailable, "", "")
		} else {
			h.sendResponse(conn, http.StatusProxyAuthRequired, "Proxy-Authenticate", "Basic realm=\"moxy\"")
		}
		return
	}

	if req.Method == http.MethodConnect {
		h.handleConnect(conn, req, slot)
	} else {
		h.handleForward(conn, req, slot)
	}
}

func (h *HttpProxyHandler) parseProxyAuth(req *http.Request) (string, string, bool) {
	auth := req.Header.Get("Proxy-Authorization")
	if auth == "" {
		return "", "", false
	}

	if !strings.HasPrefix(auth, "Basic ") {
		return "", "", false
	}

	decoded, err := base64.StdEncoding.DecodeString(auth[6:])
	if err != nil {
		return "", "", false
	}

	parts := strings.SplitN(string(decoded), ":", 2)
	if len(parts) != 2 {
		return "", "", false
	}

	return parts[0], parts[1], true
}

func (h *HttpProxyHandler) handleConnect(conn net.Conn, req *http.Request, slot *entity.Slot) {
	remote, err := h.ProxyUC.Connect(slot, req.Host)
	if err != nil {
		h.Log.WithError(err).Warnf("http CONNECT dial failed: %s via %s", req.Host, slot.Name)
		h.sendResponse(conn, http.StatusBadGateway, "", "")
		return
	}
	defer remote.Close()

	conn.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n"))

	errc := make(chan error, 2)
	go func() {
		_, err := io.Copy(remote, conn)
		errc <- err
	}()
	go func() {
		_, err := io.Copy(conn, remote)
		errc <- err
	}()
	<-errc
}

func (h *HttpProxyHandler) handleForward(conn net.Conn, req *http.Request, slot *entity.Slot) {
	host := req.Host
	if !strings.Contains(host, ":") {
		host = host + ":80"
	}

	remote, err := h.ProxyUC.Connect(slot, host)
	if err != nil {
		h.Log.WithError(err).Warnf("http forward dial failed: %s via %s", host, slot.Name)
		h.sendResponse(conn, http.StatusBadGateway, "", "")
		return
	}
	defer remote.Close()

	req.Header.Del("Proxy-Authorization")
	req.Header.Del("Proxy-Connection")
	req.RequestURI = req.URL.Path
	if req.URL.RawQuery != "" {
		req.RequestURI += "?" + req.URL.RawQuery
	}

	if err := req.Write(remote); err != nil {
		h.sendResponse(conn, http.StatusBadGateway, "", "")
		return
	}

	io.Copy(conn, remote)
}

func (h *HttpProxyHandler) sendResponse(conn net.Conn, status int, headerKey, headerVal string) {
	statusText := http.StatusText(status)
	resp := fmt.Sprintf("HTTP/1.1 %d %s\r\n", status, statusText)
	if headerKey != "" {
		resp += fmt.Sprintf("%s: %s\r\n", headerKey, headerVal)
	}
	resp += "\r\n"
	conn.Write([]byte(resp))
}
