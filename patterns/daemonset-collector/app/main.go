package main

import (
	"fmt"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// 1. Define a custom metric (Counter)
// We use 'promauto' to automatically register it with the default registry.
var (
	opsProcessed = promauto.NewCounter(prometheus.CounterOpts{
		Name: "myapp_processed_ops_total",
		Help: "The total number of processed operations",
	})
)

// 2. Simulate traffic/work in the background
// In a real app, this would be your API handler logic.
func recordMetrics() {
	go func() {
		for {
			opsProcessed.Inc() // Increment the counter
			time.Sleep(2 * time.Second)
		}
	}()
}

func main() {
	// Start the background simulation
	recordMetrics()

	// 3. Expose the registered metrics via HTTP
	// The 'promhttp.Handler()' function gives us the standard scrape page
	http.Handle("/metrics", promhttp.Handler())

	fmt.Println("Starting server...")
	fmt.Println("Serving metrics on :2112/metrics")

	// Start the web server on port 2112
	// 2112 is a common convention for instrumentation ports to avoid collision with 80/8080
	err := http.ListenAndServe(":2112", nil)
	if err != nil {
		fmt.Printf("Error starting server: %s\n", err)
	}
}
