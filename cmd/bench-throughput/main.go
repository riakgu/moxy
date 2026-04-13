package main

import (
	"crypto/tls"
	"flag"
	"fmt"
	"net/http"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/net/proxy"
)

func main() {
	proxyAddr := flag.String("proxy", "localhost:1080", "SOCKS5 proxy address")
	target := flag.String("url", "http://speedtest.tele2.net/10MB.zip", "URL to download")
	concurrency := flag.Int("c", 1, "Number of concurrent downloads")
	duration := flag.Duration("d", 10*time.Second, "Test duration")
	flag.Parse()

	fmt.Printf("Moxy Throughput Benchmark\n")
	fmt.Printf("══════════════════════════════════════\n")
	fmt.Printf("Proxy:       %s\n", *proxyAddr)
	fmt.Printf("Target:      %s\n", *target)
	fmt.Printf("Concurrency: %d\n", *concurrency)
	fmt.Printf("Duration:    %s\n", *duration)
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
		DisableKeepAlives:   true,
	}
	client := &http.Client{
		Transport: transport,
		Timeout:   30 * time.Second,
	}

	var (
		totalBytes    int64
		totalRequests int64
		totalErrors   int64
		done          = make(chan struct{})
	)

	fmt.Printf("Running...\n\n")
	start := time.Now()

	// Stop after duration
	go func() {
		time.Sleep(*duration)
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
				fmt.Printf("  [%.0fs] %.2f MB/s | %d downloads | %d errors\n", elapsed, mbps, reqs, errs)
			}
		}
	}()

	// Workers
	var wg sync.WaitGroup
	for w := 0; w < *concurrency; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			buf := make([]byte, 32*1024) // 32KB read buffer
			for {
				select {
				case <-done:
					return
				default:
				}

				resp, err := client.Get(*target)
				if err != nil {
					atomic.AddInt64(&totalErrors, 1)
					continue
				}

				if resp.StatusCode >= 400 {
					_ = resp.Body.Close()
					atomic.AddInt64(&totalErrors, 1)
					continue
				}

				// Read in chunks, counting bytes incrementally
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

	// Results
	bytes := atomic.LoadInt64(&totalBytes)
	reqs := atomic.LoadInt64(&totalRequests)
	errs := atomic.LoadInt64(&totalErrors)
	mbps := float64(bytes) / elapsed.Seconds() / 1024 / 1024
	avgSize := float64(0)
	if reqs > 0 {
		avgSize = float64(bytes) / float64(reqs) / 1024 / 1024
	}

	fmt.Printf("\nResults\n")
	fmt.Printf("══════════════════════════════════════\n")
	fmt.Printf("Duration:    %s\n", elapsed.Round(time.Millisecond))
	fmt.Printf("Downloads:   %d\n", reqs)
	fmt.Printf("Errors:      %d\n", errs)
	fmt.Printf("Total data:  %.2f MB\n", float64(bytes)/1024/1024)
	fmt.Printf("Avg file:    %.2f MB\n", avgSize)
	fmt.Printf("\n")
	fmt.Printf("Throughput\n")
	fmt.Printf("──────────────────────────────────────\n")
	fmt.Printf("  Speed:     %.2f MB/s\n", mbps)
	fmt.Printf("  Speed:     %.2f Mbps\n", mbps*8)
	fmt.Printf("\n")
}
