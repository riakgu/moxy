package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/riakgu/moxy/internal/config"
	"github.com/riakgu/moxy/internal/gateway/netns"
)

func NewTeardownCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "teardown",
		Short: "Destroy all provisioned namespace slots",
		RunE: func(cmd *cobra.Command, args []string) error {
			v := config.NewViper()
			log := config.NewLogger(v)

			provisioner := netns.NewProvisioner(log)

			slots, err := provisioner.ListSlotNamespaces()
			if err != nil {
				return fmt.Errorf("list namespaces: %w", err)
			}

			if len(slots) == 0 {
				log.Info("no slot namespaces found")
				return nil
			}

			successCount := 0
			for _, name := range slots {
				log.Infof("destroying %s", name)
				if err := provisioner.DestroySlot(name); err != nil {
					log.WithError(err).Errorf("failed to destroy %s", name)
					continue
				}
				successCount++
			}

			log.Infof("teardown complete: %d/%d slots destroyed", successCount, len(slots))
			if successCount < len(slots) {
				return fmt.Errorf("%d slots failed to destroy", len(slots)-successCount)
			}
			return nil
		},
	}

	return cmd
}
