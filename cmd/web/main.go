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

	// ── Startup ─────────────────────────────────────────────

	b.RouteConfig.Setup()
	go b.EventHub.Run()

	b.PortHandler.StartAll(v.GetInt("devices.max_devices"), v.GetInt("slots.max_slots"))

	if cleaned, err := b.SlotUseCase.CleanupOrphans(); err != nil {
		log.Warn("namespace cleanup failed", "error", err)
	} else if cleaned > 0 {
		log.Info("orphaned namespaces cleaned", "count", cleaned)
	}

	if err := b.ADBGateway.EnsureServer(); err != nil {
		log.Warn("adb server start failed", "error", err)
	}

	go b.DeviceUseCase.StartWatching(context.Background())

	apiAddr := fmt.Sprintf(":%d", v.GetInt("api.port"))
	go func() {
		if err := app.Listen(apiAddr); err != nil {
			log.Error("api listener failed", "error", err)
			os.Exit(1)
		}
	}()

	log.Info("moxy started", "api", apiAddr)

	// ── Wait for shutdown signal ────────────────────────────

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	// ── Graceful shutdown ───────────────────────────────────

	log.Info("shutting down")

	b.SlotMonitor.StopAll()

	drainTimeout := time.Duration(v.GetInt("devices.drain_timeout_seconds")) * time.Second
	devices, _ := b.DeviceUseCase.List()
	for _, d := range devices {
		b.SlotUseCase.TeardownByDevice(d.Alias, drainTimeout)
	}

	b.EventHub.Shutdown()

	shutdownDrain := time.Duration(v.GetInt("server.shutdown_drain_seconds")) * time.Second
	ctx, cancel := context.WithTimeout(context.Background(), shutdownDrain)
	defer cancel()

	if err := b.PortHandler.Shutdown(ctx); err != nil {
		log.Warn("proxy shutdown incomplete", "error", err)
	}
	if err := app.ShutdownWithTimeout(shutdownDrain); err != nil {
		log.Error("api shutdown failed", "error", err)
	}

	log.Info("moxy stopped")
}
