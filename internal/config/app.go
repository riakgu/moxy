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
	"github.com/riakgu/moxy/internal/delivery/sse"
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
	EventHub      *sse.EventHub
}

func Bootstrap(cfg *BootstrapConfig) *BootstrapResult {
	// Repositories (all in-memory)
	deviceRepo := repository.NewDeviceRepository(cfg.Logger)
	slotRepo := repository.NewSlotRepository(cfg.Logger)
	maxTracked := cfg.Viper.GetInt("traffic.max_tracked")
	if maxTracked == 0 {
		maxTracked = 5000
	}
	trafficRepo := repository.NewTrafficRepository(cfg.Logger, maxTracked)

	// SSE Event Hub
	sseMaxClients := cfg.Viper.GetInt("sse.max_clients")
	if sseMaxClients == 0 {
		sseMaxClients = 10
	}
	hub := sse.NewEventHub(cfg.Logger, sseMaxClients)

	// Gateways
	adbGateway := adb.NewADBGateway(cfg.Logger)
	provisioner := netns.NewProvisioner(cfg.Logger)
	discovery := netns.NewDiscovery(cfg.Logger, cfg.Viper.GetString("slots.ip_check_host"))
	// DNS cache
	resolver := netns.NewCachingResolver(cfg.Logger, netns.CacheConfig{
		MaxEntriesPerDevice: cfg.Viper.GetInt("dns.cache_max_entries_per_device"),
		MinTTL:              time.Duration(cfg.Viper.GetInt("dns.cache_min_ttl_seconds")) * time.Second,
		MaxTTL:              time.Duration(cfg.Viper.GetInt("dns.cache_max_ttl_seconds")) * time.Second,
	})
	dialer := netns.NewSetnsDialer(cfg.Logger, resolver)

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

	// Inject EventPublisher into usecases
	slotUC.EventPub = hub
	slotMonitor.EventPub = hub
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
		adbWatcher, gracePeriod, drainTimeout, trafficRepo)
	deviceUC.SetMonitor(slotMonitor)
	deviceUC.EventPub = hub
	proxyUC := usecase.NewProxyUseCase(cfg.Logger, slotRepo, deviceRepo, dialer, strategy, trafficRepo)
	proxyUC.EventPub = hub

	// Port-based handler (shared + device + per-slot mux listeners)
	// Must be created before controllers so we can inject it.
	proxyPort := cfg.Viper.GetInt("proxy.port")
	slotPortStart := cfg.Viper.GetInt("proxy.slot_port_start")
	ipv6Port := cfg.Viper.GetInt("proxy.ipv6_port")
	ipv6SlotPortStart := cfg.Viper.GetInt("proxy.ipv6_slot_port_start")
	portHandler := proxy.NewPortBasedHandler(cfg.Logger, proxyUC, proxyPort, slotPortStart, ipv6Port, ipv6SlotPortStart)

	// Wire teardown callback — cleans up stale proxy listeners after background device teardown
	deviceUC.OnTeardown = func() {
		slotNames := slotUC.GetSlotNames()
		onlineAliases := deviceUC.ListOnlineAliases()
		portHandler.SyncSlots(slotNames)
		portHandler.SyncDevices(onlineAliases)
		portHandler.SyncSlotsIPv6(slotNames)
		portHandler.SyncDevicesIPv6(onlineAliases)
	}

	// Controllers
	deviceCtrl := httpdelivery.NewDeviceController(deviceUC, cfg.Logger, portHandler, slotUC.GetSlotNames)
	slotCtrl := httpdelivery.NewSlotController(slotUC, cfg.Logger, portHandler)

	// DNS
	dnsUC := usecase.NewDNSUseCase(cfg.Logger, resolver)
	dnsCtrl := httpdelivery.NewDNSController(dnsUC, cfg.Logger)

	// Traffic
	trafficUC := usecase.NewTrafficUseCase(cfg.Logger, trafficRepo)
	trafficCtrl := httpdelivery.NewTrafficController(trafficUC, cfg.Logger)

	// SSE handler
	sseDebounce := cfg.Viper.GetInt("sse.debounce_ms")
	sseHeartbeat := cfg.Viper.GetInt("sse.heartbeat_seconds")
	sseSnapshot := func() (*sse.InitPayload, error) {
		devices, err := deviceUC.List()
		if err != nil {
			return nil, err
		}
		slots := slotUC.ListAll()
		return &sse.InitPayload{Devices: devices, Slots: slots}, nil
	}
	sseHandler := sse.NewSSEHandler(hub, cfg.Logger, sseSnapshot, sseDebounce, sseHeartbeat)

	// Routes
	routeConfig := &route.RouteConfig{
		App:               cfg.Fiber,
		DeviceController:  deviceCtrl,
		SlotController:    slotCtrl,
		DNSController:     dnsCtrl,
		TrafficController: trafficCtrl,
		SSEHandler:        sseHandler,
		Log:               cfg.Logger,
		StaticFS:          cfg.StaticFS,
	}

	return &BootstrapResult{
		SlotUseCase:   slotUC,
		DeviceUseCase: deviceUC,
		SlotMonitor:   slotMonitor,
		PortHandler:   portHandler,
		RouteConfig:   routeConfig,
		EventHub:      hub,
	}
}
