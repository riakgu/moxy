//go:build linux

package system

import (
	"bufio"
	"fmt"
	"log/slog"
	"os"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// CPUSample holds a snapshot of /proc/stat CPU times.
type CPUSample struct {
	Total uint64
	Idle  uint64
}

// Stats holds raw system metrics.
type Stats struct {
	CPUPercent     float64
	MemUsedBytes   uint64
	MemTotalBytes  uint64
	Temperature    float64
	LoadAvg1       float64
	LoadAvg5       float64
	LoadAvg15      float64
	DiskUsedBytes  uint64
	DiskTotalBytes uint64
	HostUptime     float64
	Goroutines     int
}

// SystemGateway reads host and process metrics from /proc and /sys.
type SystemGateway struct {
	log      *slog.Logger
	prevCPU  CPUSample
}

func NewSystemGateway(log *slog.Logger) *SystemGateway {
	g := &SystemGateway{log: log}
	// Prime the CPU sample so the first Collect gives a meaningful delta
	g.prevCPU, _ = readCPUSample()
	return g
}

// Collect gathers a full system snapshot.
func (g *SystemGateway) Collect() Stats {
	var s Stats

	// CPU
	cur, err := readCPUSample()
	if err == nil {
		totalDelta := cur.Total - g.prevCPU.Total
		idleDelta := cur.Idle - g.prevCPU.Idle
		if totalDelta > 0 {
			s.CPUPercent = float64(totalDelta-idleDelta) / float64(totalDelta) * 100
		}
		g.prevCPU = cur
	}

	// Memory
	s.MemTotalBytes, s.MemUsedBytes = readMemInfo()

	// Temperature
	s.Temperature = readTemperature()

	// Load average
	s.LoadAvg1, s.LoadAvg5, s.LoadAvg15 = readLoadAvg()

	// Disk
	s.DiskTotalBytes, s.DiskUsedBytes = readDiskUsage("/")

	// Host uptime
	s.HostUptime = readUptime()

	// Goroutines
	s.Goroutines = runtime.NumGoroutine()

	return s
}

func readCPUSample() (CPUSample, error) {
	data, err := os.ReadFile("/proc/stat")
	if err != nil {
		return CPUSample{}, err
	}
	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		if strings.HasPrefix(line, "cpu ") {
			fields := strings.Fields(line)
			if len(fields) < 5 {
				return CPUSample{}, fmt.Errorf("unexpected /proc/stat format")
			}
			var total, idle uint64
			for i := 1; i < len(fields); i++ {
				v, _ := strconv.ParseUint(fields[i], 10, 64)
				total += v
				if i == 4 { // idle is the 4th value (index 4 in fields, after "cpu")
					idle = v
				}
			}
			return CPUSample{Total: total, Idle: idle}, nil
		}
	}
	return CPUSample{}, fmt.Errorf("cpu line not found in /proc/stat")
}

func readMemInfo() (total, used uint64) {
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return 0, 0
	}
	defer f.Close()

	var memTotal, memAvailable uint64
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "MemTotal:") {
			memTotal = parseMemLine(line)
		} else if strings.HasPrefix(line, "MemAvailable:") {
			memAvailable = parseMemLine(line)
		}
	}
	// /proc/meminfo reports in kB
	total = memTotal * 1024
	if memTotal > memAvailable {
		used = (memTotal - memAvailable) * 1024
	}
	return
}

func parseMemLine(line string) uint64 {
	fields := strings.Fields(line)
	if len(fields) < 2 {
		return 0
	}
	v, _ := strconv.ParseUint(fields[1], 10, 64)
	return v
}

func readTemperature() float64 {
	// Try common thermal zones
	paths := []string{
		"/sys/class/thermal/thermal_zone0/temp",
		"/sys/class/thermal/thermal_zone1/temp",
	}
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		v, err := strconv.ParseFloat(strings.TrimSpace(string(data)), 64)
		if err != nil {
			continue
		}
		return v / 1000.0 // millidegrees to degrees
	}
	return 0
}

func readLoadAvg() (avg1, avg5, avg15 float64) {
	data, err := os.ReadFile("/proc/loadavg")
	if err != nil {
		return
	}
	fields := strings.Fields(string(data))
	if len(fields) < 3 {
		return
	}
	avg1, _ = strconv.ParseFloat(fields[0], 64)
	avg5, _ = strconv.ParseFloat(fields[1], 64)
	avg15, _ = strconv.ParseFloat(fields[2], 64)
	return
}

func readDiskUsage(path string) (total, used uint64) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return 0, 0
	}
	total = stat.Blocks * uint64(stat.Bsize)
	free := stat.Bavail * uint64(stat.Bsize)
	used = total - free
	return
}

func readUptime() float64 {
	data, err := os.ReadFile("/proc/uptime")
	if err != nil {
		return 0
	}
	fields := strings.Fields(string(data))
	if len(fields) < 1 {
		return 0
	}
	v, _ := strconv.ParseFloat(fields[0], 64)
	return v
}

// StartTime should be set at process startup for uptime calculation.
var StartTime = time.Now()
