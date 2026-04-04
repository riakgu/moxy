package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/riakgu/moxy/internal/config"
	"github.com/riakgu/moxy/internal/model"
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

	// Auto-scan: discover ADB devices, setup, provision 1 slot each
	log.Info("running initial device scan...")
	scanResult, err := b.DeviceUseCase.Scan()
	if err != nil {
		log.WithError(err).Warn("initial scan failed")
	} else {
		log.Infof("scan complete: %d discovered, %d ok, %d failed",
			scanResult.Discovered, scanResult.SetupOk, scanResult.Failed)
	}

	// Discover slots (picks up just-provisioned + any pre-existing namespaces)
	log.Info("running initial slot discovery...")
	count, err := b.SlotUseCase.DiscoverSlots()
	if err != nil {
		log.WithError(err).Warn("initial discovery failed")
	} else {
		log.Infof("discovered %d slots", count)
	}

	// Sync device + slot listeners
	b.PortHandler.SyncDevices(deviceAliases(scanResult))
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
				b.DeviceUseCase.CheckHealth()
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
	close(stopDiscovery)

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

func deviceAliases(scan *model.ScanResponse) []string {
	if scan == nil {
		return nil
	}
	aliases := make([]string, 0, len(scan.Devices))
	for _, d := range scan.Devices {
		aliases = append(aliases, d.Alias)
	}
	return aliases
}
