package config

import (
	"embed"
	"time"

	"github.com/go-playground/validator/v10"
	"github.com/gofiber/fiber/v2"
	"github.com/sirupsen/logrus"
	"github.com/spf13/viper"

	httpdelivery "github.com/riakgu/moxy/internal/delivery/http"
	"github.com/riakgu/moxy/internal/delivery/http/route"
	"github.com/riakgu/moxy/internal/delivery/proxy"
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
	Socks5Handler    *proxy.Socks5Handler
	HttpProxyHandler *proxy.HttpProxyHandler
	PortHandler      *proxy.PortBasedHandler
	RouteConfig      *route.RouteConfig
}

func Bootstrap(cfg *BootstrapConfig) *BootstrapResult {
	// Gateways
	provisioner := netns.NewProvisioner(cfg.Logger)
	discovery := netns.NewDiscovery(cfg.Logger, cfg.Viper.GetInt("slots.discovery_concurrency"), provisioner, cfg.Viper.GetString("provision.interface"))
	dialer := netns.NewSetnsDialer(cfg.Logger, cfg.Viper.GetString("provision.dns64_server"))

	usersFile := cfg.Viper.GetString("proxy.users_file")
	if usersFile == "" {
		usersFile = "users.json"
	}
	userRepo, err := repository.NewJSONUserRepository(cfg.Logger, usersFile)
	if err != nil {
		cfg.Logger.WithError(err).Fatal("failed to load user repository")
	}

	// UseCases
	slotUC := usecase.NewSlotUseCase(
		cfg.Logger, cfg.Validator, discovery,
		provisioner,
		cfg.Viper.GetString("provision.interface"),
		cfg.Viper.GetString("provision.dns64_server"),
	)
	proxyUC := usecase.NewProxyUseCase(cfg.Logger, slotUC, dialer, userRepo)
	userUC := usecase.NewUserUseCase(cfg.Logger, userRepo)

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
	slotCtrl := httpdelivery.NewSlotController(slotUC, cfg.Logger)
	statsCtrl := httpdelivery.NewStatsController(slotUC, proxyUC, cfg.Logger)
	userCtrl := httpdelivery.NewUserController(userUC, cfg.Logger)

	// Proxy handlers
	socks5Handler := proxy.NewSocks5Handler(cfg.Logger, proxyUC, proxySem, idleTimeout)
	httpProxyHandler := proxy.NewHttpProxyHandler(cfg.Logger, proxyUC, proxySem, idleTimeout)
	portBase := cfg.Viper.GetInt("proxy.port_based_start")
	portHandler := proxy.NewPortBasedHandler(cfg.Logger, proxyUC, proxySem, idleTimeout, portBase)

	// Routes
	routeConfig := &route.RouteConfig{
		App:             cfg.Fiber,
		SlotController:  slotCtrl,
		StatsController: statsCtrl,
		UserController:  userCtrl,
		Log:             cfg.Logger,
		StaticFS:        cfg.StaticFS,
	}

	return &BootstrapResult{
		SlotUseCase:      slotUC,
		Socks5Handler:    socks5Handler,
		HttpProxyHandler: httpProxyHandler,
		PortHandler:      portHandler,
		RouteConfig:      routeConfig,
	}
}
