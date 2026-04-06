//go:build linux

package config

import (
	"embed"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/sirupsen/logrus"
	"github.com/spf13/viper"

	httpdelivery "github.com/riakgu/moxy/internal/delivery/http"
	"github.com/riakgu/moxy/internal/delivery/http/route"
	"github.com/riakgu/moxy/internal/delivery/proxy"
	"github.com/riakgu/moxy/internal/gateway/adb"
	"github.com/riakgu/moxy/internal/gateway/netns"
	"github.com/riakgu/moxy/internal/repository"
	"github.com/riakgu/moxy/internal/usecase"
)

type BootstrapConfig struct {
	Viper    *viper.Viper
	Logger   *logrus.Logger
	Fiber    *fiber.App
	StaticFS embed.FS
}

type BootstrapResult struct {
	SlotUseCase   *usecase.SlotUseCase
	DeviceUseCase *usecase.DeviceUseCase
	SlotMonitor   *usecase.SlotMonitorUseCase
	PortHandler   *proxy.PortBasedHandler
	RouteConfig   *route.RouteConfig
}

func Bootstrap(cfg *BootstrapConfig) *BootstrapResult {
	// Repositories (all in-memory)
	deviceRepo := repository.NewDeviceRepository(cfg.Logger)
	slotRepo := repository.NewSlotRepository(cfg.Logger)

	// Gateways
	adbGateway := adb.NewADBGateway(cfg.Logger)
	provisioner := netns.NewProvisioner(cfg.Logger)
	discovery := netns.NewDiscovery(cfg.Logger, cfg.Viper.GetString("slots.ip_check_host"))
	dialer := netns.NewSetnsDialer(cfg.Logger)

	// UseCases
	maxSlots := cfg.Viper.GetInt("slots.max_slots_per_device")
	strategy := usecase.NewSlotStrategy(cfg.Viper.GetString("proxy.source_ip_strategy"))
	slotUC := usecase.NewSlotUseCase(
		cfg.Logger, slotRepo, discovery,
		provisioner,
		maxSlots,
	)

	// Slot monitor (per-slot discovery goroutines)
	monitorConfig := usecase.SlotMonitorConfig{
		FastInterval:     time.Duration(cfg.Viper.GetInt("slots.monitor_fast_interval_seconds")) * time.Second,
		SteadyInterval:     time.Duration(cfg.Viper.GetInt("slots.monitor_steady_interval_seconds")) * time.Second,
		RecoveryInterval:   time.Duration(cfg.Viper.GetInt("slots.monitor_recovery_interval_seconds")) * time.Second,
		FastTicks:          cfg.Viper.GetInt("slots.monitor_fast_ticks"),
		UnhealthyThreshold: cfg.Viper.GetInt("slots.monitor_unhealthy_threshold"),
	}
	if monitorConfig.FastInterval == 0 {
		monitorConfig.FastInterval = 10 * time.Second
	}
	if monitorConfig.SteadyInterval == 0 {
		monitorConfig.SteadyInterval = 60 * time.Second
	}
	if monitorConfig.RecoveryInterval == 0 {
		monitorConfig.RecoveryInterval = 15 * time.Second
	}
	if monitorConfig.FastTicks == 0 {
		monitorConfig.FastTicks = 6
	}
	if monitorConfig.UnhealthyThreshold == 0 {
		monitorConfig.UnhealthyThreshold = 3
	}
	slotMonitor := usecase.NewSlotMonitorUseCase(cfg.Logger, slotRepo, discovery, provisioner, monitorConfig)
	slotUC.SetMonitor(slotMonitor)
	ispProbe := netns.NewISPProbe(cfg.Logger)

	// ADB device watcher (event-driven device monitoring)
	adbWatcher := adb.NewADBWatcher(cfg.Logger, cfg.Viper.GetInt("devices.watcher_reconnect_max_seconds")*1000)
	gracePeriod := time.Duration(cfg.Viper.GetInt("devices.grace_period_seconds")) * time.Second
	if gracePeriod == 0 {
		gracePeriod = 30 * time.Second
	}
	drainTimeout := time.Duration(cfg.Viper.GetInt("devices.drain_timeout_seconds")) * time.Second
	if drainTimeout == 0 {
		drainTimeout = 10 * time.Second
	}

	deviceUC := usecase.NewDeviceUseCase(cfg.Logger,
		deviceRepo, adbGateway, provisioner, slotRepo, slotUC, ispProbe,
		adbWatcher, gracePeriod, drainTimeout)
	deviceUC.SetMonitor(slotMonitor)
	proxyUC := usecase.NewProxyUseCase(cfg.Logger, slotRepo, dialer, strategy)

	// Port-based handler (shared + device + per-slot mux listeners)
	// Must be created before controllers so we can inject it.
	proxyPort := cfg.Viper.GetInt("proxy.port")
	slotPortStart := cfg.Viper.GetInt("proxy.slot_port_start")
	portHandler := proxy.NewPortBasedHandler(cfg.Logger, proxyUC, proxyPort, slotPortStart)

	// Controllers
	deviceCtrl := httpdelivery.NewDeviceController(deviceUC, cfg.Logger, portHandler, slotUC.GetSlotNames)
	slotCtrl := httpdelivery.NewSlotController(slotUC, cfg.Logger, portHandler)

	// Routes
	routeConfig := &route.RouteConfig{
		App:              cfg.Fiber,
		DeviceController: deviceCtrl,
		SlotController:   slotCtrl,
		Log:              cfg.Logger,
		StaticFS:         cfg.StaticFS,
	}

	return &BootstrapResult{
		SlotUseCase:   slotUC,
		DeviceUseCase: deviceUC,
		SlotMonitor:   slotMonitor,
		PortHandler:   portHandler,
		RouteConfig:   routeConfig,
	}
}
