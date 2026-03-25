package config

import (
	"embed"
	"os"

	"github.com/go-playground/validator/v10"
	"github.com/gofiber/fiber/v2"
	"github.com/sirupsen/logrus"
	"github.com/spf13/viper"

	httpdelivery "github.com/riakgu/moxy/internal/delivery/http"
	"github.com/riakgu/moxy/internal/delivery/http/route"
	"github.com/riakgu/moxy/internal/delivery/proxy"
	"github.com/riakgu/moxy/internal/gateway/netns"
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
	ProxyUseCase     *usecase.ProxyUseCase
	Socks5Handler    *proxy.Socks5Handler
	HttpProxyHandler *proxy.HttpProxyHandler
	Discovery        *netns.Discovery
	Provisioner      *netns.Provisioner
	RouteConfig      *route.RouteConfig
}

func Bootstrap(cfg *BootstrapConfig) *BootstrapResult {
	binaryPath, _ := os.Executable()

	// Gateways
	discovery := netns.NewDiscovery(cfg.Logger, cfg.Viper.GetInt("slots.discovery_concurrency"))
	dialer := netns.NewDialer(cfg.Logger, binaryPath)
	provisioner := netns.NewProvisioner(cfg.Logger)

	// UseCases
	slotUC := usecase.NewSlotUseCase(
		cfg.Logger, cfg.Validator, discovery,
		provisioner,
		cfg.Viper.GetString("provision.interface"),
		cfg.Viper.GetString("provision.dns64_server"),
	)
	proxyUC := usecase.NewProxyUseCase(
		cfg.Logger, slotUC, dialer,
		cfg.Viper.GetString("proxy.username"),
		cfg.Viper.GetString("proxy.password"),
	)

	// Shared connection semaphore
	maxConns := cfg.Viper.GetInt("proxy.max_connections")
	if maxConns <= 0 {
		maxConns = 500
	}
	proxySem := make(chan struct{}, maxConns)
	cfg.Logger.Infof("proxy connection limit: %d", maxConns)

	// Controllers
	slotCtrl := httpdelivery.NewSlotController(slotUC, cfg.Logger)
	statsCtrl := httpdelivery.NewStatsController(slotUC, cfg.Logger)

	// Proxy handlers
	socks5Handler := proxy.NewSocks5Handler(cfg.Logger, proxyUC, proxySem)
	httpProxyHandler := proxy.NewHttpProxyHandler(cfg.Logger, proxyUC, proxySem)

	// Routes
	routeConfig := &route.RouteConfig{
		App:             cfg.Fiber,
		SlotController:  slotCtrl,
		StatsController: statsCtrl,
		Log:             cfg.Logger,
		StaticFS:        cfg.StaticFS,
	}

	return &BootstrapResult{
		SlotUseCase:      slotUC,
		ProxyUseCase:     proxyUC,
		Socks5Handler:    socks5Handler,
		HttpProxyHandler: httpProxyHandler,
		Discovery:        discovery,
		Provisioner:      provisioner,
		RouteConfig:      routeConfig,
	}
}
