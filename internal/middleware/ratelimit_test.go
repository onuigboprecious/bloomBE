package middleware

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestRateLimiter(t *testing.T) {
	// Create rate limiter allowing 2 requests per 100ms with burst of 2
	rl := NewRateLimiter(2, 100*time.Millisecond, 2)
	defer rl.Close()

	handler := rl.LimitFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})

	req1 := httptest.NewRequest("POST", "/api/login", nil)
	req1.RemoteAddr = "192.168.1.100:12345"
	rec1 := httptest.NewRecorder()

	handler(rec1, req1)
	if rec1.Code != http.StatusOK {
		t.Fatalf("expected status 200 on first request, got %d", rec1.Code)
	}

	req2 := httptest.NewRequest("POST", "/api/login", nil)
	req2.RemoteAddr = "192.168.1.100:12345"
	rec2 := httptest.NewRecorder()

	handler(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Fatalf("expected status 200 on second request, got %d", rec2.Code)
	}

	// Third request exceeding burst limit
	req3 := httptest.NewRequest("POST", "/api/login", nil)
	req3.RemoteAddr = "192.168.1.100:12345"
	rec3 := httptest.NewRecorder()

	handler(rec3, req3)
	if rec3.Code != http.StatusTooManyRequests {
		t.Fatalf("expected status 429 on third request, got %d", rec3.Code)
	}

	var resBody map[string]string
	if err := json.Unmarshal(rec3.Body.Bytes(), &resBody); err != nil {
		t.Fatalf("failed to parse json error response: %v", err)
	}
	if resBody["error"] == "" {
		t.Fatalf("expected error message in 429 response")
	}

	// Different IP address should be allowed
	reqDiff := httptest.NewRequest("POST", "/api/login", nil)
	reqDiff.RemoteAddr = "192.168.1.200:12345"
	recDiff := httptest.NewRecorder()

	handler(recDiff, reqDiff)
	if recDiff.Code != http.StatusOK {
		t.Fatalf("expected status 200 for different IP, got %d", recDiff.Code)
	}
}
