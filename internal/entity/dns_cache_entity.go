package entity

type DNSCacheStats struct {
	Nameserver  string
	NAT64Prefix string
	Entries     int
	Hits        int64
	Misses      int64
}
