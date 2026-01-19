package proxy

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"
)

const (
	defaultUpstreamTimeout = 10 * time.Second
)

var hopByHopHeaders = map[string]bool{
	"Connection":          true,
	"Keep-Alive":          true,
	"Proxy-Authenticate":  true,
	"Proxy-Authorization": true,
	"Te":                  true,
	"Trailers":            true,
	"Transfer-Encoding":   true,
	"Upgrade":             true,
}

// ProxyHandler forwards requests to the bot API with Bearer token from session
// Extracts session from context, decrypts token on-demand, forwards request to upstream
// upstreamTimeout: timeout for upstream API requests (0 uses default 10s)
func ProxyHandler(botAPIURL string, store *SessionStore, upstreamTimeout time.Duration) http.Handler {
	if upstreamTimeout == 0 {
		upstreamTimeout = defaultUpstreamTimeout
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		session, ok := GetSession(r)
		if !ok {
			http.Error(w, `{"error":"Unauthorized"}`, http.StatusUnauthorized)
			return
		}

		// Decrypt token on-demand to minimize time in memory
		token, err := store.GetToken(session.ID)
		if err != nil {
			log.Printf("Failed to get token for session %s: %v", session.ID, err)
			http.Error(w, `{"error":"Unauthorized"}`, http.StatusUnauthorized)
			return
		}

		upstreamResp, err := forwardRequest(r, botAPIURL, token, upstreamTimeout)
		if err != nil {
			log.Printf("Proxy error: %v", err)
			http.Error(w, `{"error":"Service unavailable","details":"Failed to reach upstream API"}`, http.StatusServiceUnavailable)
			return
		}
		defer upstreamResp.Body.Close()

		copyHeaders(w.Header(), upstreamResp.Header)
		w.WriteHeader(upstreamResp.StatusCode)

		if _, err := io.Copy(w, upstreamResp.Body); err != nil {
			log.Printf("Error copying response body: %v", err)
		}
	})
}

// forwardRequest creates upstream request with Bearer token and forwards it
// Copies headers, body, query parameters from original request
// timeout: timeout for upstream request (configurable via PROXY_UPSTREAM_TIMEOUT env var)
func forwardRequest(req *http.Request, botAPIURL string, bearerToken string, timeout time.Duration) (*http.Response, error) {
	ctx, cancel := context.WithTimeout(req.Context(), timeout)
	defer cancel()

	upstreamURL := botAPIURL + req.URL.Path
	if req.URL.RawQuery != "" {
		upstreamURL += "?" + req.URL.RawQuery
	}

	var bodyReader io.Reader
	if req.Body != nil {
		bodyBytes, err := io.ReadAll(req.Body)
		if err != nil {
			return nil, fmt.Errorf("failed to read request body: %w", err)
		}
		bodyReader = bytes.NewReader(bodyBytes)
	}

	upstreamReq, err := http.NewRequestWithContext(ctx, req.Method, upstreamURL, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("failed to create upstream request: %w", err)
	}

	copyHeaders(upstreamReq.Header, req.Header)
	upstreamReq.Header.Set("Authorization", "Bearer "+bearerToken)

	upstreamReq.Header.Del("Host")

	client := &http.Client{}
	return client.Do(upstreamReq)
}

// copyHeaders copies all headers from src to dst except hop-by-hop headers
func copyHeaders(dst, src http.Header) {
	for key, values := range src {
		if !hopByHopHeaders[key] && !strings.HasPrefix(key, "X-") {
			dst.Del(key)
			for _, value := range values {
				dst.Add(key, value)
			}
		}
	}
}
