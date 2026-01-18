package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/redis/go-redis/v9"
)

var startTime = time.Now()

func getS3Client() (*s3.S3, error) {
	accessKey := os.Getenv("S3_ACCESS_KEY_ID")
	secretKey := os.Getenv("S3_SECRET_ACCESS_KEY")
	endpoint := os.Getenv("S3_ENDPOINT_URL")

	if accessKey == "" || secretKey == "" || endpoint == "" {
		return nil, fmt.Errorf("S3 credentials not configured")
	}

	sess, err := session.NewSession(&aws.Config{
		Credentials:      credentials.NewStaticCredentials(accessKey, secretKey, ""),
		Endpoint:         aws.String(endpoint),
		Region:           aws.String("auto"),
		S3ForcePathStyle: aws.Bool(true),
	})
	if err != nil {
		return nil, err
	}
	return s3.New(sess), nil
}

func getRedisClient() (*redis.Client, error) {
	redisURL := os.Getenv("REDIS_URL")
	if redisURL == "" {
		return nil, fmt.Errorf("REDIS_URL not configured")
	}

	opts, err := redis.ParseURL(redisURL)
	if err != nil {
		return nil, fmt.Errorf("failed to parse REDIS_URL: %w", err)
	}

	return redis.NewClient(opts), nil
}

func main() {
	// Health check endpoint
	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "OK")
	})

	// Redis Test endpoint
	http.HandleFunc("/redis-test", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		ctx := context.Background()

		client, err := getRedisClient()
		if err != nil {
			fmt.Fprintf(w, "<h1>Redis Test Failed</h1><p>Error: %s</p><p><a href=\"/\">Back</a></p>", err)
			return
		}
		defer client.Close()

		var results []string

		// 1. PING test
		pong, err := client.Ping(ctx).Result()
		if err != nil {
			results = append(results, fmt.Sprintf("PING FAILED: %s", err))
		} else {
			results = append(results, fmt.Sprintf("PING SUCCESS: %s", pong))
		}

		// 2. SET test
		testKey := fmt.Sprintf("test-key-%d", time.Now().Unix())
		testValue := fmt.Sprintf("Hello from Container Platform! Time: %s", time.Now().Format(time.RFC3339))
		err = client.Set(ctx, testKey, testValue, 60*time.Second).Err()
		if err != nil {
			results = append(results, fmt.Sprintf("SET FAILED: %s", err))
		} else {
			results = append(results, fmt.Sprintf("SET SUCCESS: %s", testKey))
		}

		// 3. GET test
		val, err := client.Get(ctx, testKey).Result()
		if err != nil {
			results = append(results, fmt.Sprintf("GET FAILED: %s", err))
		} else if val == testValue {
			results = append(results, "GET SUCCESS: Value verified")
		} else {
			results = append(results, "GET FAILED: Value mismatch")
		}

		// 4. INCR test
		counterKey := "visit-counter"
		count, err := client.Incr(ctx, counterKey).Result()
		if err != nil {
			results = append(results, fmt.Sprintf("INCR FAILED: %s", err))
		} else {
			results = append(results, fmt.Sprintf("INCR SUCCESS: Counter = %d", count))
		}

		// 5. DELETE test key (cleanup)
		err = client.Del(ctx, testKey).Err()
		if err != nil {
			results = append(results, fmt.Sprintf("DEL FAILED: %s", err))
		} else {
			results = append(results, fmt.Sprintf("DEL SUCCESS: %s", testKey))
		}

		// Get Redis info
		info, _ := client.Info(ctx, "server").Result()
		redisVersion := "unknown"
		for _, line := range strings.Split(info, "\n") {
			if strings.HasPrefix(line, "redis_version:") {
				redisVersion = strings.TrimPrefix(line, "redis_version:")
				redisVersion = strings.TrimSpace(redisVersion)
				break
			}
		}

		fmt.Fprintf(w, `<!DOCTYPE html>
<html>
<head>
    <title>Redis Test Results</title>
    <style>
        body { font-family: system-ui, sans-serif; max-width: 800px; margin: 50px auto; padding: 20px; }
        .success { color: #22c55e; }
        .failed { color: #ef4444; }
        .card { background: #f5f5f5; padding: 20px; border-radius: 8px; margin: 20px 0; }
        code { background: #e0e0e0; padding: 2px 6px; border-radius: 4px; }
    </style>
</head>
<body>
    <h1>Redis Test Results</h1>
    <div class="card">
        <p><strong>Redis Version:</strong> <code>%s</code></p>
        <p><strong>Visit Counter:</strong> <code>%d</code></p>
    </div>
    <div class="card">
        <h2>Operations</h2>
        <ul>
`, redisVersion, count)

		for _, result := range results {
			class := "success"
			if strings.Contains(result, "FAILED") {
				class = "failed"
			}
			fmt.Fprintf(w, `            <li class="%s">%s</li>`+"\n", class, result)
		}

		fmt.Fprintf(w, `        </ul>
    </div>
    <p><a href="/">Back to main page</a></p>
</body>
</html>`)
	})

	// S3 Test endpoint - uploads, downloads, and lists objects
	http.HandleFunc("/s3-test", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")

		client, err := getS3Client()
		if err != nil {
			fmt.Fprintf(w, "<h1>S3 Test Failed</h1><p>Error: %s</p>", err)
			return
		}

		bucket := os.Getenv("S3_BUCKET_NAME")
		testKey := fmt.Sprintf("test-%d.txt", time.Now().Unix())
		testContent := fmt.Sprintf("Hello from Container Platform! Time: %s", time.Now().Format(time.RFC3339))

		var results []string

		// 1. Upload test object
		_, err = client.PutObject(&s3.PutObjectInput{
			Bucket: aws.String(bucket),
			Key:    aws.String(testKey),
			Body:   bytes.NewReader([]byte(testContent)),
		})
		if err != nil {
			results = append(results, fmt.Sprintf("Upload FAILED: %s", err))
		} else {
			results = append(results, fmt.Sprintf("Upload SUCCESS: %s", testKey))
		}

		// 2. Download and verify
		getResp, err := client.GetObject(&s3.GetObjectInput{
			Bucket: aws.String(bucket),
			Key:    aws.String(testKey),
		})
		if err != nil {
			results = append(results, fmt.Sprintf("Download FAILED: %s", err))
		} else {
			body, _ := io.ReadAll(getResp.Body)
			getResp.Body.Close()
			if string(body) == testContent {
				results = append(results, "Download SUCCESS: Content verified")
			} else {
				results = append(results, "Download FAILED: Content mismatch")
			}
		}

		// 3. List objects
		listResp, err := client.ListObjectsV2(&s3.ListObjectsV2Input{
			Bucket:  aws.String(bucket),
			MaxKeys: aws.Int64(10),
		})
		if err != nil {
			results = append(results, fmt.Sprintf("List FAILED: %s", err))
		} else {
			var keys []string
			for _, obj := range listResp.Contents {
				keys = append(keys, *obj.Key)
			}
			results = append(results, fmt.Sprintf("List SUCCESS: %d objects [%s]", len(keys), strings.Join(keys, ", ")))
		}

		// 4. Delete test object (cleanup)
		_, err = client.DeleteObject(&s3.DeleteObjectInput{
			Bucket: aws.String(bucket),
			Key:    aws.String(testKey),
		})
		if err != nil {
			results = append(results, fmt.Sprintf("Delete FAILED: %s", err))
		} else {
			results = append(results, fmt.Sprintf("Delete SUCCESS: %s", testKey))
		}

		fmt.Fprintf(w, `<!DOCTYPE html>
<html>
<head>
    <title>S3 Test Results</title>
    <style>
        body { font-family: system-ui, sans-serif; max-width: 800px; margin: 50px auto; padding: 20px; }
        .success { color: #22c55e; }
        .failed { color: #ef4444; }
        .card { background: #f5f5f5; padding: 20px; border-radius: 8px; margin: 20px 0; }
        code { background: #e0e0e0; padding: 2px 6px; border-radius: 4px; }
    </style>
</head>
<body>
    <h1>S3 Bucket Test Results</h1>
    <div class="card">
        <p><strong>Bucket:</strong> <code>%s</code></p>
        <p><strong>Endpoint:</strong> <code>%s</code></p>
    </div>
    <div class="card">
        <h2>Operations</h2>
        <ul>
`, bucket, os.Getenv("S3_ENDPOINT_URL"))

		for _, result := range results {
			class := "success"
			if strings.Contains(result, "FAILED") {
				class = "failed"
			}
			fmt.Fprintf(w, `            <li class="%s">%s</li>`+"\n", class, result)
		}

		fmt.Fprintf(w, `        </ul>
    </div>
    <p><a href="/">Back to main page</a></p>
</body>
</html>`)
	})

	// Environment Variables endpoint - shows all env vars for debugging injection
	http.HandleFunc("/env", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")

		// Categorize environment variables
		platformVars := []struct{ key, value string }{}
		s3Vars := []struct{ key, value string }{}
		redisVars := []struct{ key, value string }{}
		dbVars := []struct{ key, value string }{}
		userVars := []struct{ key, value string }{}
		systemVars := []struct{ key, value string }{}

		for _, env := range os.Environ() {
			parts := strings.SplitN(env, "=", 2)
			if len(parts) != 2 {
				continue
			}
			key, value := parts[0], parts[1]

			// Mask sensitive values
			displayValue := value
			if strings.Contains(strings.ToLower(key), "secret") ||
				strings.Contains(strings.ToLower(key), "password") ||
				strings.Contains(strings.ToLower(key), "key") ||
				strings.Contains(strings.ToLower(key), "token") {
				if len(value) > 8 {
					displayValue = value[:4] + "****" + value[len(value)-4:]
				} else if len(value) > 0 {
					displayValue = "****"
				}
			}

			entry := struct{ key, value string }{key, displayValue}

			// Categorize
			switch {
			case strings.HasPrefix(key, "S3_"):
				s3Vars = append(s3Vars, entry)
			case strings.HasPrefix(key, "REDIS_"):
				redisVars = append(redisVars, entry)
			case strings.HasPrefix(key, "DATABASE_") || strings.HasPrefix(key, "DB_") || strings.HasPrefix(key, "POSTGRES_"):
				dbVars = append(dbVars, entry)
			case key == "PORT" || key == "GREETING" || key == "SECRET_VALUE" || key == "APP_NAME":
				userVars = append(userVars, entry)
			case key == "KUBERNETES_SERVICE_HOST" || key == "KUBERNETES_PORT" ||
				strings.HasPrefix(key, "HOSTNAME") || key == "HOME" || key == "PATH":
				systemVars = append(systemVars, entry)
			default:
				if strings.HasPrefix(key, "CP_") || strings.HasPrefix(key, "PLATFORM_") {
					platformVars = append(platformVars, entry)
				} else {
					systemVars = append(systemVars, entry)
				}
			}
		}

		fmt.Fprintf(w, `<!DOCTYPE html>
<html>
<head>
    <title>Environment Variables - Test App</title>
    <style>
        body { font-family: system-ui, sans-serif; max-width: 1000px; margin: 50px auto; padding: 20px; }
        h1 { color: #333; }
        h2 { color: #666; border-bottom: 1px solid #ddd; padding-bottom: 8px; }
        .card { background: #f5f5f5; padding: 20px; border-radius: 8px; margin: 20px 0; }
        table { width: 100%%; border-collapse: collapse; }
        th, td { text-align: left; padding: 8px 12px; border-bottom: 1px solid #e0e0e0; }
        th { background: #e8e8e8; font-weight: 600; }
        code { background: #e0e0e0; padding: 2px 6px; border-radius: 4px; font-size: 0.9em; }
        .empty { color: #999; font-style: italic; }
        .injected { background: #dcfce7; }
        a { color: #3b82f6; }
    </style>
</head>
<body>
    <h1>Environment Variables</h1>
    <p>This page shows all environment variables to help debug injection behavior.</p>
    <p><a href="/">Back to main page</a></p>
`)

		renderSection := func(title string, vars []struct{ key, value string }, highlight bool) {
			fmt.Fprintf(w, `    <div class="card">
        <h2>%s</h2>
`, title)
			if len(vars) == 0 {
				fmt.Fprintf(w, `        <p class="empty">No variables in this category</p>
`)
			} else {
				fmt.Fprintf(w, `        <table>
            <tr><th>Variable</th><th>Value</th></tr>
`)
				for _, v := range vars {
					rowClass := ""
					if highlight {
						rowClass = ` class="injected"`
					}
					fmt.Fprintf(w, `            <tr%s><td><code>%s</code></td><td><code>%s</code></td></tr>
`, rowClass, v.key, v.value)
				}
				fmt.Fprintf(w, `        </table>
`)
			}
			fmt.Fprintf(w, `    </div>
`)
		}

		renderSection("S3 Storage Variables (Injected)", s3Vars, true)
		renderSection("Redis Variables (Injected)", redisVars, true)
		renderSection("Database Variables (Injected)", dbVars, true)
		renderSection("Platform Variables", platformVars, false)
		renderSection("User-Defined Variables", userVars, false)
		renderSection("System Variables", systemVars, false)

		fmt.Fprintf(w, `</body>
</html>`)
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

		// Check for Redis
		redisURL := os.Getenv("REDIS_URL")
		redisConnected := redisURL != ""
		redisMasked := ""
		if redisConnected {
			parts := strings.Split(redisURL, "@")
			if len(parts) == 2 {
				redisMasked = "rediss://***@" + parts[1]
			} else {
				redisMasked = "[set]"
			}
		}

		// Check for S3 Storage
		s3AccessKey := os.Getenv("S3_ACCESS_KEY_ID")
		s3Endpoint := os.Getenv("S3_ENDPOINT_URL")
		s3Bucket := os.Getenv("S3_BUCKET_NAME")
		storageConnected := s3AccessKey != "" && s3Endpoint != ""

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
        .status { font-weight: bold; }
        .connected { color: #22c55e; }
        .disconnected { color: #999; }
        .btn { display: inline-block; background: #3b82f6; color: white; padding: 8px 16px; border-radius: 4px; text-decoration: none; margin-top: 10px; margin-right: 10px; }
        .btn:hover { background: #2563eb; }
        .btn-redis { background: #dc2626; }
        .btn-redis:hover { background: #b91c1c; }
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
        <h2>Debug</h2>
        <a href="/env" class="btn">View All Environment Variables</a>
    </div>

    <div class="card">
        <h2>Managed Services</h2>
        <p><strong>Redis:</strong> <span class="status %s">%s</span></p>
        %s
        %s
        <p><strong>Object Storage (S3):</strong> <span class="status %s">%s</span></p>
        %s
        %s
    </div>
</body>
</html>`,
			greeting, hostname, uptime, time.Now().Format(time.RFC3339), greeting, secretValue != "",
			statusClass(redisConnected), statusText(redisConnected),
			redisDetails(redisConnected, redisMasked),
			redisTestButton(redisConnected),
			statusClass(storageConnected), statusText(storageConnected),
			storageDetails(storageConnected, s3Endpoint, s3Bucket),
			s3TestButton(storageConnected),
		)
	})

	port := os.Getenv("PORT")
	if port == "" {
		port = "80"
	}
	fmt.Printf("Server starting on port %s\n", port)
	fmt.Printf("Health check available at /health\n")
	fmt.Printf("Environment variables at /env\n")
	fmt.Printf("Redis test available at /redis-test\n")
	fmt.Printf("S3 test available at /s3-test\n")
	http.ListenAndServe(":"+port, nil)
}

func statusClass(connected bool) string {
	if connected {
		return "connected"
	}
	return "disconnected"
}

func statusText(connected bool) string {
	if connected {
		return "Connected"
	}
	return "Not configured"
}

func redisDetails(connected bool, maskedURL string) string {
	if !connected {
		return ""
	}
	return fmt.Sprintf(`<p style="margin-left: 20px;"><code>%s</code></p>`, maskedURL)
}

func redisTestButton(connected bool) string {
	if !connected {
		return ""
	}
	return `<a href="/redis-test" class="btn btn-redis">Run Redis Test</a>`
}

func storageDetails(connected bool, endpoint, bucket string) string {
	if !connected {
		return ""
	}
	return fmt.Sprintf(`<p style="margin-left: 20px;">Endpoint: <code>%s</code></p>
        <p style="margin-left: 20px;">Bucket: <code>%s</code></p>`, endpoint, bucket)
}

func s3TestButton(connected bool) string {
	if !connected {
		return ""
	}
	return `<a href="/s3-test" class="btn">Run S3 Test</a>`
}
