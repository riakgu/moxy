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
	mode := flag.String("mode", "rps", "Benchmark mode: rps or throughput")
	proxyAddr := flag.String("proxy", "localhost:1080", "SOCKS5 proxy address")
	target := flag.String("url", "", "Target URL (default: auto per mode)")
	concurrency := flag.Int("c", 10, "Number of concurrent workers")
	requests := flag.Int("n", 100, "Total requests (rps mode)")
	timeout := flag.Duration("timeout", 15*time.Second, "Per-request timeout (rps mode)")
	duration := flag.Duration("d", 15*time.Second, "Test duration (throughput mode)")
	flag.Parse()

	// Auto-select URL per mode if not specified
	if *target == "" {
		switch *mode {
		case "throughput":
			*target = "http://speedtest.tele2.net/10MB.zip"
		default:
			*target = "https://www.google.com/generate_204"
		}
	}

	// Setup SOCKS5 client
	client := createClient(*proxyAddr, *concurrency, *timeout)

	switch *mode {
	case "throughput":
		runThroughput(client, *proxyAddr, *target, *concurrency, *duration)
	default:
		runRPS(client, *proxyAddr, *target, *concurrency, *requests, *timeout)
	}
}

func createClient(proxyAddr string, concurrency int, timeout time.Duration) *http.Client {
	dialer, err := proxy.SOCKS5("tcp", proxyAddr, nil, proxy.Direct)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create SOCKS5 dialer: %v\n", err)
		os.Exit(1)
	}

	transport := &http.Transport{
		DialContext:         dialer.(proxy.ContextDialer).DialContext,
		TLSClientConfig:    &tls.Config{InsecureSkipVerify: false},
		MaxIdleConnsPerHost: concurrency,
		MaxConnsPerHost:     concurrency,
		DisableKeepAlives:   true,
	}
	return &http.Client{
		Transport: transport,
		Timeout:   timeout,
	}
}

// ── RPS Mode ─────────────────────────────────────────────────────────────────

func runRPS(client *http.Client, proxyAddr, target string, concurrency, requests int, timeout time.Duration) {
	fmt.Printf("Moxy Benchmark — RPS\n")
	fmt.Printf("══════════════════════════════════════\n")
	fmt.Printf("Proxy:       %s\n", proxyAddr)
	fmt.Printf("Target:      %s\n", target)
	fmt.Printf("Concurrency: %d\n", concurrency)
	fmt.Printf("Requests:    %d\n", requests)
	fmt.Printf("Timeout:     %s\n", timeout)
	fmt.Printf("══════════════════════════════════════\n\n")

	var (
		successCount int64
		errorCount   int64
		latencies    []time.Duration
		latMu        sync.Mutex
		errors       []string
		errMu        sync.Mutex
	)

	work := make(chan int, requests)
	for i := 0; i < requests; i++ {
		work <- i
	}
	close(work)

	fmt.Printf("Running...\n\n")
	start := time.Now()

	var wg sync.WaitGroup
	for w := 0; w < concurrency; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range work {
				reqStart := time.Now()
				resp, err := client.Get(target)
				elapsed := time.Since(reqStart)

				if err != nil {
					atomic.AddInt64(&errorCount, 1)
					errMu.Lock()
					if len(errors) < 10 {
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

// ── Throughput Mode ──────────────────────────────────────────────────────────

func runThroughput(client *http.Client, proxyAddr, target string, concurrency int, duration time.Duration) {
	client.Timeout = 30 * time.Second

	fmt.Printf("Moxy Benchmark — Throughput\n")
	fmt.Printf("══════════════════════════════════════\n")
	fmt.Printf("Proxy:       %s\n", proxyAddr)
	fmt.Printf("Target:      %s\n", target)
	fmt.Printf("Concurrency: %d\n", concurrency)
	fmt.Printf("Duration:    %s\n", duration)
	fmt.Printf("══════════════════════════════════════\n\n")

	var (
		totalBytes    int64
		totalRequests int64
		totalErrors   int64
		done          = make(chan struct{})
	)

	fmt.Printf("Running...\n\n")
	start := time.Now()

	go func() {
		time.Sleep(duration)
		close(done)
	}()

	// Progress ticker
	go func() {
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				elapsed := time.Since(start).Seconds()
				bytes := atomic.LoadInt64(&totalBytes)
				reqs := atomic.LoadInt64(&totalRequests)
				errs := atomic.LoadInt64(&totalErrors)
				mbps := float64(bytes) / elapsed / 1024 / 1024
				fmt.Printf("  [%.0fs] %.2f MB/s (%.1f Mbps) | %d downloads | %d errors\n", elapsed, mbps, mbps*8, reqs, errs)
			}
		}
	}()

	var wg sync.WaitGroup
	for w := 0; w < concurrency; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			buf := make([]byte, 32*1024)
			for {
				select {
				case <-done:
					return
				default:
				}

				resp, err := client.Get(target)
				if err != nil {
					atomic.AddInt64(&totalErrors, 1)
					continue
				}

				if resp.StatusCode >= 400 {
					_ = resp.Body.Close()
					atomic.AddInt64(&totalErrors, 1)
					continue
				}

				for {
					n, err := resp.Body.Read(buf)
					if n > 0 {
						atomic.AddInt64(&totalBytes, int64(n))
					}
					if err != nil {
						break
					}
				}
				_ = resp.Body.Close()
				atomic.AddInt64(&totalRequests, 1)
			}
		}()
	}

	wg.Wait()
	elapsed := time.Since(start)

	bytes := atomic.LoadInt64(&totalBytes)
	reqs := atomic.LoadInt64(&totalRequests)
	errs := atomic.LoadInt64(&totalErrors)
	mbps := float64(bytes) / elapsed.Seconds() / 1024 / 1024

	fmt.Printf("\nResults\n")
	fmt.Printf("══════════════════════════════════════\n")
	fmt.Printf("Duration:    %s\n", elapsed.Round(time.Millisecond))
	fmt.Printf("Downloads:   %d\n", reqs)
	fmt.Printf("Errors:      %d\n", errs)
	fmt.Printf("Total data:  %.2f MB\n", float64(bytes)/1024/1024)
	fmt.Printf("\n")
	fmt.Printf("Throughput\n")
	fmt.Printf("──────────────────────────────────────\n")
	fmt.Printf("  Speed:     %.2f MB/s\n", mbps)
	fmt.Printf("  Speed:     %.2f Mbps\n", mbps*8)
	fmt.Printf("\n")
}

// ── Helpers ──────────────────────────────────────────────────────────────────

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
