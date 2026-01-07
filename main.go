package main

import (
	"fmt"
	"net/http"
	"os"
	"time"
)

var startTime = time.Now()

func main() {
	// Health check endpoint (for Kubernetes probes)
	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "OK")
	})

	// Main page
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		greeting := os.Getenv("GREETING")
		if greeting == "" {
			greeting = "Hello"
		}
		secretValue := os.Getenv("SECRET_VALUE")
		hostname, _ := os.Hostname()
		uptime := time.Since(startTime).Round(time.Second)

		w.Header().Set("Content-Type", "text/html")
		fmt.Fprintf(w, `<!DOCTYPE html>
<html>
<head>
    <title>Test App - Container Platform</title>
    <style>
        body { font-family: system-ui, sans-serif; max-width: 800px; margin: 50px auto; padding: 20px; }
        h1 { color: #333; }
        .card { background: #f5f5f5; padding: 20px; border-radius: 8px; margin: 20px 0; }
        code { background: #e0e0e0; padding: 2px 6px; border-radius: 4px; }
        .status { color: #22c55e; font-weight: bold; }
    </style>
</head>
<body>
    <h1>%s from Container Platform!</h1>

    <div class="card">
        <h2>App Status</h2>
        <p><strong>Hostname:</strong> <code>%s</code></p>
        <p><strong>Uptime:</strong> <code>%s</code></p>
        <p><strong>Time:</strong> <code>%s</code></p>
    </div>

    <div class="card">
        <h2>Environment Variables</h2>
        <p><strong>GREETING:</strong> <code>%s</code></p>
        <p><strong>SECRET_VALUE set:</strong> <code>%v</code></p>
    </div>

    <div class="card">
        <h2>Features Demo</h2>
        <p>This app demonstrates Container Platform features:</p>
        <ul>
            <li><strong>Background Workers:</strong> Add a worker with command <code>./worker</code></li>
            <li><strong>Scale to Zero:</strong> Enable in settings, app sleeps when idle</li>
            <li><strong>Health Checks:</strong> Configured at <code>/health</code></li>
            <li><strong>Autoscaling:</strong> Scales based on CPU usage</li>
        </ul>
    </div>
</body>
</html>`, greeting, hostname, uptime, time.Now().Format(time.RFC3339), greeting, secretValue != "")
	})

	port := os.Getenv("PORT")
	if port == "" {
		port = "80"
	}
	fmt.Printf("Server starting on port %s\n", port)
	fmt.Printf("Health check available at /health\n")
	http.ListenAndServe(":"+port, nil)
}
