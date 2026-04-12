package usecase

import (
	"log/slog"

	"github.com/riakgu/moxy/internal/model"
)

// DNSStatsProvider abstracts DNS cache statistics retrieval.
type DNSStatsProvider interface {
	Stats() []model.DNSCacheStats
}

// DNSUseCase provides DNS cache operations.
type DNSUseCase struct {
	Log      *slog.Logger
	Resolver DNSStatsProvider
}

// NewDNSUseCase creates a new DNSUseCase.
func NewDNSUseCase(log *slog.Logger, resolver DNSStatsProvider) *DNSUseCase {
	return &DNSUseCase{
		Log:      log,
		Resolver: resolver,
	}
}

// GetCacheStats returns DNS cache statistics as a response model.
func (uc *DNSUseCase) GetCacheStats() *model.DNSCacheStatsResponse {
	rawStats := uc.Resolver.Stats()

	caches := make([]model.DeviceCacheStatsResponse, 0, len(rawStats))
	var totalEntries int
	var totalHits, totalMisses int64

	for _, s := range rawStats {
		hitRate := 0.0
		total := s.Hits + s.Misses
		if total > 0 {
			hitRate = float64(s.Hits) / float64(total) * 100.0
		}

		caches = append(caches, model.DeviceCacheStatsResponse{
			Nameserver:     s.Nameserver,
			NAT64Prefix:    s.NAT64Prefix,
			Entries:        s.Entries,
			Hits:           s.Hits,
			Misses:         s.Misses,
			HitRatePercent: hitRate,
		})

		totalEntries += s.Entries
		totalHits += s.Hits
		totalMisses += s.Misses
	}

	totalHitRate := 0.0
	if totalHits+totalMisses > 0 {
		totalHitRate = float64(totalHits) / float64(totalHits+totalMisses) * 100.0
	}

	return &model.DNSCacheStatsResponse{
		Caches:              caches,
		TotalEntries:        totalEntries,
		TotalHits:           totalHits,
		TotalMisses:         totalMisses,
		TotalHitRatePercent: totalHitRate,
	}
}
