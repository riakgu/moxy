//go:build linux

package config

import (
	"embed"
	"time"
	"log/slog"

	"github.com/gofiber/fiber/v2"
	"github.com/spf13/viper"

	httpdelivery "github.com/riakgu/moxy/internal/delivery/http"
	"github.com/riakgu/moxy/internal/delivery/http/route"
	"github.com/riakgu/moxy/internal/delivery/proxy"
	"github.com/riakgu/moxy/internal/delivery/sse"
	"github.com/riakgu/moxy/internal/gateway/adb"
	"github.com/riakgu/moxy/internal/gateway/netns"
	"github.com/riakgu/moxy/internal/model/converter"
	"github.com/riakgu/moxy/internal/repository"
	"github.com/riakgu/moxy/internal/usecase"
)

type BootstrapConfig struct {
	Viper    *viper.Viper
	Logger   *slog.Logger
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
	RingHandler   *sse.RingHandler
}

func Bootstrap(cfg *BootstrapConfig) *BootstrapResult {
	ringSize := cfg.Viper.GetInt("log.ring_buffer_size")
	if ringSize == 0 {
		ringSize = 1000
	}
	level := parseLevel(cfg.Viper.GetString("log.level"))
	logRepo := repository.NewLogRepository(nil, ringSize)
	ringHandler := sse.NewRingHandler(logRepo, level)

	cfg.Logger = NewLoggerWithRing(cfg.Viper, ringHandler)

	deviceLog := cfg.Logger.With("component", "device")
	slotLog := cfg.Logger.With("component", "slot")
	monitorLog := cfg.Logger.With("component", "slot.monitor")
	proxyLog := cfg.Logger.With("component", "proxy")
	dnsLog := cfg.Logger.With("component", "dns")
	trafficLog := cfg.Logger.With("component", "traffic")
	adbLog := cfg.Logger.With("component", "adb")
	netnsLog := cfg.Logger.With("component", "netns")
	sseLog := cfg.Logger.With("component", "sse")

	deviceRepo := repository.NewDeviceRepository(deviceLog)
	slotRepo := repository.NewSlotRepository(slotLog)
	maxTracked := cfg.Viper.GetInt("traffic.max_tracked")
	if maxTracked == 0 {
		maxTracked = 5000
	}
	trafficRepo := repository.NewTrafficRepository(trafficLog, maxTracked)

	sseMaxClients := cfg.Viper.GetInt("sse.max_clients")
	if sseMaxClients == 0 {
		sseMaxClients = 10
	}
	hub := sse.NewEventHub(sseLog, sseMaxClients)
	ringHandler.SetHub(hub)

	adbGateway := adb.NewADBGateway(adbLog)
	provisioner := netns.NewProvisioner(netnsLog)
	discovery := netns.NewDiscovery(netnsLog, cfg.Viper.GetString("slots.ip_check_host"))
	dnsRepo := repository.NewDNSCacheRepository(dnsLog, cfg.Viper.GetInt("dns.cache_max_entries_per_device"))
	resolver := netns.NewCachingResolver(dnsLog, dnsRepo, netns.CacheConfig{
		MinTTL: time.Duration(cfg.Viper.GetInt("dns.cache_min_ttl_seconds")) * time.Second,
		MaxTTL: time.Duration(cfg.Viper.GetInt("dns.cache_max_ttl_seconds")) * time.Second,
	})
	dialer := netns.NewSetnsDialer(netnsLog, resolver)

	maxSlots := cfg.Viper.GetInt("slots.max_slots_per_device")
	strategy := usecase.NewBalancingStrategy(cfg.Viper.GetString("proxy.source_ip_strategy"))
	slotUC := usecase.NewSlotUseCase(
		slotLog, slotRepo, discovery,
		provisioner,
		maxSlots,
	)

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
	slotMonitor := usecase.NewSlotMonitorUseCase(monitorLog, slotRepo, discovery, provisioner, monitorConfig)
	slotUC.SetMonitor(slotMonitor)

	slotUC.EventPub = hub
	slotMonitor.EventPub = hub
	ispProbe := netns.NewISPProbe(netnsLog)

	adbWatcher := adb.NewADBWatcher(adbLog, cfg.Viper.GetInt("devices.watcher_reconnect_max_seconds")*1000)
	gracePeriod := time.Duration(cfg.Viper.GetInt("devices.grace_period_seconds")) * time.Second
	if gracePeriod == 0 {
		gracePeriod = 30 * time.Second
	}
	drainTimeout := time.Duration(cfg.Viper.GetInt("devices.drain_timeout_seconds")) * time.Second
	if drainTimeout == 0 {
		drainTimeout = 10 * time.Second
	}

	deviceUC := usecase.NewDeviceUseCase(deviceLog,
		deviceRepo, adbGateway, provisioner, slotRepo, slotUC, ispProbe,
		adbWatcher, gracePeriod, drainTimeout, trafficRepo)
	deviceUC.EventPub = hub
	trafficUC := usecase.NewTrafficUseCase(trafficLog, trafficRepo)

	proxyUC := usecase.NewProxyUseCase(proxyLog, slotRepo, deviceRepo, dialer, strategy, trafficRepo)
	proxyUC.EventPub = hub

	// Must be created before controllers so we can inject it.
	proxyPort := cfg.Viper.GetInt("proxy.ipv4.port")
	slotPortStart := cfg.Viper.GetInt("proxy.ipv4.slot_port_start")
	ipv6Port := cfg.Viper.GetInt("proxy.ipv6.port")
	ipv6SlotPortStart := cfg.Viper.GetInt("proxy.ipv6.slot_port_start")
	portHandler := proxy.NewPortBasedHandler(proxyLog, proxyUC, proxyPort, slotPortStart, ipv6Port, ipv6SlotPortStart)

	// Wire teardown callback — cleans up stale proxy listeners after background device teardown
	deviceUC.OnTeardown = func() {
		slotNames := slotUC.GetSlotNames()
		onlineAliases := deviceUC.ListOnlineAliases()
		portHandler.SyncSlots(slotNames)
		portHandler.SyncDevices(onlineAliases)
		portHandler.SyncSlotsIPv6(slotNames)
		portHandler.SyncDevicesIPv6(onlineAliases)
	}

	deviceCtrl := httpdelivery.NewDeviceController(deviceUC, deviceLog, portHandler, slotUC.GetSlotNames)
	slotCtrl := httpdelivery.NewSlotController(slotUC, slotLog, portHandler)

	dnsUC := usecase.NewDNSUseCase(dnsLog, dnsRepo)
	dnsCtrl := httpdelivery.NewDNSController(dnsUC, dnsLog)

	trafficCtrl := httpdelivery.NewTrafficController(trafficUC, trafficLog)

	configCtrl := httpdelivery.NewConfigController(
		cfg.Logger.With("component", "config"),
		"config.json",
	)

	sseDebounce := cfg.Viper.GetInt("sse.debounce_ms")
	sseHeartbeat := cfg.Viper.GetInt("sse.heartbeat_seconds")
	sseTrafficLimit := cfg.Viper.GetInt("sse.traffic_snapshot_limit")
	if sseTrafficLimit == 0 {
		sseTrafficLimit = 100
	}

	// Inject traffic snapshot config into proxyUC
	proxyUC.TrafficUC = trafficUC
	proxyUC.SnapshotLimit = sseTrafficLimit
	proxyUC.DNSUC = dnsUC

	sseSnapshot := func() (*sse.InitPayload, error) {
		devices, err := deviceUC.List()
		if err != nil {
			return nil, err
		}
		slots := slotUC.ListAll()
		logs := converter.LogEntriesToResponse(logRepo.GetRecent())
		traffic := trafficUC.ListTop(sseTrafficLimit)
		dnsStats := dnsUC.GetCacheStats()
		return &sse.InitPayload{Devices: devices, Slots: slots, Logs: logs, Traffic: traffic, DNSStats: dnsStats}, nil
	}
	sseHandler := sse.NewSSEHandler(hub, sseLog, sseSnapshot, sseDebounce, sseHeartbeat)

	// Routes
	routeConfig := &route.RouteConfig{
		App:               cfg.Fiber,
		DeviceController:  deviceCtrl,
		SlotController:    slotCtrl,
		DNSController:     dnsCtrl,
		TrafficController: trafficCtrl,
		ConfigController:  configCtrl,
		SSEHandler:        sseHandler,
		Log:               cfg.Logger.With("component", "api"),
		StaticFS:          cfg.StaticFS,
	}

	return &BootstrapResult{
		SlotUseCase:   slotUC,
		DeviceUseCase: deviceUC,
		SlotMonitor:   slotMonitor,
		PortHandler:   portHandler,
		RouteConfig:   routeConfig,
		EventHub:      hub,
		RingHandler:   ringHandler,
	}
}
