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
	"github.com/riakgu/moxy/internal/gateway/systemd"
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
	ADBGateway    *adb.ADBGateway
}

type bootstrapper struct {
	cfg *BootstrapConfig
	v   *viper.Viper

	// logging
	logRepo     *repository.LogRepository
	ringHandler *sse.RingHandler

	// component loggers
	deviceLog  *slog.Logger
	slotLog    *slog.Logger
	monitorLog *slog.Logger
	proxyLog   *slog.Logger
	dnsLog     *slog.Logger
	trafficLog *slog.Logger
	adbLog     *slog.Logger
	netnsLog   *slog.Logger
	sseLog     *slog.Logger

	// repositories
	deviceRepo  *repository.DeviceRepository
	slotRepo    *repository.SlotRepository
	trafficRepo *repository.TrafficRepository
	dnsRepo     *repository.DNSCacheRepository

	// gateways
	adbGateway *adb.ADBGateway
	adbWatcher *adb.ADBWatcher
	provisioner *netns.Provisioner
	discovery   *netns.Discovery
	resolver    *netns.CachingResolver
	dialer      *netns.SetnsDialer
	ispProbe    *netns.ISPProbe

	// usecases
	slotUC      *usecase.SlotUseCase
	slotMonitor *usecase.SlotMonitorUseCase
	deviceUC    *usecase.DeviceUseCase
	trafficUC   *usecase.TrafficUseCase
	proxyUC     *usecase.ProxyUseCase
	dnsUC       *usecase.DNSUseCase
	configUC    *usecase.ConfigUseCase

	// delivery
	hub         *sse.EventHub
	portHandler *proxy.PortBasedHandler
	routeConfig *route.RouteConfig
}

func Bootstrap(cfg *BootstrapConfig) *BootstrapResult {
	b := &bootstrapper{cfg: cfg, v: cfg.Viper}
	b.initLogging()
	b.initRepositories()
	b.initGateways()
	b.initUseCases()
	b.initDelivery()
	return b.result()
}

func (b *bootstrapper) initLogging() {
	level := parseLevel(b.v.GetString("log.level"))
	b.logRepo = repository.NewLogRepository(nil, b.v.GetInt("log.ring_buffer_size"))
	b.ringHandler = sse.NewRingHandler(b.logRepo, level)

	b.cfg.Logger = NewLoggerWithRing(b.cfg.Viper, b.ringHandler)

	b.deviceLog = b.cfg.Logger.With("component", "device")
	b.slotLog = b.cfg.Logger.With("component", "slot")
	b.monitorLog = b.cfg.Logger.With("component", "slot.monitor")
	b.proxyLog = b.cfg.Logger.With("component", "proxy")
	b.dnsLog = b.cfg.Logger.With("component", "dns")
	b.trafficLog = b.cfg.Logger.With("component", "traffic")
	b.adbLog = b.cfg.Logger.With("component", "adb")
	b.netnsLog = b.cfg.Logger.With("component", "netns")
	b.sseLog = b.cfg.Logger.With("component", "sse")
}

func (b *bootstrapper) initRepositories() {
	b.deviceRepo = repository.NewDeviceRepository(b.deviceLog, b.v.GetInt("devices.max_devices"))
	b.slotRepo = repository.NewSlotRepository(b.slotLog, b.v.GetInt("slots.max_slots"))
	b.trafficRepo = repository.NewTrafficRepository(b.trafficLog, b.v.GetInt("traffic.max_tracked"))
	b.dnsRepo = repository.NewDNSCacheRepository(b.dnsLog, b.v.GetInt("dns.cache_max_entries_per_device"))
}

func (b *bootstrapper) initGateways() {
	b.adbGateway = adb.NewADBGateway(b.adbLog)
	b.adbWatcher = adb.NewADBWatcher(b.adbLog, b.v.GetInt("devices.watcher_reconnect_max_seconds")*1000)
	b.provisioner = netns.NewProvisioner(b.netnsLog)
	b.discovery = netns.NewDiscovery(b.netnsLog, b.v.GetString("slots.ip_check_host"))
	b.resolver = netns.NewCachingResolver(b.dnsLog, b.dnsRepo, netns.CacheConfig{
		MinTTL: time.Duration(b.v.GetInt("dns.cache_min_ttl_seconds")) * time.Second,
		MaxTTL: time.Duration(b.v.GetInt("dns.cache_max_ttl_seconds")) * time.Second,
	})
	b.dialer = netns.NewSetnsDialer(b.netnsLog, b.resolver)
	b.ispProbe = netns.NewISPProbe(b.netnsLog)
}

func (b *bootstrapper) initUseCases() {
	b.slotUC = usecase.NewSlotUseCase(
		b.slotLog, b.slotRepo, b.discovery,
		b.provisioner,
		b.v.GetInt("slots.max_slots_per_device"),
	)

	monitorConfig := usecase.SlotMonitorConfig{
		SteadyInterval:     time.Duration(b.v.GetInt("slots.monitor_steady_interval_seconds")) * time.Second,
		RecoveryInterval:   time.Duration(b.v.GetInt("slots.monitor_recovery_interval_seconds")) * time.Second,
		UnhealthyThreshold: b.v.GetInt("slots.monitor_unhealthy_threshold"),
	}
	b.slotMonitor = usecase.NewSlotMonitorUseCase(b.monitorLog, b.slotRepo, b.discovery, b.provisioner, monitorConfig)
	b.slotUC.SetMonitor(b.slotMonitor)

	gracePeriod := time.Duration(b.v.GetInt("devices.grace_period_seconds")) * time.Second
	drainTimeout := time.Duration(b.v.GetInt("devices.drain_timeout_seconds")) * time.Second
	b.deviceUC = usecase.NewDeviceUseCase(b.deviceLog,
		b.deviceRepo, b.adbGateway, b.provisioner, b.slotRepo, b.slotUC, b.ispProbe,
		b.adbWatcher, gracePeriod, drainTimeout, b.trafficRepo)

	b.trafficUC = usecase.NewTrafficUseCase(b.trafficLog, b.trafficRepo)
	b.proxyUC = usecase.NewProxyUseCase(b.proxyLog, b.slotRepo, b.deviceRepo, b.dialer, b.v.GetString("proxy.source_ip_strategy"), b.trafficRepo)
	b.dnsUC = usecase.NewDNSUseCase(b.dnsLog, b.dnsRepo)

	systemdGW := systemd.NewSystemdGateway(b.cfg.Logger.With("component", "systemd"), "moxy")
	b.configUC = usecase.NewConfigUseCase(b.cfg.Logger.With("component", "config"), "config.json", systemdGW)

	// Hot-reload wiring
	b.configUC.HotReload = &usecase.HotReloader{
		Log:         b.cfg.Logger.With("component", "hot_reload"),
		ProxyUC:     b.proxyUC,
		DeviceUC:    b.deviceUC,
		SlotUC:      b.slotUC,
		SlotMonitor: b.slotMonitor,
		SetIPCheckHost: func(host string) {
			b.discovery.IPCheckHost = host
		},
		SetWatcherBackoff: func(ms int) {
			b.adbWatcher.MaxReconnectMs = ms
		},
		SetDNSCacheTTL: func(min, max time.Duration) {
			b.resolver.SetTTL(min, max)
		},
		SetDNSCacheMaxSize: func(n int) {
			b.dnsRepo.SetMaxEntries(n)
		},
		SetTrafficMax: func(n int) {
			b.trafficRepo.SetMaxEntries(n)
		},
	}
}

func (b *bootstrapper) initDelivery() {
	b.hub = sse.NewEventHub(b.sseLog, b.v.GetInt("sse.max_clients"))
	b.ringHandler.SetHub(b.hub)

	// Wire event publishers
	b.slotUC.EventPub = b.hub
	b.slotMonitor.EventPub = b.hub
	b.deviceUC.EventPub = b.hub
	b.proxyUC.EventPub = b.hub

	// Proxy port handler
	proxyPort := b.v.GetInt("proxy.ipv4.port")
	slotPortStart := b.v.GetInt("proxy.ipv4.slot_port_start")
	ipv6Port := b.v.GetInt("proxy.ipv6.port")
	ipv6SlotPortStart := b.v.GetInt("proxy.ipv6.slot_port_start")
	b.portHandler = proxy.NewPortBasedHandler(b.proxyLog, b.proxyUC, proxyPort, slotPortStart, ipv6Port, ipv6SlotPortStart)

	// SSE traffic snapshot config
	sseTrafficLimit := b.v.GetInt("sse.traffic_snapshot_limit")
	if sseTrafficLimit == 0 {
		sseTrafficLimit = 100
	}
	b.proxyUC.TrafficUC = b.trafficUC
	b.proxyUC.SnapshotLimit = sseTrafficLimit
	b.proxyUC.DNSUC = b.dnsUC

	// Controllers
	deviceCtrl := httpdelivery.NewDeviceController(b.deviceUC, b.deviceLog)
	slotCtrl := httpdelivery.NewSlotController(b.slotUC, b.slotLog)
	dnsCtrl := httpdelivery.NewDNSController(b.dnsUC, b.dnsLog)
	trafficCtrl := httpdelivery.NewTrafficController(b.trafficUC, b.trafficLog)
	configCtrl := httpdelivery.NewConfigController(
		b.cfg.Logger.With("component", "config"),
		b.configUC,
	)

	// SSE handler
	sseSnapshot := func() (*sse.InitPayload, error) {
		devices, err := b.deviceUC.List()
		if err != nil {
			return nil, err
		}
		slots := b.slotUC.ListAll()
		logs := converter.LogEntriesToResponse(b.logRepo.GetRecent())
		traffic := b.trafficUC.ListTop(sseTrafficLimit)
		dnsStats := b.dnsUC.GetCacheStats()
		return &sse.InitPayload{Devices: devices, Slots: slots, Logs: logs, Traffic: traffic, DNSStats: dnsStats}, nil
	}
	sseHandler := sse.NewSSEHandler(b.hub, b.sseLog, sseSnapshot, b.v.GetInt("sse.debounce_ms"), b.v.GetInt("sse.heartbeat_seconds"))

	// Routes
	b.routeConfig = &route.RouteConfig{
		App:               b.cfg.Fiber,
		DeviceController:  deviceCtrl,
		SlotController:    slotCtrl,
		DNSController:     dnsCtrl,
		TrafficController: trafficCtrl,
		ConfigController:  configCtrl,
		SSEHandler:        sseHandler,
		Log:               b.cfg.Logger.With("component", "api"),
		StaticFS:          b.cfg.StaticFS,
	}
}

func (b *bootstrapper) result() *BootstrapResult {
	return &BootstrapResult{
		SlotUseCase:   b.slotUC,
		DeviceUseCase: b.deviceUC,
		SlotMonitor:   b.slotMonitor,
		PortHandler:   b.portHandler,
		RouteConfig:   b.routeConfig,
		EventHub:      b.hub,
		RingHandler:   b.ringHandler,
		ADBGateway:    b.adbGateway,
	}
}
