//go:build linux

package usecase

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"

	"github.com/riakgu/moxy/internal/model"
)

type ServiceManager interface {
	Restart() error
}

type ConfigUseCase struct {
	Log        *slog.Logger
	ConfigPath string
	Service    ServiceManager
}

func NewConfigUseCase(log *slog.Logger, configPath string, service ServiceManager) *ConfigUseCase {
	return &ConfigUseCase{
		Log:        log,
		ConfigPath: configPath,
		Service:    service,
	}
}

func (uc *ConfigUseCase) GetConfig() (json.RawMessage, error) {
	data, err := os.ReadFile(uc.ConfigPath)
	if err != nil {
		uc.Log.Error("failed to read config file", "error", err)
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}
	return json.RawMessage(data), nil
}

func (uc *ConfigUseCase) UpdateConfig(cfg *model.MoxyConfig) (json.RawMessage, error) {
	if errs := uc.validateConfig(cfg); errs != nil {
		return nil, &ValidationError{Fields: errs}
	}

	data, err := json.MarshalIndent(cfg, "", "    ")
	if err != nil {
		uc.Log.Error("failed to marshal config", "error", err)
		return nil, fmt.Errorf("failed to marshal config: %w", err)
	}

	tmpPath := uc.ConfigPath + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		uc.Log.Error("failed to write temp config", "error", err)
		return nil, fmt.Errorf("failed to write config: %w", err)
	}
	if err := os.Rename(tmpPath, uc.ConfigPath); err != nil {
		uc.Log.Error("failed to rename config", "error", err)
		_ = os.Remove(tmpPath)
		return nil, fmt.Errorf("failed to save config: %w", err)
	}

	uc.Log.Info("config updated via dashboard")
	return json.RawMessage(data), nil
}

func (uc *ConfigUseCase) RestartService() error {
	uc.Log.Warn("service restart requested via dashboard")
	return uc.Service.Restart()
}

type ValidationError struct {
	Fields map[string]string
}

func (e *ValidationError) Error() string {
	return "validation failed"
}

func (uc *ConfigUseCase) validateConfig(cfg *model.MoxyConfig) map[string]string {
	errs := make(map[string]string)

	if cfg.Proxy.IPv4.Port < 1 || cfg.Proxy.IPv4.Port > 65535 {
		errs["proxy.ipv4.port"] = "must be between 1 and 65535"
	}
	if cfg.Proxy.IPv4.SlotPortStart < 1 || cfg.Proxy.IPv4.SlotPortStart > 65535 {
		errs["proxy.ipv4.slot_port_start"] = "must be between 1 and 65535"
	}
	if cfg.Proxy.IPv6.Port < 0 || cfg.Proxy.IPv6.Port > 65535 {
		errs["proxy.ipv6.port"] = "must be between 0 and 65535 (0 = disabled)"
	}
	if cfg.Proxy.IPv6.SlotPortStart < 0 || cfg.Proxy.IPv6.SlotPortStart > 65535 {
		errs["proxy.ipv6.slot_port_start"] = "must be between 0 and 65535 (0 = disabled)"
	}
	validStrategies := map[string]bool{"random": true, "round-robin": true, "least-connections": true}
	if !validStrategies[cfg.Proxy.SourceIPStrategy] {
		errs["proxy.source_ip_strategy"] = "must be one of: random, round-robin, least-connections"
	}
	if cfg.Proxy.UDPIdleTimeoutSeconds != 0 && cfg.Proxy.UDPIdleTimeoutSeconds < 10 {
		errs["proxy.udp_idle_timeout_seconds"] = "must be >= 10 (or 0 for default)"
	}
	if cfg.Proxy.UDPMaxAssociations != 0 && (cfg.Proxy.UDPMaxAssociations < 1 || cfg.Proxy.UDPMaxAssociations > 10000) {
		errs["proxy.udp_max_associations"] = "must be between 1 and 10000 (or 0 for default)"
	}

	if cfg.API.Port < 1 || cfg.API.Port > 65535 {
		errs["api.port"] = "must be between 1 and 65535"
	}

	if cfg.Devices.GracePeriodSeconds < 1 {
		errs["devices.grace_period_seconds"] = "must be >= 1"
	}
	if cfg.Devices.WatcherReconnectMaxSeconds < 1 {
		errs["devices.watcher_reconnect_max_seconds"] = "must be >= 1"
	}
	if cfg.Devices.DrainTimeoutSeconds < 1 {
		errs["devices.drain_timeout_seconds"] = "must be >= 1"
	}

	if cfg.Slots.MaxSlots < 1 || cfg.Slots.MaxSlots > 10000 {
		errs["slots.max_slots"] = "must be between 1 and 10000"
	}
	if cfg.Slots.MaxSlotsPerDevice < 1 || cfg.Slots.MaxSlotsPerDevice > cfg.Slots.MaxSlots {
		errs["slots.max_slots_per_device"] = fmt.Sprintf("must be between 1 and %d (max_slots)", cfg.Slots.MaxSlots)
	}
	if cfg.Slots.IPCheckHost == "" {
		errs["slots.ip_check_host"] = "must not be empty"
	}
	if cfg.Slots.MonitorSteadyIntervalSeconds < 1 {
		errs["slots.monitor_steady_interval_seconds"] = "must be >= 1"
	}
	if cfg.Slots.MonitorRecoveryIntervalSeconds < 1 {
		errs["slots.monitor_recovery_interval_seconds"] = "must be >= 1"
	}
	if cfg.Slots.MonitorUnhealthyThreshold < 1 {
		errs["slots.monitor_unhealthy_threshold"] = "must be >= 1"
	}

	if cfg.DNS.CacheMaxEntriesPerDevice < 100 {
		errs["dns.cache_max_entries_per_device"] = "must be >= 100"
	}
	if cfg.DNS.CacheMinTTLSeconds < 1 {
		errs["dns.cache_min_ttl_seconds"] = "must be >= 1"
	}
	if cfg.DNS.CacheMaxTTLSeconds < cfg.DNS.CacheMinTTLSeconds {
		errs["dns.cache_max_ttl_seconds"] = "must be >= cache_min_ttl_seconds"
	}

	if cfg.Traffic.MaxTracked < 100 {
		errs["traffic.max_tracked"] = "must be >= 100"
	}

	if cfg.SSE.DebounceMs < 100 {
		errs["sse.debounce_ms"] = "must be >= 100"
	}
	if cfg.SSE.HeartbeatSeconds < 5 {
		errs["sse.heartbeat_seconds"] = "must be >= 5"
	}
	if cfg.SSE.MaxClients < 1 {
		errs["sse.max_clients"] = "must be >= 1"
	}
	if cfg.SSE.TrafficSnapshotLimit < 10 {
		errs["sse.traffic_snapshot_limit"] = "must be >= 10"
	}

	if cfg.Server.ShutdownDrainSeconds < 1 {
		errs["server.shutdown_drain_seconds"] = "must be >= 1"
	}

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
