package main

import (
	"fmt"
	"io"
	"net/http"
	"time"
)

func main() {
	// The application thinks it is talking to a local service.
	// It has NO idea that the ambassador is actually routing this to httpbin.org
	targetURL := "http://localhost:8080/get"

	fmt.Println("Client App Started: Polling " + targetURL)

	for {
		resp, err := http.Get(targetURL)
		if err != nil {
			fmt.Printf("Error reaching ambassador: %v\n", err)
		} else {
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			fmt.Printf("Success! Status: %s | Body Length: %d bytes\n", resp.Status, len(body))
		}

		// Wait 5 seconds before next request
		time.Sleep(5 * time.Second)
	}
}
