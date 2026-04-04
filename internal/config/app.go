//go:build linux

package config

import (
	"embed"

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
	discovery := netns.NewDiscovery(cfg.Logger, cfg.Viper.GetInt("slots.discovery_concurrency"), cfg.Viper.GetString("slots.ip_check_host"))
	dialer := netns.NewSetnsDialer(cfg.Logger)

	// UseCases
	maxSlots := cfg.Viper.GetInt("slots.max_slots_per_device")
	strategy := cfg.Viper.GetString("proxy.source_ip_strategy")
	if strategy == "" {
		strategy = usecase.StrategyRandom
	}
	slotUC := usecase.NewSlotUseCase(
		cfg.Logger, slotRepo, discovery,
		provisioner,
		maxSlots,
	)
	ispProbe := netns.NewISPProbe(cfg.Logger)
	deviceUC := usecase.NewDeviceUseCase(cfg.Logger,
		deviceRepo, adbGateway, provisioner, slotRepo, slotUC, ispProbe)
	proxyUC := usecase.NewProxyUseCase(cfg.Logger, slotRepo, dialer, strategy)

	// Controllers
	deviceCtrl := httpdelivery.NewDeviceController(deviceUC, cfg.Logger)
	slotCtrl := httpdelivery.NewSlotController(slotUC, cfg.Logger)

	// Port-based handler (shared + device + per-slot mux listeners)
	proxyPort := cfg.Viper.GetInt("proxy.port")
	slotPortStart := cfg.Viper.GetInt("proxy.slot_port_start")
	portHandler := proxy.NewPortBasedHandler(cfg.Logger, proxyUC, proxyPort, slotPortStart)

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
		PortHandler:   portHandler,
		RouteConfig:   routeConfig,
	}
}
