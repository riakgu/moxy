//go:build linux

package config

import (
	"database/sql"
	"embed"
	"time"

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
	proxyUserRepo := repository.NewProxyUserRepository(cfg.Logger)

	// Gateways
	adbGateway := adb.NewADBGateway(cfg.Logger)
	provisioner := netns.NewProvisioner(cfg.Logger)
	dns64 := cfg.Viper.GetString("provision.dns64_server")
	discovery := netns.NewDiscovery(cfg.Logger, cfg.Viper.GetInt("slots.discovery_concurrency"), provisioner, "", dns64)
	dialer := netns.NewSetnsDialer(cfg.Logger, dns64)

	// UseCases
	maxSlots := cfg.Viper.GetInt("slots.max_slots_per_device")
	strategy := cfg.Viper.GetString("proxy.source_ip_strategy")
	slotUC := usecase.NewSlotUseCase(
		cfg.Logger, cfg.Validator, discovery,
		provisioner,
		dns64,
		maxSlots,
		strategy,
	)
	deviceUC := usecase.NewDeviceUseCase(cfg.Logger, cfg.Validator, db,
		deviceRepo, adbGateway, provisioner, slotUC, dns64)
	proxyUC := usecase.NewProxyUseCase(cfg.Logger, slotUC, dialer, proxyUserRepo, db)
	proxyUserUC := usecase.NewProxyUserUseCase(cfg.Logger, db, proxyUserRepo)

	// Shared connection semaphore
	maxConns := cfg.Viper.GetInt("proxy.max_connections")
	if maxConns <= 0 {
		maxConns = 500
	}
	proxySem := make(chan struct{}, maxConns)
	cfg.Logger.Infof("proxy connection limit: %d", maxConns)

	// Idle timeout
	idleTimeoutSec := cfg.Viper.GetInt("proxy.idle_timeout_seconds")
	if idleTimeoutSec <= 0 {
		idleTimeoutSec = 300
	}
	idleTimeout := time.Duration(idleTimeoutSec) * time.Second
	cfg.Logger.Infof("proxy idle timeout: %s", idleTimeout)

	// Controllers
	deviceCtrl := httpdelivery.NewDeviceController(deviceUC, slotUC, cfg.Logger)
	slotCtrl := httpdelivery.NewSlotController(slotUC, cfg.Logger)
	statsCtrl := httpdelivery.NewStatsController(slotUC, cfg.Logger)
	proxyUserCtrl := httpdelivery.NewProxyUserController(proxyUserUC, cfg.Logger)

	// Proxy handlers
	socks5Handler := proxy.NewSocks5Handler(cfg.Logger, proxyUC, proxySem, idleTimeout)
	httpProxyHandler := proxy.NewHttpProxyHandler(cfg.Logger, proxyUC, proxySem, idleTimeout)
	portStart := cfg.Viper.GetInt("proxy.port_based_start")
	portEnd := cfg.Viper.GetInt("proxy.port_based_end")
	portHandler := proxy.NewPortBasedHandler(cfg.Logger, proxyUC, proxySem, idleTimeout, portStart, portEnd)

	// Routes
	routeConfig := &route.RouteConfig{
		App:                 cfg.Fiber,
		DeviceController:    deviceCtrl,
		SlotController:      slotCtrl,
		StatsController:     statsCtrl,
		ProxyUserController: proxyUserCtrl,
		Log:                 cfg.Logger,
		StaticFS:            cfg.StaticFS,
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
