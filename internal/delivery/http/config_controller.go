package http

import (
	"encoding/json"
	"log/slog"
	"os"
	"os/exec"
	"time"

	"github.com/gofiber/fiber/v2"

	"github.com/riakgu/moxy/internal/model"
)

type ConfigController struct {
	Log        *slog.Logger
	ConfigPath string
}

func NewConfigController(log *slog.Logger, configPath string) *ConfigController {
	return &ConfigController{Log: log, ConfigPath: configPath}
}

// Get reads config.json from disk and returns it as JSON.
func (c *ConfigController) Get(ctx *fiber.Ctx) error {
	data, err := os.ReadFile(c.ConfigPath)
	if err != nil {
		c.Log.Error("failed to read config file", "error", err)
		return fiber.NewError(fiber.StatusInternalServerError, "failed to read config file")
	}

	var raw json.RawMessage = data
	return ctx.JSON(model.WebResponse[json.RawMessage]{Data: raw})
}

// Update validates and saves config to disk atomically.
func (c *ConfigController) Update(ctx *fiber.Ctx) error {
	var cfg MoxyConfig
	if err := ctx.BodyParser(&cfg); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid JSON: "+err.Error())
	}

	if errs := cfg.Validate(); errs != nil {
		return ctx.Status(fiber.StatusBadRequest).JSON(fiber.Map{"errors": errs})
	}

	// Marshal with indentation to keep config.json human-readable
	data, err := json.MarshalIndent(cfg, "", "    ")
	if err != nil {
		c.Log.Error("failed to marshal config", "error", err)
		return fiber.NewError(fiber.StatusInternalServerError, "failed to marshal config")
	}

	// Atomic write: temp file → rename
	tmpPath := c.ConfigPath + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		c.Log.Error("failed to write temp config", "error", err)
		return fiber.NewError(fiber.StatusInternalServerError, "failed to write config")
	}
	if err := os.Rename(tmpPath, c.ConfigPath); err != nil {
		c.Log.Error("failed to rename config", "error", err)
		os.Remove(tmpPath)
		return fiber.NewError(fiber.StatusInternalServerError, "failed to save config")
	}

	c.Log.Info("config updated via dashboard")
	return ctx.JSON(model.WebResponse[json.RawMessage]{Data: data})
}

// Restart triggers a systemd service restart.
func (c *ConfigController) Restart(ctx *fiber.Ctx) error {
	c.Log.Warn("service restart requested via dashboard")

	go func() {
		// Give HTTP response time to flush
		time.Sleep(500 * time.Millisecond)

		cmd := exec.Command("systemctl", "restart", "moxy")
		if err := cmd.Run(); err != nil {
			c.Log.Error("restart failed", "error", err)
		}
	}()

	return ctx.JSON(model.WebResponse[string]{Data: "restarting"})
}

// --- Config structs and validation ---

// MoxyConfig mirrors the config.json structure for validation.
type MoxyConfig struct {
	Proxy   ProxyConfig   `json:"proxy"`
	API     APIConfig     `json:"api"`
	Devices DevicesConfig `json:"devices"`
	Slots   SlotsConfig   `json:"slots"`
	DNS     DNSConfig     `json:"dns"`
	Traffic TrafficConfig `json:"traffic"`
	SSE     SSEConfig     `json:"sse"`
	Server  ServerConfig  `json:"server"`
	Log     LogConfig     `json:"log"`
}

type ProxyConfig struct {
	Port              int    `json:"port"`
	SlotPortStart     int    `json:"slot_port_start"`
	IPv6Port          int    `json:"ipv6_port"`
	IPv6SlotPortStart int    `json:"ipv6_slot_port_start"`
	SourceIPStrategy  string `json:"source_ip_strategy"`
}

type APIConfig struct {
	Port int `json:"port"`
}

type DevicesConfig struct {
	GracePeriodSeconds         int `json:"grace_period_seconds"`
	WatcherReconnectMaxSeconds int `json:"watcher_reconnect_max_seconds"`
	DrainTimeoutSeconds        int `json:"drain_timeout_seconds"`
}

type SlotsConfig struct {
	MaxSlotsPerDevice              int    `json:"max_slots_per_device"`
	IPCheckHost                    string `json:"ip_check_host"`
	MonitorFastIntervalSeconds     int    `json:"monitor_fast_interval_seconds"`
	MonitorSteadyIntervalSeconds   int    `json:"monitor_steady_interval_seconds"`
	MonitorRecoveryIntervalSeconds int    `json:"monitor_recovery_interval_seconds"`
	MonitorFastTicks               int    `json:"monitor_fast_ticks"`
	MonitorUnhealthyThreshold      int    `json:"monitor_unhealthy_threshold"`
}

type DNSConfig struct {
	CacheMaxEntriesPerDevice int `json:"cache_max_entries_per_device"`
	CacheMinTTLSeconds       int `json:"cache_min_ttl_seconds"`
	CacheMaxTTLSeconds       int `json:"cache_max_ttl_seconds"`
}

type TrafficConfig struct {
	MaxTracked int `json:"max_tracked"`
}

type SSEConfig struct {
	DebounceMs       int `json:"debounce_ms"`
	HeartbeatSeconds int `json:"heartbeat_seconds"`
	MaxClients       int `json:"max_clients"`
}

type ServerConfig struct {
	ShutdownDrainSeconds int `json:"shutdown_drain_seconds"`
}

type LogConfig struct {
	Level          string `json:"level"`
	Format         string `json:"format"`
	RingBufferSize int    `json:"ring_buffer_size,omitempty"`
}

// Validate checks all config fields and returns a map of field path → error message.
// Returns nil if everything is valid.
func (cfg *MoxyConfig) Validate() map[string]string {
	errs := make(map[string]string)

	// Proxy
	if cfg.Proxy.Port < 1 || cfg.Proxy.Port > 65535 {
		errs["proxy.port"] = "must be between 1 and 65535"
	}
	if cfg.Proxy.SlotPortStart < 1 || cfg.Proxy.SlotPortStart > 65535 {
		errs["proxy.slot_port_start"] = "must be between 1 and 65535"
	}
	if cfg.Proxy.IPv6Port < 0 || cfg.Proxy.IPv6Port > 65535 {
		errs["proxy.ipv6_port"] = "must be between 0 and 65535 (0 = disabled)"
	}
	if cfg.Proxy.IPv6SlotPortStart < 0 || cfg.Proxy.IPv6SlotPortStart > 65535 {
		errs["proxy.ipv6_slot_port_start"] = "must be between 0 and 65535 (0 = disabled)"
	}
	validStrategies := map[string]bool{"random": true, "round-robin": true, "least-connections": true}
	if !validStrategies[cfg.Proxy.SourceIPStrategy] {
		errs["proxy.source_ip_strategy"] = "must be one of: random, round-robin, least-connections"
	}

	// API
	if cfg.API.Port < 1 || cfg.API.Port > 65535 {
		errs["api.port"] = "must be between 1 and 65535"
	}

	// Devices
	if cfg.Devices.GracePeriodSeconds < 1 {
		errs["devices.grace_period_seconds"] = "must be >= 1"
	}
	if cfg.Devices.WatcherReconnectMaxSeconds < 1 {
		errs["devices.watcher_reconnect_max_seconds"] = "must be >= 1"
	}
	if cfg.Devices.DrainTimeoutSeconds < 1 {
		errs["devices.drain_timeout_seconds"] = "must be >= 1"
	}

	// Slots
	if cfg.Slots.MaxSlotsPerDevice < 1 || cfg.Slots.MaxSlotsPerDevice > 1000 {
		errs["slots.max_slots_per_device"] = "must be between 1 and 1000"
	}
	if cfg.Slots.IPCheckHost == "" {
		errs["slots.ip_check_host"] = "must not be empty"
	}
	if cfg.Slots.MonitorFastIntervalSeconds < 1 {
		errs["slots.monitor_fast_interval_seconds"] = "must be >= 1"
	}
	if cfg.Slots.MonitorSteadyIntervalSeconds < 1 {
		errs["slots.monitor_steady_interval_seconds"] = "must be >= 1"
	}
	if cfg.Slots.MonitorRecoveryIntervalSeconds < 1 {
		errs["slots.monitor_recovery_interval_seconds"] = "must be >= 1"
	}
	if cfg.Slots.MonitorFastTicks < 1 {
		errs["slots.monitor_fast_ticks"] = "must be >= 1"
	}
	if cfg.Slots.MonitorUnhealthyThreshold < 1 {
		errs["slots.monitor_unhealthy_threshold"] = "must be >= 1"
	}

	// DNS
	if cfg.DNS.CacheMaxEntriesPerDevice < 100 {
		errs["dns.cache_max_entries_per_device"] = "must be >= 100"
	}
	if cfg.DNS.CacheMinTTLSeconds < 1 {
		errs["dns.cache_min_ttl_seconds"] = "must be >= 1"
	}
	if cfg.DNS.CacheMaxTTLSeconds < cfg.DNS.CacheMinTTLSeconds {
		errs["dns.cache_max_ttl_seconds"] = "must be >= cache_min_ttl_seconds"
	}

	// Traffic
	if cfg.Traffic.MaxTracked < 100 {
		errs["traffic.max_tracked"] = "must be >= 100"
	}

	// SSE
	if cfg.SSE.DebounceMs < 100 {
		errs["sse.debounce_ms"] = "must be >= 100"
	}
	if cfg.SSE.HeartbeatSeconds < 5 {
		errs["sse.heartbeat_seconds"] = "must be >= 5"
	}
	if cfg.SSE.MaxClients < 1 {
		errs["sse.max_clients"] = "must be >= 1"
	}

	// Server
	if cfg.Server.ShutdownDrainSeconds < 1 {
		errs["server.shutdown_drain_seconds"] = "must be >= 1"
	}

	// Log
	validLevels := map[string]bool{"debug": true, "info": true, "warn": true, "error": true}
	if !validLevels[cfg.Log.Level] {
		errs["log.level"] = "must be one of: debug, info, warn, error"
	}
	validFormats := map[string]bool{"json": true, "text": true}
	if !validFormats[cfg.Log.Format] {
		errs["log.format"] = "must be one of: json, text"
	}
	if cfg.Log.RingBufferSize != 0 && cfg.Log.RingBufferSize < 100 {
		errs["log.ring_buffer_size"] = "must be >= 100 (or 0 for default)"
	}

	if len(errs) == 0 {
		return nil
	}
	return errs
}
