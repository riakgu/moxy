//go:build linux

package adb

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"log/slog"
)

type ADBGateway struct {
	Log *slog.Logger
}

func NewADBGateway(log *slog.Logger) *ADBGateway {
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

// GetDeviceInfo reads device model, brand, and Android version from system properties.
func (g *ADBGateway) GetDeviceInfo(serial string) (model, brand, androidVersion string) {
	model, _ = g.adbShell(serial, "getprop", "ro.product.model")
	brand, _ = g.adbShell(serial, "getprop", "ro.product.brand")
	androidVersion, _ = g.adbShell(serial, "getprop", "ro.build.version.release")
	return
}

func (g *ADBGateway) GetCarrier(serial string) (string, error) {
	// 1. Get data subscription ID
	subId, err := g.adbShell(serial, "settings", "get", "global", "multi_sim_data_call")
	if err == nil && subId != "" && subId != "null" {
		// 2. Parse dumpsys isub to find carrier name for that subscription
		out, err := g.adbShell(serial, "dumpsys", "isub")
		if err == nil {
			target := fmt.Sprintf("id=%s ", subId)
			for _, line := range strings.Split(out, "\n") {
				if !strings.Contains(line, target) {
					continue
				}
				idx := strings.Index(line, "carrierName=")
				if idx == -1 {
					continue
				}
				rest := line[idx+len("carrierName="):]
				// carrierName value ends at next " key=" pattern
				endIdx := strings.Index(rest, " isOpportunistic=")
				if endIdx == -1 {
					endIdx = len(rest)
				}
				name := strings.TrimSpace(rest[:endIdx])
				if name != "" {
					return name, nil
				}
			}
		}
	}

	// Fallback: first non-empty from gsm.sim.operator.alpha
	out, err := g.adbShell(serial, "getprop", "gsm.sim.operator.alpha")
	if err != nil {
		return "", fmt.Errorf("get carrier: %w", err)
	}
	for _, part := range strings.Split(out, ",") {
		name := strings.TrimSpace(part)
		if name != "" {
			return name, nil
		}
	}
	return "", fmt.Errorf("no carrier found for %s", serial)
}

// GetDNSServers reads the phone's carrier-assigned DNS servers from
// `dumpsys connectivity`. Returns IPv6 DNS addresses from the internet
// connection (not IMS). These are typically the carrier's DNS64 servers.
func (g *ADBGateway) GetDNSServers(serial string) ([]string, error) {
	out, err := g.adbShell(serial, "dumpsys", "connectivity")
	if err != nil {
		return nil, fmt.Errorf("dumpsys connectivity: %w", err)
	}

	var ipv6Servers []string
	for _, line := range strings.Split(out, "\n") {
		// Only look at the internet connection (skip IMS, etc.)
		if !strings.Contains(line, "extra: internet") {
			continue
		}

		// Extract DnsAddresses: [ /addr1,/addr2,... ]
		dnsIdx := strings.Index(line, "DnsAddresses: [")
		if dnsIdx == -1 {
			continue
		}
		start := dnsIdx + len("DnsAddresses: [")
		end := strings.Index(line[start:], "]")
		if end == -1 {
			continue
		}
		dnsBlock := strings.TrimSpace(line[start : start+end])
		if dnsBlock == "" {
			continue
		}

		for _, entry := range strings.Split(dnsBlock, ",") {
			addr := strings.TrimSpace(entry)
			addr = strings.TrimPrefix(addr, "/")
			if addr == "" {
				continue
			}
			// Keep only IPv6 addresses
			ip := net.ParseIP(addr)
			if ip != nil && ip.To4() == nil {
				ipv6Servers = append(ipv6Servers, ip.String())
			}
		}
		break // only need the internet connection
	}
	return ipv6Servers, nil
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
			g.Log.Debug("skip interface", "interface", name, "error", err)
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

