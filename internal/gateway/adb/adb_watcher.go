//go:build linux

package adb

import (
	"context"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"strings"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/riakgu/moxy/internal/model"
)

// ADBWatcher maintains a persistent connection to the ADB server using
// the track-devices protocol and emits DeviceEvent on a channel when
// devices connect or disconnect.
type ADBWatcher struct {
	Log            *logrus.Logger
	MaxReconnectMs int
}

// NewADBWatcher creates a new ADB device watcher.
func NewADBWatcher(log *logrus.Logger, maxReconnectMs int) *ADBWatcher {
	if maxReconnectMs <= 0 {
		maxReconnectMs = 30000
	}
	return &ADBWatcher{
		Log:            log,
		MaxReconnectMs: maxReconnectMs,
	}
}

// Watch connects to the ADB server and streams device events.
// It automatically reconnects with exponential backoff on connection loss.
// The returned channel is closed when the context is cancelled.
func (w *ADBWatcher) Watch(ctx context.Context) <-chan model.DeviceEvent {
	events := make(chan model.DeviceEvent, 16)

	go func() {
		defer close(events)
		backoff := time.Second
		maxBackoff := time.Duration(w.MaxReconnectMs) * time.Millisecond

		for {
			err := w.trackDevices(ctx, events)
			if ctx.Err() != nil {
				return // context cancelled — clean shutdown
			}
			w.Log.Warnf("adb watcher: connection lost: %v — reconnecting in %s", err, backoff)

			select {
			case <-ctx.Done():
				return
			case <-time.After(backoff):
			}

			// Exponential backoff
			backoff *= 2
			if backoff > maxBackoff {
				backoff = maxBackoff
			}
		}
	}()

	return events
}

// trackDevices connects once and streams until error or context cancellation.
func (w *ADBWatcher) trackDevices(ctx context.Context, events chan<- model.DeviceEvent) error {
	// Connect to ADB server
	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", "localhost:5037")
	if err != nil {
		return fmt.Errorf("connect to ADB: %w", err)
	}
	defer conn.Close()

	// Close connection when context is cancelled
	go func() {
		<-ctx.Done()
		conn.Close()
	}()

	// Send track-devices command using ADB wire protocol:
	// <4-char hex length><payload>
	cmd := "host:track-devices"
	header := fmt.Sprintf("%04x%s", len(cmd), cmd)
	if _, err := conn.Write([]byte(header)); err != nil {
		return fmt.Errorf("send track-devices: %w", err)
	}

	// Read response: expect "OKAY"
	resp := make([]byte, 4)
	if _, err := io.ReadFull(conn, resp); err != nil {
		return fmt.Errorf("read response: %w", err)
	}
	if string(resp) != "OKAY" {
		return fmt.Errorf("ADB rejected track-devices: %s", string(resp))
	}

	w.Log.Info("adb watcher: connected, tracking devices")

	prev := make(map[string]string) // serial → status

	for {
		// Read 4-char hex length
		lenBuf := make([]byte, 4)
		if _, err := io.ReadFull(conn, lenBuf); err != nil {
			return fmt.Errorf("read length: %w", err)
		}

		payloadLen, err := parseHexLen(lenBuf)
		if err != nil {
			return fmt.Errorf("parse length %q: %w", string(lenBuf), err)
		}

		// Read payload
		var payload []byte
		if payloadLen > 0 {
			payload = make([]byte, payloadLen)
			if _, err := io.ReadFull(conn, payload); err != nil {
				return fmt.Errorf("read payload: %w", err)
			}
		}

		// Parse device list
		current := parseDeviceList(string(payload))

		// Diff: detect connects
		for serial, status := range current {
			prevStatus, existed := prev[serial]
			if status == "device" && (!existed || prevStatus != "device") {
				select {
				case events <- model.DeviceEvent{Serial: serial, Status: "connected"}:
				case <-ctx.Done():
					return ctx.Err()
				}
			}
		}

		// Diff: detect disconnects
		for serial, prevStatus := range prev {
			if prevStatus == "device" {
				curStatus, exists := current[serial]
				if !exists || curStatus != "device" {
					select {
					case events <- model.DeviceEvent{Serial: serial, Status: "disconnected"}:
					case <-ctx.Done():
						return ctx.Err()
					}
				}
			}
		}

		prev = current
	}
}

// parseHexLen converts a 4-char hex string to an integer.
func parseHexLen(b []byte) (int, error) {
	dst := make([]byte, 2)
	_, err := hex.Decode(dst, b)
	if err != nil {
		return 0, err
	}
	return int(dst[0])<<8 | int(dst[1]), nil
}

// parseDeviceList parses "serial\tstatus\n" lines into a map.
func parseDeviceList(data string) map[string]string {
	devices := make(map[string]string)
	for _, line := range strings.Split(strings.TrimSpace(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) >= 2 {
			devices[parts[0]] = parts[1]
		}
	}
	return devices
}
