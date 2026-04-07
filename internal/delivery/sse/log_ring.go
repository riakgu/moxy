package sse

import (
	"context"
	"log/slog"
	"sync"
)

// LogEntry is a structured log entry for SSE streaming.
type LogEntry struct {
	Time      int64             `json:"time"`
	Level     string            `json:"level"`
	Message   string            `json:"msg"`
	Component string            `json:"component,omitempty"`
	Attrs     map[string]string `json:"attrs,omitempty"`
}

// ringCore holds the shared mutable state for all RingHandler instances.
// Child handlers created via WithAttrs share the same core pointer,
// so SetHub propagates to all of them.
type ringCore struct {
	mu      sync.RWMutex
	entries []LogEntry
	size    int
	pos     int
	count   int
	hub     *EventHub
}

// RingHandler is a slog.Handler that stores log entries in a circular buffer
// and publishes them to an EventHub for SSE streaming.
type RingHandler struct {
	core  *ringCore
	level slog.Level
	attrs []slog.Attr
}

func NewRingHandler(size int, level slog.Level) *RingHandler {
	if size <= 0 {
		size = 1000
	}
	return &RingHandler{
		core: &ringCore{
			entries: make([]LogEntry, size),
			size:    size,
		},
		level: level,
	}
}

// SetHub wires the EventHub for live streaming (called after hub creation).
// Visible to all child handlers since they share the same core.
func (h *RingHandler) SetHub(hub *EventHub) {
	h.core.mu.Lock()
	defer h.core.mu.Unlock()
	h.core.hub = hub
}

func (h *RingHandler) Enabled(_ context.Context, level slog.Level) bool {
	return level >= h.level
}

func (h *RingHandler) Handle(_ context.Context, r slog.Record) error {
	entry := LogEntry{
		Time:    r.Time.UnixMilli(),
		Level:   r.Level.String(),
		Message: r.Message,
		Attrs:   make(map[string]string),
	}

	// Collect pre-set attrs (from WithAttrs)
	for _, a := range h.attrs {
		if a.Key == "component" {
			entry.Component = a.Value.String()
		} else {
			entry.Attrs[a.Key] = a.Value.String()
		}
	}

	// Collect record attrs
	r.Attrs(func(a slog.Attr) bool {
		if a.Key == "component" {
			entry.Component = a.Value.String()
		} else {
			entry.Attrs[a.Key] = a.Value.String()
		}
		return true
	})

	// Remove empty attrs map to keep JSON clean
	if len(entry.Attrs) == 0 {
		entry.Attrs = nil
	}

	c := h.core
	c.mu.Lock()
	c.entries[c.pos] = entry
	c.pos = (c.pos + 1) % c.size
	if c.count < c.size {
		c.count++
	}
	hub := c.hub
	c.mu.Unlock()

	// Publish to SSE (non-blocking, outside lock)
	if hub != nil {
		hub.Publish("log_entry", entry)
	}

	return nil
}

func (h *RingHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	newAttrs := make([]slog.Attr, len(h.attrs)+len(attrs))
	copy(newAttrs, h.attrs)
	copy(newAttrs[len(h.attrs):], attrs)
	return &RingHandler{
		core:  h.core, // shared — same buffer, same hub
		level: h.level,
		attrs: newAttrs,
	}
}

func (h *RingHandler) WithGroup(_ string) slog.Handler {
	// Flat structure — no group support needed
	return h
}

// GetRecent returns buffered entries in chronological order.
func (h *RingHandler) GetRecent() []LogEntry {
	c := h.core
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.count == 0 {
		return nil
	}

	result := make([]LogEntry, 0, c.count)
	start := 0
	if c.count >= c.size {
		start = c.pos // oldest entry is at current write position
	}

	for i := 0; i < c.count; i++ {
		idx := (start + i) % c.size
		result = append(result, c.entries[idx])
	}
	return result
}

// Ensure compile-time interface check
var _ slog.Handler = (*RingHandler)(nil)
