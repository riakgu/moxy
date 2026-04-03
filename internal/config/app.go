//go:build linux

package config

import (
	"context"
	"database/sql"
	"embed"
	"net"

	"github.com/go-playground/validator/v10"
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
	Viper     *viper.Viper
	Logger    *logrus.Logger
	Validator *validator.Validate
	Fiber     *fiber.App
	StaticFS  embed.FS
}

type BootstrapResult struct {
	SlotUseCase      *usecase.SlotUseCase
	DeviceUseCase    *usecase.DeviceUseCase
	Socks5Handler    *proxy.Socks5Handler
	HttpProxyHandler *proxy.HttpProxyHandler
	PortHandler      *proxy.PortBasedHandler
	RouteConfig      *route.RouteConfig
	DB               *sql.DB
}

func Bootstrap(cfg *BootstrapConfig) *BootstrapResult {
	// SQLite database
	db := NewSQLite(cfg.Viper, cfg.Logger)

	// Repositories
	deviceRepo := repository.NewDeviceRepository(cfg.Logger)

	// Gateways
	adbGateway := adb.NewADBGateway(cfg.Logger)
	provisioner := netns.NewProvisioner(cfg.Logger)
	discovery := netns.NewDiscovery(cfg.Logger, cfg.Viper.GetInt("slots.discovery_concurrency"), provisioner, "", "")
	dialer := netns.NewSetnsDialer(cfg.Logger)

	// UseCases
	maxSlots := cfg.Viper.GetInt("slots.max_slots_per_device")
	strategy := cfg.Viper.GetString("proxy.source_ip_strategy")
	slotRepo := repository.NewSlotRepository(cfg.Logger)
	slotUC := usecase.NewSlotUseCase(
		cfg.Logger, cfg.Validator, slotRepo, discovery,
		provisioner,
		maxSlots,
		strategy,
	)
	ispProbe := netns.NewISPProbe(cfg.Logger)
	deviceUC := usecase.NewDeviceUseCase(cfg.Logger, cfg.Validator, db,
		deviceRepo, adbGateway, provisioner, slotUC, ispProbe)
	proxyUC := usecase.NewProxyUseCase(cfg.Logger, slotUC, dialer)

	// Controllers
	deviceCtrl := httpdelivery.NewDeviceController(deviceUC, slotUC, cfg.Logger)
	slotCtrl := httpdelivery.NewSlotController(slotUC, cfg.Logger)

	// Main proxy ConnectFunc — selects slot via strategy, then connects
	mainConnect := proxy.ConnectFunc(func(ctx context.Context, addr string) (net.Conn, error) {
		slotName, err := proxyUC.SelectSlot("")
		if err != nil {
			return nil, err
		}
		return proxyUC.Connect(slotName, addr)
	})

	// Proxy handlers
	socks5Handler := proxy.NewSocks5Handler(cfg.Logger, mainConnect)
	httpProxyHandler := proxy.NewHttpProxyHandler(cfg.Logger, mainConnect)
	socks5PortStart := cfg.Viper.GetInt("proxy.port_based_socks5_start")
	httpPortStart := cfg.Viper.GetInt("proxy.port_based_http_start")
	portHandler := proxy.NewPortBasedHandler(cfg.Logger, proxyUC, socks5PortStart, httpPortStart)

	// Routes
	routeConfig := &route.RouteConfig{
		App:              cfg.Fiber,
		DeviceController: deviceCtrl,
		SlotController:   slotCtrl,
		Log:              cfg.Logger,
		StaticFS:         cfg.StaticFS,
	}

	return &BootstrapResult{
		SlotUseCase:      slotUC,
		DeviceUseCase:    deviceUC,
		Socks5Handler:    socks5Handler,
		HttpProxyHandler: httpProxyHandler,
		PortHandler:      portHandler,
		RouteConfig:      routeConfig,
		DB:               db,
	}
}
