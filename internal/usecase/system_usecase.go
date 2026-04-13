//go:build linux

package usecase

import (
	"log/slog"
	"os"
	"runtime"
	"time"

	systemgw "github.com/riakgu/moxy/internal/gateway/system"
	"github.com/riakgu/moxy/internal/model"
)

type SystemUseCase struct {
	Log     *slog.Logger
	Gateway *systemgw.SystemGateway
}

func NewSystemUseCase(log *slog.Logger, gw *systemgw.SystemGateway) *SystemUseCase {
	return &SystemUseCase{Log: log, Gateway: gw}
}

func (uc *SystemUseCase) Collect() *model.SystemStatsResponse {
	s := uc.Gateway.Collect()

	hostname, _ := os.Hostname()
	processUptime := int64(time.Since(systemgw.StartTime).Seconds())

	return &model.SystemStatsResponse{
		CPUPercent:     s.CPUPercent,
		MemUsedBytes:   s.MemUsedBytes,
		MemTotalBytes:  s.MemTotalBytes,
		Temperature:    s.Temperature,
		LoadAvg1:       s.LoadAvg1,
		LoadAvg5:       s.LoadAvg5,
		LoadAvg15:      s.LoadAvg15,
		DiskUsedBytes:  s.DiskUsedBytes,
		DiskTotalBytes: s.DiskTotalBytes,
		HostUptime:     int64(s.HostUptime),
		ProcessUptime:  processUptime,
		Goroutines:     s.Goroutines,
		GoVersion:      runtime.Version(),
		Hostname:       hostname,
		Arch:           runtime.GOARCH,
	}
}
