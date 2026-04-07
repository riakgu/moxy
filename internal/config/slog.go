package config

import (
	"context"
	"log/slog"
	"os"
	"strings"

	"github.com/spf13/viper"
)

// MultiHandler fans out slog records to multiple handlers.
type MultiHandler struct {
	handlers []slog.Handler
}

func NewMultiHandler(handlers ...slog.Handler) *MultiHandler {
	return &MultiHandler{handlers: handlers}
}

func (h *MultiHandler) Enabled(ctx context.Context, level slog.Level) bool {
	for _, handler := range h.handlers {
		if handler.Enabled(ctx, level) {
			return true
		}
	}
	return false
}

func (h *MultiHandler) Handle(ctx context.Context, r slog.Record) error {
	for _, handler := range h.handlers {
		if handler.Enabled(ctx, r.Level) {
			if err := handler.Handle(ctx, r.Clone()); err != nil {
				return err
			}
		}
	}
	return nil
}

func (h *MultiHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	handlers := make([]slog.Handler, len(h.handlers))
	for i, handler := range h.handlers {
		handlers[i] = handler.WithAttrs(attrs)
	}
	return &MultiHandler{handlers: handlers}
}

func (h *MultiHandler) WithGroup(name string) slog.Handler {
	handlers := make([]slog.Handler, len(h.handlers))
	for i, handler := range h.handlers {
		handlers[i] = handler.WithGroup(name)
	}
	return &MultiHandler{handlers: handlers}
}

func parseLevel(s string) slog.Level {
	switch strings.ToLower(s) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// NewLogger creates a slog.Logger with stdout handler only.
func NewLogger(v *viper.Viper) *slog.Logger {
	level := parseLevel(v.GetString("log.level"))
	opts := &slog.HandlerOptions{Level: level}

	var handler slog.Handler
	if v.GetString("log.format") == "json" {
		handler = slog.NewJSONHandler(os.Stdout, opts)
	} else {
		handler = slog.NewTextHandler(os.Stdout, opts)
	}

	return slog.New(handler)
}

// NewLoggerWithRing creates a slog.Logger that writes to stdout AND a ring buffer.
func NewLoggerWithRing(v *viper.Viper, ring slog.Handler) *slog.Logger {
	level := parseLevel(v.GetString("log.level"))
	opts := &slog.HandlerOptions{Level: level}

	var stdoutHandler slog.Handler
	if v.GetString("log.format") == "json" {
		stdoutHandler = slog.NewJSONHandler(os.Stdout, opts)
	} else {
		stdoutHandler = slog.NewTextHandler(os.Stdout, opts)
	}

	return slog.New(NewMultiHandler(stdoutHandler, ring))
}
