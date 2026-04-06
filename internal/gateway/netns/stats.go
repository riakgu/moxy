//go:build linux

package netns

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// ReadInterfaceStats reads cumulative TX/RX byte counters from sysfs.
// Returns 0s on error (non-fatal — interface may be down).
func ReadInterfaceStats(iface string) (rxBytes, txBytes uint64, err error) {
	rxBytes, err = readSysfsCounter(iface, "rx_bytes")
	if err != nil {
		return 0, 0, fmt.Errorf("read rx_bytes for %s: %w", iface, err)
	}
	txBytes, err = readSysfsCounter(iface, "tx_bytes")
	if err != nil {
		return 0, 0, fmt.Errorf("read tx_bytes for %s: %w", iface, err)
	}
	return rxBytes, txBytes, nil
}

func readSysfsCounter(iface, counter string) (uint64, error) {
	path := fmt.Sprintf("/sys/class/net/%s/statistics/%s", iface, counter)
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	return strconv.ParseUint(strings.TrimSpace(string(data)), 10, 64)
}
