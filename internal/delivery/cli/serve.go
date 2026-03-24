package cli

import (
	"embed"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/riakgu/moxy/internal/config"
	httpdelivery "github.com/riakgu/moxy/internal/delivery/http"
	"github.com/riakgu/moxy/internal/delivery/http/route"
	"github.com/riakgu/moxy/internal/delivery/proxy"
	"github.com/riakgu/moxy/internal/gateway/netns"
	"github.com/riakgu/moxy/internal/usecase"
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

			binaryPath, _ := os.Executable()

			// Gateways
			discovery := netns.NewDiscovery(log, v.GetInt("slots.discovery_concurrency"))
			dialer := netns.NewDialer(log, binaryPath)
			provisioner := netns.NewProvisioner(log)

			// UseCases
			slotUC := usecase.NewSlotUseCase(log, validate, discovery)
			proxyUC := usecase.NewProxyUseCase(
				log, slotUC, dialer,
				v.GetString("proxy.username"),
				v.GetString("proxy.password"),
			)

			// Controllers
			slotCtrl := httpdelivery.NewSlotController(slotUC, log)
			statsCtrl := httpdelivery.NewStatsController(slotUC, log)

			// Routes
			routeConfig := &route.RouteConfig{
				App:             app,
				SlotController:  slotCtrl,
				StatsController: statsCtrl,
				Log:             log,
				DashboardFS:     dashboardFS,
			}
			routeConfig.Setup()

			// Initial discovery
			log.Info("running initial slot discovery...")
			slotNames, err := provisioner.ListSlotNamespaces()
			if err != nil {
				log.WithError(err).Warn("initial namespace scan failed")
			} else {
				discovered := discovery.DiscoverAll(slotNames)
				slotUC.UpdateSlots(discovered)
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
						names, err := provisioner.ListSlotNamespaces()
						if err != nil {
							log.WithError(err).Error("discovery scan failed")
							continue
						}
						discovered := discovery.DiscoverAll(names)
						slotUC.UpdateSlots(discovered)
						log.Debugf("discovery tick: %d slots", len(discovered))
					case <-stopDiscovery:
						return
					}
				}
			}()

			// Start proxy listeners
			socks5Addr := fmt.Sprintf(":%d", v.GetInt("proxy.socks5_port"))
			httpProxyAddr := fmt.Sprintf(":%d", v.GetInt("proxy.http_port"))
			apiAddr := fmt.Sprintf(":%d", v.GetInt("api.port"))

			socks5Handler := proxy.NewSocks5Handler(log, proxyUC)
			httpProxyHandler := proxy.NewHttpProxyHandler(log, proxyUC)

			go func() {
				if err := socks5Handler.ListenAndServe(socks5Addr); err != nil {
					log.WithError(err).Fatal("SOCKS5 listener failed")
				}
			}()

			go func() {
				if err := httpProxyHandler.ListenAndServe(httpProxyAddr); err != nil {
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
			if err := app.ShutdownWithTimeout(drainTimeout); err != nil {
				log.WithError(err).Error("API shutdown error")
			}

			log.Info("moxy stopped")
			return nil
		},
	}

	return cmd
}
