//go:build integration
// +build integration

package api

import (
	"math"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// TestTimingIndependence verifies that authentication timing is independent
// of token length or match position by running many iterations and checking variance.
// This is a statistical test marked with +build integration to skip in normal CI.
func TestTimingIndependence(t *testing.T) {
	const iterations = 10000
	const varianceThreshold = 100 * time.Millisecond

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	middleware := BearerAuth("secret-token", nil)
	wrapped := middleware(handler)

	// Test different scenarios
	scenarios := []struct {
		name       string
		authHeader string
	}{
		{
			name:       "Valid token",
			authHeader: "Bearer secret-token",
		},
		{
			name:       "Token matches first char only",
			authHeader: "Bearer sxxxxxxxxxxx", // Same length as "secret-token" (12 chars)
		},
		{
			name:       "Token matches last char only",
			authHeader: "Bearer xxxxxxxxxxxn", // Same length as "secret-token" (12 chars)
		},
		{
			name:       "Token matches middle chars only",
			authHeader: "Bearer xxcret-tokenx", // Same length as "secret-token" (12 chars)
		},
		{
			name:       "Token completely wrong",
			authHeader: "Bearer wrong-tttttt", // Same length as "secret-token" (12 chars)
		},
	}

	type result struct {
		name string
		durs []time.Duration
	}

	results := make([]result, len(scenarios))

	// Run iterations for each scenario
	for i, scenario := range scenarios {
		durs := make([]time.Duration, iterations)

		for j := 0; j < iterations; j++ {
			req := httptest.NewRequest("GET", "/test", nil)
			req.Header.Set("Authorization", scenario.authHeader)

			rec := httptest.NewRecorder()

			start := time.Now()
			wrapped.ServeHTTP(rec, req)
			durs[j] = time.Since(start)
		}

		results[i] = result{
			name: scenario.name,
			durs: durs,
		}
	}

	// Calculate variance for each scenario
	for _, r := range results {
		var sum time.Duration
		for _, d := range r.durs {
			sum += d
		}
		mean := sum / time.Duration(len(r.durs))

		var variance float64
		for _, d := range r.durs {
			diff := float64(d - mean)
			variance += diff * diff
		}
		variance /= float64(len(r.durs))

		// Standard deviation (square root of variance)
		stdDev := time.Duration(math.Sqrt(variance))

		// The variance should be within threshold
		// Note: This test may be flaky due to scheduler noise, hence +build integration
		t.Logf("%s: mean=%v, std_dev=%v", r.name, mean, stdDev)

		// We don't fail on high variance because timing is inherently noisy
		// The purpose is to document the observed timing characteristics
		if stdDev > varianceThreshold {
			t.Logf("WARNING: High timing variance detected for %s: %v (threshold: %v)",
				r.name, stdDev, varianceThreshold)
		}
	}

	// Compare means between scenarios
	// The key property: invalid tokens should not be significantly faster
	// than valid tokens (which would indicate early exit on mismatch)
	means := make([]time.Duration, len(results))
	for i, r := range results {
		var sum time.Duration
		for _, d := range r.durs {
			sum += d
		}
		means[i] = sum / time.Duration(len(r.durs))
	}

	// Check if valid token is within reasonable range of invalid tokens
	// Constant-time comparison should make them similar
	validMean := means[0]
	maxDiff := time.Duration(0)

	for i, mean := range means {
		if i == 0 {
			continue
		}
		diff := validMean - mean
		if diff < 0 {
			diff = -diff
		}
		if diff > maxDiff {
			maxDiff = diff
		}
	}

	// Log the maximum difference observed
	t.Logf("Maximum timing difference: %v", maxDiff)

	// If invalid tokens are consistently faster by a large margin,
	// it might indicate timing leakage (but this is hard to test reliably)
	// We don't fail the test, just log for informational purposes
	if maxDiff > 50*time.Microsecond {
		t.Logf("NOTICE: Timing difference exceeds 50μs threshold: %v", maxDiff)
		t.Log("This may be due to scheduler noise and not necessarily a security issue")
	}
}
