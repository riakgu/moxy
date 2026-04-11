package sse

import (
	"bufio"
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"log/slog"

	"github.com/gofiber/fiber/v2"

	"github.com/riakgu/moxy/internal/model"
)

// SnapshotFunc returns the current full state for the init event.
type SnapshotFunc func() (*InitPayload, error)

// InitPayload is sent on first connect.
type InitPayload struct {
	Devices  []model.DeviceResponse          `json:"devices"`
	Slots    []model.SlotResponse            `json:"slots"`
	Logs     []LogEntry                      `json:"logs,omitempty"`
	Traffic  *model.TrafficListResponse      `json:"traffic,omitempty"`
	DNSStats *model.DNSCacheStatsResponse    `json:"dns_stats,omitempty"`
}

// SSEHandler serves the GET /api/events endpoint.
type SSEHandler struct {
	hub          *EventHub
	log          *slog.Logger
	snapshot     SnapshotFunc
	debounceMs   int
	heartbeatSec int
}

func NewSSEHandler(
	hub *EventHub,
	log *slog.Logger,
	snapshot SnapshotFunc,
	debounceMs int,
	heartbeatSec int,
) *SSEHandler {
	if debounceMs <= 0 {
		debounceMs = 1000
	}
	if heartbeatSec <= 0 {
		heartbeatSec = 30
	}
	return &SSEHandler{
		hub:          hub,
		log:          log,
		snapshot:     snapshot,
		debounceMs:   debounceMs,
		heartbeatSec: heartbeatSec,
	}
}

// Stream handles the SSE connection lifecycle.
func (h *SSEHandler) Stream(c *fiber.Ctx) error {
	c.Set("Content-Type", "text/event-stream")
	c.Set("Cache-Control", "no-cache")
	c.Set("Connection", "keep-alive")
	c.Set("X-Accel-Buffering", "no") // nginx

	clientID, eventCh := h.hub.Subscribe()
	if clientID == "" {
		return fiber.NewError(fiber.StatusTooManyRequests, "max SSE clients reached")
	}

	c.Context().SetBodyStreamWriter(func(w *bufio.Writer) {
		defer h.hub.Unsubscribe(clientID)

		// Send init snapshot
		if snap, err := h.snapshot(); err == nil {
			writeSSE(w, "init", snap)
			_ = w.Flush()
		} else {
			h.log.Warn("failed to build init snapshot", "error", err)
		}

		debounceInterval := time.Duration(h.debounceMs) * time.Millisecond
		heartbeatInterval := time.Duration(h.heartbeatSec) * time.Second
		heartbeatTicker := time.NewTicker(heartbeatInterval)
		defer heartbeatTicker.Stop()
		debounceTicker := time.NewTicker(debounceInterval)
		defer debounceTicker.Stop()

		// Dirty map: key = "topic:entityID", value = latest event
		dirty := make(map[string]Event)

		for {
			select {
			case evt, ok := <-eventCh:
				if !ok {
					return // hub shut down or unsubscribed
				}
				// Deduplicate by topic + entity key
				key := dedupeKey(evt)
				dirty[key] = evt

			case <-debounceTicker.C:
				if len(dirty) == 0 {
					continue
				}
				for _, evt := range dirty {
					writeSSE(w, evt.Topic, evt.Data)
				}
				if err := w.Flush(); err != nil {
					return // client disconnected
				}
				dirty = make(map[string]Event)

			case <-heartbeatTicker.C:
				if _, err := fmt.Fprintf(w, ": heartbeat\n\n"); err != nil {
					return // client disconnected
				}
				if err := w.Flush(); err != nil {
					return
				}
			}
		}
	})

	return nil
}

// writeSSE writes a single SSE event.
func writeSSE(w *bufio.Writer, event string, data interface{}) {
	jsonBytes, err := json.Marshal(data)
	if err != nil {
		return
	}
	_, _ = fmt.Fprintf(w, "event: %s\n", event)
	// SSE spec: each line of data must be prefixed with "data: "
	for _, line := range strings.Split(string(jsonBytes), "\n") {
		_, _ = fmt.Fprintf(w, "data: %s\n", line)
	}
	_, _ = fmt.Fprint(w, "\n")
}

// dedupeKey creates a unique key for deduplication.
// For entity events, we extract the identifier from the data.
func dedupeKey(evt Event) string {
	switch v := evt.Data.(type) {
	case *model.SlotResponse:
		return evt.Topic + ":" + v.Name
	case model.SlotResponse:
		return evt.Topic + ":" + v.Name
	case *model.DeviceResponse:
		return evt.Topic + ":" + v.Alias
	case model.DeviceResponse:
		return evt.Topic + ":" + v.Alias
	case map[string]string:
		if name, ok := v["name"]; ok {
			return evt.Topic + ":" + name
		}
		if alias, ok := v["alias"]; ok {
			return evt.Topic + ":" + alias
		}
	}
	return evt.Topic
}
