package kickertoolhtml

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"kickertool-ranking/internal/domain"
)

func TestFailedEligibilityProbeDoesNotReturnSuccessfulPartialListing(t *testing.T) {
	for _, status := range []int{http.StatusForbidden, http.StatusServiceUnavailable} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/community" {
					_, _ = w.Write([]byte(`<a class="tournament-card" href="/community/tournaments/known/standings"><span class="name">Known</span></a><a class="tournament-card" href="/community/tournaments/probe"><span class="name">Probe</span></a>`))
					return
				}
				http.Error(w, "probe unavailable", status)
			}))
			defer server.Close()
			source, err := NewSource(server.URL+"/community", server.Client(), nil)
			if err != nil {
				t.Fatal(err)
			}
			tournaments, err := source.FetchTournaments(context.Background())
			if err == nil || !strings.Contains(err.Error(), "standings eligibility") || len(tournaments) != 0 {
				t.Fatalf("partial discovery reported success: tournaments=%+v err=%v", tournaments, err)
			}
		})
	}
}

type canceledProbeClient struct{ cancel context.CancelFunc }

func (c canceledProbeClient) Do(request *http.Request) (*http.Response, error) {
	if request.URL.Path == "/community" {
		return &http.Response{StatusCode: 200, Request: request, Body: http.NoBody}, nil
	}
	c.cancel()
	return nil, request.Context().Err()
}

func TestCanceledEligibilityProbePreservesCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	source, err := NewSource("https://example.test/community", canceledProbeClient{cancel: cancel}, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = source.standingsPageAvailable(ctx, domain.Tournament{SourceID: "probe", URL: "https://example.test/community/tournaments/probe"})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("probe error=%v", err)
	}
}
