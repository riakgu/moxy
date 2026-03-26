package cli

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/riakgu/moxy/internal/config"
	"github.com/riakgu/moxy/internal/gateway/netns"
)

func NewProvisionCommand() *cobra.Command {
	var (
		iface string
		slots int
		dns64 string
	)

	cmd := &cobra.Command{
		Use:   "provision",
		Short: "Create IPVLAN/namespace slots for proxy use",
		RunE: func(cmd *cobra.Command, args []string) error {
			v := config.NewViper()
			log := config.NewLogger(v)

			if iface == "" {
				iface = v.GetString("provision.interface")
			}
			if dns64 == "" {
				dns64 = v.GetString("provision.dns64_server")
			}

			provisioner := netns.NewProvisioner(log)
			discovery := netns.NewDiscovery(log, 20, provisioner, iface)

			log.Infof("enabling NDP proxy on %s", iface)
			if err := provisioner.EnableNDPProxy(iface); err != nil {
				return fmt.Errorf("NDP proxy setup failed: %w", err)
			}

			successCount := 0
			failCount := 0

			for i := 0; i < slots; i++ {
				log.Infof("provisioning slot%d (%d/%d)", i, i+1, slots)
				if err := provisioner.CreateSlot(i, iface, dns64); err != nil {
					log.WithError(err).Errorf("failed to provision slot%d", i)
					failCount++
					continue
				}
				successCount++
			}

			log.Infof("provisioning complete: %d created, %d failed", successCount, failCount)

			// Wait for SLAAC to assign IPv6 addresses
			log.Info("waiting 5s for SLAAC IPv6 assignment...")
			time.Sleep(5 * time.Second)

			// Verify: resolve public IPv4 for each created slot
			log.Info("verifying slot IPs...")
			slotNames, err := provisioner.ListSlotNamespaces()
			if err != nil {
				return fmt.Errorf("list namespaces for verification: %w", err)
			}

			discovered := discovery.DiscoverAll(slotNames)
			ipMap := make(map[string][]string) // IPv4 -> slot names
			verifyFail := 0
			for _, s := range discovered {
				if !s.Healthy || s.IPv4Address == "" {
					log.Warnf("slot %s: no public IPv4 resolved", s.Name)
					verifyFail++
					continue
				}
				ipMap[s.IPv4Address] = append(ipMap[s.IPv4Address], s.Name)
				log.Infof("slot %s: %s", s.Name, s.IPv4Address)
			}

			// Report duplicates
			dupCount := 0
			for ip, names := range ipMap {
				if len(names) > 1 {
					dupCount += len(names) - 1
					log.Warnf("duplicate IP %s shared by: %v", ip, names)
				}
			}

			uniqueIPs := len(ipMap)
			log.Infof("verification: %d slots verified, %d failed, %d unique IPs, %d duplicates",
				len(discovered)-verifyFail, verifyFail, uniqueIPs, dupCount)

			if failCount > 0 {
				return fmt.Errorf("%d slots failed to provision", failCount)
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&iface, "interface", "usb0", "Network interface for IPVLAN")
	cmd.Flags().IntVar(&slots, "slots", 20, "Number of slots to create")
	cmd.Flags().StringVar(&dns64, "dns64", "2001:4860:4860::6464", "DNS64 server address")

	return cmd
}
