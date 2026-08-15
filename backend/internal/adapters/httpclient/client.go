package httpclient

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/rs/zerolog"
)

type RetryingClient struct {
	client     *http.Client
	userAgent  string
	maxRetries int
	backoff    time.Duration
	logger     *zerolog.Logger
}

func New(client *http.Client, timeout time.Duration, maxRetries int, backoff time.Duration, userAgent string, logger *zerolog.Logger) *RetryingClient {
	if client == nil {
		client = &http.Client{}
	}
	if timeout > 0 {
		client.Timeout = timeout
	}
	if maxRetries < 0 {
		maxRetries = 0
	}
	if backoff <= 0 {
		backoff = time.Second
	}
	if strings.TrimSpace(userAgent) == "" {
		userAgent = "kickertool-ranking/1.0"
	}
	return &RetryingClient{client: client, userAgent: userAgent, maxRetries: maxRetries, backoff: backoff, logger: logger}
}

func (c *RetryingClient) Do(req *http.Request) (*http.Response, error) {
	for attempt := 0; ; attempt++ {
		request := req.Clone(req.Context())
		request.Header.Set("User-Agent", c.userAgent)
		resp, err := c.client.Do(request)
		if !shouldRetry(resp, err) || attempt >= c.maxRetries {
			if err != nil {
				return nil, fmt.Errorf("http request: %w", err)
			}
			return resp, nil
		}
		if resp != nil && resp.Body != nil {
			_ = resp.Body.Close()
		}
		delay := c.backoff * time.Duration(1<<min(attempt, 6))
		if resp != nil && resp.StatusCode == http.StatusTooManyRequests {
			if retryAfter := retryAfterDelay(resp.Header.Get("Retry-After")); retryAfter > delay {
				delay = retryAfter
			}
		}
		if c.logger != nil {
			c.logger.Warn().Int("retry_attempt", attempt+1).Int("max_retries", c.maxRetries).Dur("backoff", delay).Msg("temporary HTTP failure; retrying")
		}
		timer := time.NewTimer(delay)
		select {
		case <-req.Context().Done():
			timer.Stop()
			return nil, req.Context().Err()
		case <-timer.C:
		}
	}
}

func shouldRetry(resp *http.Response, err error) bool {
	if err != nil {
		return !isContextError(err)
	}
	if resp == nil {
		return true
	}
	return resp.StatusCode == http.StatusRequestTimeout || resp.StatusCode == http.StatusTooEarly || resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500
}

func isContextError(err error) bool {
	return err == context.Canceled || err == context.DeadlineExceeded || strings.Contains(err.Error(), context.Canceled.Error()) || strings.Contains(err.Error(), context.DeadlineExceeded.Error())
}

func retryAfterDelay(value string) time.Duration {
	seconds, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || seconds < 0 {
		return 0
	}
	return time.Duration(seconds) * time.Second
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
