//go:build linux

package adb

import (
	"fmt"
	"net"
	"os/exec"
	"strings"

	"github.com/sirupsen/logrus"
)

type ADBGateway struct {
	Log *logrus.Logger
}

func NewADBGateway(log *logrus.Logger) *ADBGateway {
	return &ADBGateway{Log: log}
}

// ListDevices returns ADB serial numbers of connected devices
func (g *ADBGateway) ListDevices() ([]string, error) {
	out, err := exec.Command("adb", "devices").Output()
	if err != nil {
		return nil, fmt.Errorf("adb devices: %w", err)
	}

	var serials []string
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "List of") || strings.HasPrefix(line, "*") {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) >= 2 && parts[1] == "device" {
			serials = append(serials, parts[0])
		}
	}
	return serials, nil
}

// adbShell runs a command on the device and returns trimmed output
func (g *ADBGateway) adbShell(serial string, args ...string) (string, error) {
	cmdArgs := append([]string{"-s", serial, "shell"}, args...)
	out, err := exec.Command("adb", cmdArgs...).CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

func (g *ADBGateway) IsScreenUnlocked(serial string) (bool, error) {
	out, err := g.adbShell(serial, "dumpsys", "window", "|", "grep", "mDreamingLockscreen")
	if err != nil {
		return false, fmt.Errorf("check screen: %w", err)
	}
	// If mDreamingLockscreen=false, screen is unlocked
	return strings.Contains(out, "mDreamingLockscreen=false"), nil
}

func (g *ADBGateway) EnableTethering(serial string) error {
	_, err := g.adbShell(serial, "svc", "usb", "setFunctions", "rndis")
	return err
}

func (g *ADBGateway) EnableData(serial string) error {
	_, err := g.adbShell(serial, "svc", "data", "enable")
	return err
}

func (g *ADBGateway) DismissDataDialog(serial string) error {
	_, err := g.adbShell(serial, "input", "keyevent", "BACK")
	return err
}

func (g *ADBGateway) DisableWifi(serial string) error {
	_, err := g.adbShell(serial, "svc", "wifi", "disable")
	return err
}

func (g *ADBGateway) GetCarrier(serial string) (string, error) {
	out, err := g.adbShell(serial, "getprop", "gsm.operator.alpha")
	if err != nil {
		return "", fmt.Errorf("get carrier: %w", err)
	}
	return out, nil
}

// DetectTetheringInterface finds USB tethering interfaces on the host
// by listing network interfaces matching common tethering patterns (usb*, rndis*, enp*)
func (g *ADBGateway) DetectTetheringInterface() ([]string, error) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil, fmt.Errorf("list interfaces: %w", err)
	}

	var tethering []string
	for _, iface := range ifaces {
		name := iface.Name
		if strings.HasPrefix(name, "usb") || strings.HasPrefix(name, "rndis") || strings.HasPrefix(name, "enp") {
			if iface.Flags&net.FlagUp != 0 {
				tethering = append(tethering, name)
			}
		}
	}
	return tethering, nil
}
