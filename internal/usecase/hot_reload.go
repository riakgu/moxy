//go:build linux

package usecase

import (
	"time"
	"log/slog"

	"github.com/riakgu/moxy/internal/model"
)

// HotReloader applies config changes to live components without restart.
type HotReloader struct {
	Log         *slog.Logger
	ProxyUC     *ProxyUseCase
	DeviceUC    *DeviceUseCase
	SlotUC      *SlotUseCase
	SlotMonitor *SlotMonitorUseCase

	// Setters for gateway/infra fields (avoids import cycle)
	SetIPCheckHost     func(string)
	SetWatcherBackoff  func(int)
	SetDNSCacheTTL     func(min, max time.Duration)
	SetDNSCacheMaxSize func(int)
	SetTrafficMax      func(int)
}

// Apply updates all hot-reloadable fields from the given config.
// Returns true if any restart-required fields changed.
func (h *HotReloader) Apply(cfg *model.MoxyConfig) {
	// Proxy strategy
	h.ProxyUC.strategy = cfg.Proxy.SourceIPStrategy
	h.ProxyUC.SnapshotLimit = cfg.SSE.TrafficSnapshotLimit

	// Device timings
	h.DeviceUC.GracePeriod = time.Duration(cfg.Devices.GracePeriodSeconds) * time.Second
	h.DeviceUC.DrainTimeout = time.Duration(cfg.Devices.DrainTimeoutSeconds) * time.Second

	// Slot limits
	h.SlotUC.MaxSlotsPerDevice = cfg.Slots.MaxSlotsPerDevice

	// Slot monitor intervals
	h.SlotMonitor.Config.SteadyInterval = time.Duration(cfg.Slots.MonitorSteadyIntervalSeconds) * time.Second
	h.SlotMonitor.Config.RecoveryInterval = time.Duration(cfg.Slots.MonitorRecoveryIntervalSeconds) * time.Second
	h.SlotMonitor.Config.UnhealthyThreshold = cfg.Slots.MonitorUnhealthyThreshold

	// Gateway/infra via setters (avoids import cycle)
	if h.SetIPCheckHost != nil {
		h.SetIPCheckHost(cfg.Slots.IPCheckHost)
	}
	if h.SetWatcherBackoff != nil {
		h.SetWatcherBackoff(cfg.Devices.WatcherReconnectMaxSeconds * 1000)
	}
	if h.SetDNSCacheTTL != nil {
		h.SetDNSCacheTTL(
			time.Duration(cfg.DNS.CacheMinTTLSeconds)*time.Second,
			time.Duration(cfg.DNS.CacheMaxTTLSeconds)*time.Second,
		)
	}
	if h.SetDNSCacheMaxSize != nil {
		h.SetDNSCacheMaxSize(cfg.DNS.CacheMaxEntriesPerDevice)
	}
	if h.SetTrafficMax != nil {
		h.SetTrafficMax(cfg.Traffic.MaxTracked)
	}

	h.Log.Info("hot config applied")
}
