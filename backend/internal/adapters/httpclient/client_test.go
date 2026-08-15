package httpclient

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestRetry429AndRetryAfter(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			w.Header().Set("Retry-After", "0")
			http.Error(w, "slow down", http.StatusTooManyRequests)
			return
		}
		w.Header().Set("X-UA", r.UserAgent())
		_, _ = io.WriteString(w, "ok")
	}))
	defer server.Close()
	client := New(server.Client(), time.Second, 1, time.Millisecond, "crawler-test", nil)
	req, _ := http.NewRequest(http.MethodGet, server.URL, nil)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if calls != 2 || resp.Header.Get("X-UA") != "crawler-test" {
		t.Fatalf("calls=%d ua=%q", calls, resp.Header.Get("X-UA"))
	}
}

func TestNoRetryPermanentStatus(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++; http.Error(w, "no", http.StatusForbidden) }))
	defer server.Close()
	client := New(server.Client(), time.Second, 3, time.Millisecond, "test", nil)
	req, _ := http.NewRequest(http.MethodGet, server.URL, nil)
	resp, err := client.Do(req)
	if err != nil || resp.StatusCode != http.StatusForbidden || calls != 1 {
		t.Fatalf("err=%v status=%v calls=%d", err, resp.StatusCode, calls)
	}
	resp.Body.Close()
}
