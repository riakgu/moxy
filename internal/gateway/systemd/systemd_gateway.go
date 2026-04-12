//go:build linux

package systemd

import (
	"fmt"
	"log/slog"
	"os/exec"
)

type SystemdGateway struct {
	log         *slog.Logger
	serviceName string
}

func NewSystemdGateway(log *slog.Logger, serviceName string) *SystemdGateway {
	return &SystemdGateway{
		log:         log,
		serviceName: serviceName,
	}
}

func (g *SystemdGateway) Restart() error {
	g.log.Info("restarting service", "service", g.serviceName)
	cmd := exec.Command("systemctl", "restart", g.serviceName)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("systemctl restart %s: %w", g.serviceName, err)
	}
	return nil
}
