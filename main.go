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
	"github.com/jackc/pgx/v5"
	"github.com/redis/go-redis/v9"
)

var startTime = time.Now()

func getS3Client() (*s3.S3, error) {
	accessKey := os.Getenv("S3_ACCESS_KEY_ID")
	secretKey := os.Getenv("S3_SECRET_ACCESS_KEY")
	sessionToken := os.Getenv("S3_SESSION_TOKEN")
	if sessionToken == "" {
		sessionToken = os.Getenv("AWS_SESSION_TOKEN")
	}
	endpoint := os.Getenv("S3_ENDPOINT_URL")

	if accessKey == "" || secretKey == "" || endpoint == "" {
		return nil, fmt.Errorf("S3 credentials not configured")
	}

	sess, err := session.NewSession(&aws.Config{
		Credentials:      credentials.NewStaticCredentials(accessKey, secretKey, sessionToken),
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

func getDBConnection(ctx context.Context) (*pgx.Conn, error) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		return nil, fmt.Errorf("DATABASE_URL not configured")
	}

	conn, err := pgx.Connect(ctx, databaseURL)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}

	return conn, nil
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

	// Database Test endpoint - connects and runs queries
	http.HandleFunc("/db-test", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		ctx := context.Background()

		conn, err := getDBConnection(ctx)
		if err != nil {
			fmt.Fprintf(w, "<h1>Database Test Failed</h1><p>Error: %s</p><p><a href=\"/\">Back</a></p>", err)
			return
		}
		defer conn.Close(ctx)

		var results []string

		// 1. Connection test - get PostgreSQL version
		var pgVersion string
		err = conn.QueryRow(ctx, "SELECT version()").Scan(&pgVersion)
		if err != nil {
			results = append(results, fmt.Sprintf("VERSION FAILED: %s", err))
			pgVersion = "unknown"
		} else {
			// Truncate version for display
			if len(pgVersion) > 60 {
				pgVersion = pgVersion[:60] + "..."
			}
			results = append(results, "VERSION SUCCESS: Connected to PostgreSQL")
		}

		// 2. Create test table
		_, err = conn.Exec(ctx, `
			CREATE TABLE IF NOT EXISTS test_deploy_app (
				id SERIAL PRIMARY KEY,
				key TEXT UNIQUE NOT NULL,
				value TEXT,
				created_at TIMESTAMP DEFAULT NOW()
			)
		`)
		if err != nil {
			results = append(results, fmt.Sprintf("CREATE TABLE FAILED: %s", err))
		} else {
			results = append(results, "CREATE TABLE SUCCESS: test_deploy_app")
		}

		// 3. Insert test row
		testKey := fmt.Sprintf("test-key-%d", time.Now().Unix())
		testValue := fmt.Sprintf("Hello from Container Platform! Time: %s", time.Now().Format(time.RFC3339))
		_, err = conn.Exec(ctx, `
			INSERT INTO test_deploy_app (key, value) VALUES ($1, $2)
			ON CONFLICT (key) DO UPDATE SET value = $2
		`, testKey, testValue)
		if err != nil {
			results = append(results, fmt.Sprintf("INSERT FAILED: %s", err))
		} else {
			results = append(results, fmt.Sprintf("INSERT SUCCESS: %s", testKey))
		}

		// 4. Select test row
		var retrievedValue string
		err = conn.QueryRow(ctx, "SELECT value FROM test_deploy_app WHERE key = $1", testKey).Scan(&retrievedValue)
		if err != nil {
			results = append(results, fmt.Sprintf("SELECT FAILED: %s", err))
		} else if retrievedValue == testValue {
			results = append(results, "SELECT SUCCESS: Value verified")
		} else {
			results = append(results, "SELECT FAILED: Value mismatch")
		}

		// 5. Count rows
		var rowCount int
		err = conn.QueryRow(ctx, "SELECT COUNT(*) FROM test_deploy_app").Scan(&rowCount)
		if err != nil {
			results = append(results, fmt.Sprintf("COUNT FAILED: %s", err))
		} else {
			results = append(results, fmt.Sprintf("COUNT SUCCESS: %d rows in table", rowCount))
		}

		// 6. Delete test row (cleanup)
		_, err = conn.Exec(ctx, "DELETE FROM test_deploy_app WHERE key = $1", testKey)
		if err != nil {
			results = append(results, fmt.Sprintf("DELETE FAILED: %s", err))
		} else {
			results = append(results, fmt.Sprintf("DELETE SUCCESS: %s", testKey))
		}

		fmt.Fprintf(w, `<!DOCTYPE html>
<html>
<head>
    <title>Database Test Results</title>
    <style>
        body { font-family: system-ui, sans-serif; max-width: 800px; margin: 50px auto; padding: 20px; }
        .success { color: #22c55e; }
        .failed { color: #ef4444; }
        .card { background: #f5f5f5; padding: 20px; border-radius: 8px; margin: 20px 0; }
        code { background: #e0e0e0; padding: 2px 6px; border-radius: 4px; font-size: 12px; }
    </style>
</head>
<body>
    <h1>Database Test Results</h1>
    <div class="card">
        <p><strong>PostgreSQL Version:</strong></p>
        <p><code>%s</code></p>
    </div>
    <div class="card">
        <h2>Operations</h2>
        <ul>
`, pgVersion)

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

		// Check for Database
		databaseURL := os.Getenv("DATABASE_URL")
		dbConnected := databaseURL != ""
		dbMasked := ""
		if dbConnected {
			// Mask the password in the URL
			parts := strings.Split(databaseURL, "@")
			if len(parts) == 2 {
				dbMasked = "postgres://***@" + parts[1]
			} else {
				dbMasked = "[set]"
			}
		}

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
        .btn-db { background: #7c3aed; }
        .btn-db:hover { background: #6d28d9; }
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
        <h2>Managed Services</h2>
        <p><strong>Database (PostgreSQL):</strong> <span class="status %s">%s</span></p>
        %s
        %s
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
			statusClass(dbConnected), statusText(dbConnected),
			dbDetails(dbConnected, dbMasked),
			dbTestButton(dbConnected),
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
	fmt.Printf("Database test available at /db-test\n")
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

func dbDetails(connected bool, maskedURL string) string {
	if !connected {
		return ""
	}
	return fmt.Sprintf(`<p style="margin-left: 20px;"><code>%s</code></p>`, maskedURL)
}

func dbTestButton(connected bool) string {
	if !connected {
		return ""
	}
	return `<a href="/db-test" class="btn btn-db">Run DB Test</a>`
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
