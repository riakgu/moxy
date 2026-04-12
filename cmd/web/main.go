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

	go b.EventHub.Run()

	b.PortHandler.StartShared()
	b.PortHandler.StartSharedIPv6()

	log.Info("cleaning up orphaned namespaces")
	if cleaned, err := b.SlotUseCase.CleanupOrphans(); err != nil {
		log.Warn("namespace cleanup failed", "error", err)
	} else if cleaned > 0 {
		log.Info("orphaned namespaces cleaned", "count", cleaned)
	}

	watchCtx, watchCancel := context.WithCancel(context.Background())
	go b.DeviceUseCase.StartWatching(watchCtx)

	apiAddr := fmt.Sprintf(":%d", v.GetInt("api.port"))
	go func() {
		if err := app.Listen(apiAddr); err != nil {
			log.Error("api listener failed", "error", err)
			os.Exit(1)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Info("shutdown initiated")
	watchCancel()
	b.SlotMonitor.StopAll()
	b.EventHub.Shutdown()

	drainTimeout := time.Duration(v.GetInt("server.shutdown_drain_seconds")) * time.Second
	ctx, cancel := context.WithTimeout(context.Background(), drainTimeout)
	defer cancel()

	log.Info("stopping proxy listeners")
	if err := b.PortHandler.Shutdown(ctx); err != nil {
		log.Warn("proxy shutdown incomplete", "error", err)
	}

	if err := app.ShutdownWithTimeout(drainTimeout); err != nil {
		log.Error("api shutdown failed", "error", err)
	}

	log.Info("moxy stopped")

}
