package main

import (
	"crypto/tls"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/net/proxy"
)

func main() {
	proxyAddr := flag.String("proxy", "localhost:1080", "SOCKS5 proxy address")
	target := flag.String("url", "https://www.google.com/generate_204", "Target URL to request")
	concurrency := flag.Int("c", 10, "Number of concurrent workers")
	requests := flag.Int("n", 100, "Total number of requests")
	timeout := flag.Duration("timeout", 15*time.Second, "Per-request timeout")
	flag.Parse()

	fmt.Printf("Moxy Benchmark\n")
	fmt.Printf("══════════════════════════════════════\n")
	fmt.Printf("Proxy:       %s\n", *proxyAddr)
	fmt.Printf("Target:      %s\n", *target)
	fmt.Printf("Concurrency: %d\n", *concurrency)
	fmt.Printf("Requests:    %d\n", *requests)
	fmt.Printf("Timeout:     %s\n", *timeout)
	fmt.Printf("══════════════════════════════════════\n\n")

	// Setup SOCKS5 dialer
	dialer, err := proxy.SOCKS5("tcp", *proxyAddr, nil, proxy.Direct)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create SOCKS5 dialer: %v\n", err)
		os.Exit(1)
	}

	transport := &http.Transport{
		DialContext:         dialer.(proxy.ContextDialer).DialContext,
		TLSClientConfig:    &tls.Config{InsecureSkipVerify: false},
		MaxIdleConnsPerHost: *concurrency,
		MaxConnsPerHost:     *concurrency,
		DisableKeepAlives:   true, // Each request = fresh connection through proxy
	}
	client := &http.Client{
		Transport: transport,
		Timeout:   *timeout,
	}

	// Results
	var (
		successCount int64
		errorCount   int64
		latencies    []time.Duration
		latMu        sync.Mutex
		errors       []string
		errMu        sync.Mutex
	)

	// Work channel
	work := make(chan int, *requests)
	for i := 0; i < *requests; i++ {
		work <- i
	}
	close(work)

	fmt.Printf("Running...\n\n")
	start := time.Now()

	// Workers
	var wg sync.WaitGroup
	for w := 0; w < *concurrency; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range work {
				reqStart := time.Now()
				resp, err := client.Get(*target)
				elapsed := time.Since(reqStart)

				if err != nil {
					atomic.AddInt64(&errorCount, 1)
					errMu.Lock()
					if len(errors) < 10 { // keep first 10 errors
						errors = append(errors, err.Error())
					}
					errMu.Unlock()
					continue
				}
				_, _ = io.Copy(io.Discard, resp.Body)
				_ = resp.Body.Close()

				if resp.StatusCode >= 400 {
					atomic.AddInt64(&errorCount, 1)
					errMu.Lock()
					if len(errors) < 10 {
						errors = append(errors, fmt.Sprintf("HTTP %d", resp.StatusCode))
					}
					errMu.Unlock()
					continue
				}

				atomic.AddInt64(&successCount, 1)
				latMu.Lock()
				latencies = append(latencies, elapsed)
				latMu.Unlock()
			}
		}()
	}

	wg.Wait()
	totalDuration := time.Since(start)

	// Calculate percentiles
	sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })

	total := successCount + errorCount
	rps := float64(total) / totalDuration.Seconds()

	fmt.Printf("Results\n")
	fmt.Printf("══════════════════════════════════════\n")
	fmt.Printf("Total:       %d requests in %s\n", total, totalDuration.Round(time.Millisecond))
	fmt.Printf("Success:     %d (%.1f%%)\n", successCount, float64(successCount)/float64(total)*100)
	fmt.Printf("Errors:      %d (%.1f%%)\n", errorCount, float64(errorCount)/float64(total)*100)
	fmt.Printf("RPS:         %.1f req/s\n", rps)
	fmt.Printf("\n")

	if len(latencies) > 0 {
		fmt.Printf("Latency\n")
		fmt.Printf("──────────────────────────────────────\n")
		fmt.Printf("  Min:       %s\n", latencies[0].Round(time.Millisecond))
		fmt.Printf("  p50:       %s\n", percentile(latencies, 0.50).Round(time.Millisecond))
		fmt.Printf("  p90:       %s\n", percentile(latencies, 0.90).Round(time.Millisecond))
		fmt.Printf("  p95:       %s\n", percentile(latencies, 0.95).Round(time.Millisecond))
		fmt.Printf("  p99:       %s\n", percentile(latencies, 0.99).Round(time.Millisecond))
		fmt.Printf("  Max:       %s\n", latencies[len(latencies)-1].Round(time.Millisecond))
	}

	if len(errors) > 0 {
		fmt.Printf("\nFirst errors:\n")
		fmt.Printf("──────────────────────────────────────\n")
		for i, e := range errors {
			fmt.Printf("  %d. %s\n", i+1, truncate(e, 80))
		}
	}

	fmt.Printf("\n")
}

func percentile(sorted []time.Duration, p float64) time.Duration {
	if len(sorted) == 0 {
		return 0
	}
	idx := int(float64(len(sorted)-1) * p)
	return sorted[idx]
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-3] + "..."
}
