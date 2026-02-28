package proxy

import (
	"context"
	"io"
	"log"
	"net/http"
	"time"
)

// hopByHopHeaders are headers that should not be forwarded to upstream.
// These are removed per RFC 2616 Section 13.5.1
var hopByHopHeaders = []string{
	"Connection",
	"Keep-Alive",
	"Proxy-Authenticate",
	"Proxy-Authorization",
	"Te",
	"Trailer",
	"Transfer-Encoding",
	"Upgrade",
}

// ProxyHandler creates a handler that forwards requests to the upstream API.
// DL-003: Proxy injects Bearer token when forwarding to API
// DL-013: Returns 502 on upstream failure, 504 on timeout
func ProxyHandler(apiURL, bearerToken string, client *http.Client, logger *log.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Skip proxying for health endpoint (handled directly)
			if r.URL.Path == "/health" {
				next.ServeHTTP(w, r)
				return
			}

			start := time.Now()
			// Create upstream request
			upstreamURL := apiURL + r.URL.Path
			if r.URL.RawQuery != "" {
				upstreamURL += "?" + r.URL.RawQuery
			}

			upstreamReq, err := http.NewRequestWithContext(r.Context(), r.Method, upstreamURL, r.Body)
			if err != nil {
				logger.Printf("ERROR: failed to create upstream request: %v", err)
				writeProxyError(w, http.StatusInternalServerError, "Failed to create upstream request")
				return
			}

			// Copy headers from original request
			for key, values := range r.Header {
				// DL-003: Strip incoming Authorization header, inject Bearer token
				if key == "Authorization" {
					continue
				}
				// Skip hop-by-hop headers
				skipHeader := false
				for _, hopHeader := range hopByHopHeaders {
					if http.CanonicalHeaderKey(key) == hopHeader {
						skipHeader = true
						break
					}
				}
				if !skipHeader {
					for _, value := range values {
						upstreamReq.Header.Add(key, value)
					}
				}
			}

			// DL-003: Inject Bearer token for API authentication
			upstreamReq.Header.Set("Authorization", "Bearer "+bearerToken)

			// Forward request to upstream
			resp, err := client.Do(upstreamReq)
			if err != nil {
				if ctxErr := r.Context().Err(); ctxErr == context.DeadlineExceeded {
					// DL-013: Timeout returns 504 Gateway Timeout
					logger.Printf("ERROR: upstream timeout: %v", err)
					writeProxyError(w, http.StatusGatewayTimeout, "Upstream timeout")
					return
				}
				// DL-013: Connection error returns 502 Bad Gateway
				logger.Printf("ERROR: upstream connection failed: %v", err)
				writeProxyError(w, http.StatusBadGateway, "Upstream connection failed")
				return
			}
			defer resp.Body.Close()

			// Copy response headers
			for key, values := range resp.Header {
				for _, value := range values {
					w.Header().Add(key, value)
				}
			}

			// Copy response status and body
			w.WriteHeader(resp.StatusCode)
			if _, copyErr := io.Copy(w, resp.Body); copyErr != nil {
				logger.Printf("ERROR: response body copy failed: %v", copyErr)
			}

			logger.Printf("INFO: %s %s -> %d (%v)", r.Method, r.URL.Path, resp.StatusCode, time.Since(start))
		})
	}
}
