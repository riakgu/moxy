package usecase

import (
	"log/slog"

	"github.com/riakgu/moxy/internal/model"
	"github.com/riakgu/moxy/internal/model/converter"
	"github.com/riakgu/moxy/internal/repository"
)

type TrafficUseCase struct {
	Log  *slog.Logger
	Repo *repository.TrafficRepository
}

func NewTrafficUseCase(log *slog.Logger, repo *repository.TrafficRepository) *TrafficUseCase {
	return &TrafficUseCase{
		Log:  log,
		Repo: repo,
	}
}

func (uc *TrafficUseCase) List() *model.TrafficListResponse {
	return uc.buildResponse(-1)
}

func (uc *TrafficUseCase) ListTop(n int) *model.TrafficListResponse {
	return uc.buildResponse(n)
}

func (uc *TrafficUseCase) buildResponse(limit int) *model.TrafficListResponse {
	entries := uc.Repo.List()

	var totalConn, totalActive int64
	var totalTx, totalRx uint64
	deviceTotals := make(map[string]model.DeviceTrafficTotal)
	responses := make([]model.TrafficEntryResponse, 0, len(entries))
	for _, e := range entries {
		totalConn += e.ConnectionCount
		totalActive += e.ActiveConnections
		totalTx += e.TxBytes
		totalRx += e.RxBytes

		dt := deviceTotals[e.DeviceAlias]
		dt.TxBytes += e.TxBytes
		dt.RxBytes += e.RxBytes
		deviceTotals[e.DeviceAlias] = dt

		responses = append(responses, converter.TrafficEntryToResponse(e))
	}

	if limit > 0 && len(responses) > limit {
		responses = responses[:limit]
	}

	return &model.TrafficListResponse{
		Entries:          responses,
		TotalEntries:     len(entries),
		TotalConnections: totalConn,
		TotalActive:      totalActive,
		TotalTxBytes:     totalTx,
		TotalRxBytes:     totalRx,
		DeviceTotals:     deviceTotals,
	}
}
