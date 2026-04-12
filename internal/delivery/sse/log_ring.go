package sse

import (
	"context"
	"log/slog"

	"github.com/riakgu/moxy/internal/entity"
	"github.com/riakgu/moxy/internal/model/converter"
	"github.com/riakgu/moxy/internal/repository"
)

type RingHandler struct {
	store *repository.LogRepository
	hub   *EventHub
	level slog.Level
	attrs []slog.Attr
}

func NewRingHandler(store *repository.LogRepository, level slog.Level) *RingHandler {
	return &RingHandler{
		store: store,
		level: level,
	}
}

func (h *RingHandler) SetHub(hub *EventHub) {
	h.hub = hub
}

func (h *RingHandler) Enabled(_ context.Context, level slog.Level) bool {
	return level >= h.level
}

func (h *RingHandler) Handle(_ context.Context, r slog.Record) error {
	entry := entity.LogEntry{
		Time:    r.Time.UnixMilli(),
		Level:   r.Level.String(),
		Message: r.Message,
		Attrs:   make(map[string]string),
	}

	for _, a := range h.attrs {
		if a.Key == "component" {
			entry.Component = a.Value.String()
		} else {
			entry.Attrs[a.Key] = a.Value.String()
		}
	}

	r.Attrs(func(a slog.Attr) bool {
		if a.Key == "component" {
			entry.Component = a.Value.String()
		} else {
			entry.Attrs[a.Key] = a.Value.String()
		}
		return true
	})

	if len(entry.Attrs) == 0 {
		entry.Attrs = nil
	}

	h.store.Append(entry)

	if h.hub != nil {
		h.hub.Publish("log_entry", converter.LogEntryToResponse(entry))
	}

	return nil
}

func (h *RingHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	newAttrs := make([]slog.Attr, len(h.attrs)+len(attrs))
	copy(newAttrs, h.attrs)
	copy(newAttrs[len(h.attrs):], attrs)
	return &RingHandler{
		store: h.store,
		hub:   h.hub,
		level: h.level,
		attrs: newAttrs,
	}
}

func (h *RingHandler) WithGroup(_ string) slog.Handler {
	return h
}

var _ slog.Handler = (*RingHandler)(nil)
