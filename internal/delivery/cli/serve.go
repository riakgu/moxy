package cli

import (
	"context"
	"embed"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/riakgu/moxy/internal/config"
)

func NewServeCommand(dashboardFS embed.FS) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Start the proxy server (SOCKS5 + HTTP + dashboard)",
		RunE: func(cmd *cobra.Command, args []string) error {
			v := config.NewViper()
			log := config.NewLogger(v)
			validate := config.NewValidator()
			app := config.NewFiber(v)

			b := config.Bootstrap(&config.BootstrapConfig{
				Viper:     v,
				Logger:    log,
				Validator: validate,
				Fiber:     app,
				StaticFS:  dashboardFS,
			})

			b.RouteConfig.Setup()

			// Initial discovery
			log.Info("running initial slot discovery...")
			slotNames, err := b.Provisioner.ListSlotNamespaces()
			if err != nil {
				log.WithError(err).Warn("initial namespace scan failed")
			} else {
				discovered := b.Discovery.DiscoverAll(slotNames)
				b.SlotUseCase.UpdateSlots(discovered)
				log.Infof("discovered %d slots", len(discovered))
			}

			// Discovery ticker
			discoveryInterval := time.Duration(v.GetInt("slots.discovery_interval_seconds")) * time.Second
			stopDiscovery := make(chan struct{})
			go func() {
				ticker := time.NewTicker(discoveryInterval)
				defer ticker.Stop()
				for {
					select {
					case <-ticker.C:
						names, err := b.Provisioner.ListSlotNamespaces()
						if err != nil {
							log.WithError(err).Error("discovery scan failed")
							continue
						}
						discovered := b.Discovery.DiscoverAll(names)
						b.SlotUseCase.UpdateSlots(discovered)
						log.Debugf("discovery tick: %d slots", len(discovered))
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

			// Stop API/dashboard
			if err := app.ShutdownWithTimeout(drainTimeout); err != nil {
				log.WithError(err).Error("API shutdown error")
			}

			log.Info("moxy stopped")
			return nil
		},
	}

	return cmd
}
