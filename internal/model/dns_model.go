package model

type DeviceCacheStatsResponse struct {
	Nameserver      string  `json:"nameserver"`
	NAT64Prefix     string  `json:"nat64_prefix"`
	Entries         int     `json:"entries"`
	Hits            int64   `json:"hits"`
	Misses          int64   `json:"misses"`
	HitRatePercent  float64 `json:"hit_rate_percent"`
}

type DNSCacheStatsResponse struct {
	Caches              []DeviceCacheStatsResponse `json:"caches"`
	TotalEntries        int                        `json:"total_entries"`
	TotalHits           int64                      `json:"total_hits"`
	TotalMisses         int64                      `json:"total_misses"`
	TotalHitRatePercent float64                    `json:"total_hit_rate_percent"`
}
