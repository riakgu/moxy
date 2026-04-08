package usecase

import (
	"log/slog"

	"github.com/riakgu/moxy/internal/model"
	"github.com/riakgu/moxy/internal/model/converter"
	"github.com/riakgu/moxy/internal/repository"
)

// TrafficUseCase provides traffic stats operations.
type TrafficUseCase struct {
	Log  *slog.Logger
	Repo *repository.TrafficRepository
}

// NewTrafficUseCase creates a new TrafficUseCase.
func NewTrafficUseCase(log *slog.Logger, repo *repository.TrafficRepository) *TrafficUseCase {
	return &TrafficUseCase{
		Log:  log,
		Repo: repo,
	}
}

// List returns all traffic entries sorted by connection count descending,
// including summary totals computed from all entries.
func (uc *TrafficUseCase) List() *model.TrafficListResponse {
	return uc.buildResponse(-1)
}

// ListTop returns traffic entries capped to the top N by connection count.
// Summary totals are computed from ALL entries (not just top N).
// If n <= 0, returns all entries.
func (uc *TrafficUseCase) ListTop(n int) *model.TrafficListResponse {
	return uc.buildResponse(n)
}

// buildResponse builds the response, optionally capping entries to top N.
// Summary totals always reflect all entries.
func (uc *TrafficUseCase) buildResponse(limit int) *model.TrafficListResponse {
	entries := uc.Repo.List() // already sorted by ConnectionCount desc

	// Compute summaries from ALL entries
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

	// Cap entries if limit is set
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
