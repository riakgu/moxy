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

	// Device health ticker — checks ADB connectivity, marks disconnected devices offline.
	// Slot health is handled by per-slot monitor goroutines.
	// Port syncing is handled by SyncSlots calls after provisioning.
	healthInterval := time.Duration(v.GetInt("slots.monitor_steady_interval_seconds")) * time.Second
	if healthInterval == 0 {
		healthInterval = 60 * time.Second
	}
	stopHealth := make(chan struct{})
	go func() {
		ticker := time.NewTicker(healthInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				b.DeviceUseCase.CheckHealth()
				b.PortHandler.SyncSlots(b.SlotUseCase.GetSlotNames())
			case <-stopHealth:
				return
			}
		}
	}()

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
	close(stopHealth)
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
