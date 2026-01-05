package main

import (
	"fmt"
	"net/http"
	"os"
	"time"
)

func main() {
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		greeting := os.Getenv("GREETING")
		if greeting == "" {
			greeting = "Hello"
		}
		secretValue := os.Getenv("SECRET_VALUE")

		w.Header().Set("Content-Type", "text/html")
		fmt.Fprintf(w, `<!DOCTYPE html>
<html>
<head><title>Test App</title></head>
<body>
<h1>%s from Container Platform!</h1>
<p>Time: %s</p>
<p>GREETING env var: <code>%s</code></p>
<p>SECRET_VALUE set: <code>%v</code></p>
</body>
</html>`, greeting, time.Now().Format(time.RFC3339), greeting, secretValue != "")
	})

	port := os.Getenv("PORT")
	if port == "" {
		port = "80"
	}
	fmt.Printf("Listening on port %s\n", port)
	http.ListenAndServe(":"+port, nil)
}
