package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/riakgu/moxy/internal/config"
	"github.com/riakgu/moxy/web"
)

func main() {
	v := config.NewViper()
	log := config.NewLogger(v)
	validate := config.NewValidator()
	app := config.NewFiber(v)

	b := config.Bootstrap(&config.BootstrapConfig{
		Viper:     v,
		Logger:    log,
		Validator: validate,
		Fiber:     app,
		StaticFS:  web.StaticFS,
	})

	b.RouteConfig.Setup()

	// Initial discovery
	log.Info("running initial slot discovery...")
	count, err := b.SlotUseCase.DiscoverSlots()
	if err != nil {
		log.WithError(err).Warn("initial discovery failed")
	} else {
		log.Infof("discovered %d slots", count)
	}

	// Sync port-based listeners with discovered slots
	b.PortHandler.SyncSlots(b.SlotUseCase.GetSlotNames())

	// Discovery ticker
	discoveryInterval := time.Duration(v.GetInt("slots.discovery_interval_seconds")) * time.Second
	stopDiscovery := make(chan struct{})
	go func() {
		ticker := time.NewTicker(discoveryInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				count, err := b.SlotUseCase.DiscoverSlots()
				if err != nil {
					log.WithError(err).Error("discovery scan failed")
					continue
				}
				log.Debugf("discovery tick: %d slots", count)
				b.PortHandler.SyncSlots(b.SlotUseCase.GetSlotNames())
			case <-stopDiscovery:
				return
			}
		}
	}()

	// Start listeners
	socks5Addr := fmt.Sprintf(":%d", v.GetInt("proxy.socks5_port"))
	httpProxyAddr := fmt.Sprintf(":%d", v.GetInt("proxy.http_port"))
	apiAddr := fmt.Sprintf(":%d", v.GetInt("api.port"))

	go func() {
		if err := b.Socks5Handler.ListenAndServe(socks5Addr); err != nil {
			log.WithError(err).Fatal("SOCKS5 listener failed")
		}
	}()

	go func() {
		if err := b.HttpProxyHandler.ListenAndServe(httpProxyAddr); err != nil {
			log.WithError(err).Fatal("HTTP proxy listener failed")
		}
	}()

	go func() {
		if err := app.Listen(apiAddr); err != nil {
			log.WithError(err).Fatal("API listener failed")
		}
	}()

	// Graceful shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Info("shutting down...")
	close(stopDiscovery)

	drainTimeout := time.Duration(v.GetInt("server.shutdown_drain_seconds")) * time.Second
	ctx, cancel := context.WithTimeout(context.Background(), drainTimeout)
	defer cancel()

	// Stop accepting new proxy connections
	log.Info("stopping SOCKS5 listener...")
	if err := b.Socks5Handler.Shutdown(ctx); err != nil {
		log.WithError(err).Warn("SOCKS5 shutdown: some connections did not drain in time")
	}

	log.Info("stopping HTTP proxy listener...")
	if err := b.HttpProxyHandler.Shutdown(ctx); err != nil {
		log.WithError(err).Warn("HTTP proxy shutdown: some connections did not drain in time")
	}

	log.Info("stopping port-based listeners...")
	if err := b.PortHandler.Shutdown(ctx); err != nil {
		log.WithError(err).Warn("port-based shutdown: some connections did not drain in time")
	}

	// Stop API/dashboard
	if err := app.ShutdownWithTimeout(drainTimeout); err != nil {
		log.WithError(err).Error("API shutdown error")
	}

	log.Info("moxy stopped")
}
