package main

import (
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"os"
	"time"
)

// HEADER PROPAGATION (CRITICAL FOR TRACING)
// In a Mesh, if Service A calls Service B, A must forward specific headers
// so Jaeger can link the two spans together.
var traceHeaders = []string{
	"x-request-id",
	"x-b3-traceid",
	"x-b3-spanid",
	"x-b3-parentspanid",
	"x-b3-sampled",
	"x-b3-flags",
	"x-ot-span-context",
}

func getEnv(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok {
		return value
	}
	return fallback
}

// 1. THE SERVER MODE ("Echo Service")
// It replies "OK", but fails 30% of the time to simulate a flaky network.
func serverHandler(w http.ResponseWriter, r *http.Request) {
	// Simulate Flakiness: Fail 30% of requests with 503
	if rand.Intn(100) < 30 {
		fmt.Println("Server: Simulating failure (503)")
		w.WriteHeader(http.StatusServiceUnavailable)
		w.Write([]byte("Service Flaky Error"))
		return
	}

	fmt.Println("Server: Success (200)")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("Hello from Echo Service!"))
}

// 2. THE CLIENT MODE ("Caller Service")
// It calls the Echo Service and prints the result.
func clientHandler(w http.ResponseWriter, r *http.Request) {
	targetURL := getEnv("TARGET_URL", "http://localhost:8080")

	req, err := http.NewRequest("GET", targetURL, nil)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	// --- TRACING MAGIC ---
	// Forward the trace headers from the incoming request to the outgoing request
	for _, h := range traceHeaders {
		if val := r.Header.Get(h); val != "" {
			req.Header.Set(h, val)
		}
	}

	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Do(req)

	if err != nil {
		fmt.Printf("Client: Call failed: %v\n", err)
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprintf(w, "Call Failed: %v", err)
		return
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	fmt.Printf("Client: Received %s from backend\n", resp.Status)

	// Return the backend's status to our caller
	w.WriteHeader(resp.StatusCode)
	fmt.Fprintf(w, "Backend replied: %s | Body: %s", resp.Status, body)
}

func main() {
	mode := getEnv("MODE", "server")
	port := "8080"

	if mode == "client" {
		http.HandleFunc("/", clientHandler)
		fmt.Printf("Starting CLIENT mode on :%s... calling %s\n", port, getEnv("TARGET_URL", "?"))
	} else {
		rand.Seed(time.Now().UnixNano())
		http.HandleFunc("/", serverHandler)
		fmt.Printf("Starting SERVER mode on :%s... (30%% failure rate)\n", port)
	}

	http.ListenAndServe(":"+port, nil)
}
