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
	app := config.NewFiber(v)

	b := config.Bootstrap(&config.BootstrapConfig{
		Viper:    v,
		Logger:   log,
		Fiber:    app,
		StaticFS: web.StaticFS,
	})

	b.RouteConfig.Setup()

	// Start shared proxy port
	b.PortHandler.StartShared()

	// Cleanup orphaned namespaces from previous runs
	log.Info("cleaning up orphaned namespaces...")
	if cleaned, err := b.SlotUseCase.CleanupOrphans(); err != nil {
		log.WithError(err).Warn("namespace cleanup failed")
	} else if cleaned > 0 {
		log.Infof("cleaned %d orphaned namespaces", cleaned)
	}

	// Event-driven device watcher (replaces old CheckHealth polling)
	watchCtx, watchCancel := context.WithCancel(context.Background())
	go b.DeviceUseCase.StartWatching(watchCtx)

	// Start API listener
	apiAddr := fmt.Sprintf(":%d", v.GetInt("api.port"))
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
	watchCancel()
	b.SlotMonitor.StopAll()

	drainTimeout := time.Duration(v.GetInt("server.shutdown_drain_seconds")) * time.Second
	ctx, cancel := context.WithTimeout(context.Background(), drainTimeout)
	defer cancel()

	log.Info("stopping proxy listeners...")
	if err := b.PortHandler.Shutdown(ctx); err != nil {
		log.WithError(err).Warn("proxy shutdown: some connections did not drain in time")
	}

	// Stop API/dashboard
	if err := app.ShutdownWithTimeout(drainTimeout); err != nil {
		log.WithError(err).Error("API shutdown error")
	}

	log.Info("moxy stopped")
}
