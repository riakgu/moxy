//go:build linux

package adb

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
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

// DetectInterfaceForSerial finds the USB tethering interface that belongs to
// a specific phone by matching the ADB serial against the USB device serial
// exposed in sysfs at /sys/class/net/<iface>/device/../serial.
func (g *ADBGateway) DetectInterfaceForSerial(serial string) (string, error) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return "", fmt.Errorf("list interfaces: %w", err)
	}

	for _, iface := range ifaces {
		name := iface.Name
		if !strings.HasPrefix(name, "usb") && !strings.HasPrefix(name, "rndis") && !strings.HasPrefix(name, "enp") {
			continue
		}
		if iface.Flags&net.FlagUp == 0 {
			continue
		}

		usbSerial, err := g.readUSBSerial(name)
		if err != nil {
			g.Log.Debugf("skip %s: %v", name, err)
			continue
		}
		if usbSerial == serial {
			return name, nil
		}
	}
	return "", fmt.Errorf("no tethering interface found for serial %s", serial)
}

// readUSBSerial resolves the sysfs device path for a network interface
// and reads the USB device serial.
// Chain: /sys/class/net/<iface>/device -> symlink -> USB interface -> parent -> serial file
func (g *ADBGateway) readUSBSerial(ifaceName string) (string, error) {
	devicePath := fmt.Sprintf("/sys/class/net/%s/device", ifaceName)

	// EvalSymlinks resolves all symlinks (like readlink -f)
	resolved, err := filepath.EvalSymlinks(devicePath)
	if err != nil {
		return "", fmt.Errorf("resolve %s: %w", devicePath, err)
	}

	// resolved points to the USB interface (e.g. .../1-2/1-2:1.0)
	// Go up one level to the USB device node (e.g. .../1-2) which has the serial file
	usbDevice := filepath.Dir(resolved)
	serialPath := filepath.Join(usbDevice, "serial")

	data, err := os.ReadFile(serialPath)
	if err != nil {
		return "", fmt.Errorf("read serial from %s: %w", serialPath, err)
	}
	return strings.TrimSpace(string(data)), nil
}

