package helpers

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

const (
	// scrapeTimeout caps how long RequireScrapeContains waits for a metric to show up.
	scrapeTimeout = time.Second * 15
	scrapeTick    = time.Millisecond * 50
)

// Scrape returns the body of the prometheus endpoint at url.
func Scrape(t *testing.T, url string) string {
	t.Helper()

	body, err := scrape(t.Context(), url)
	require.NoError(t, err)

	return body
}

// RequireScrapeContains polls the prometheus endpoint until its body holds every
// want. Middleware-recorded metrics are written after the observed operation
// finishes, so they appear some time after the call that produced them returned.
func RequireScrapeContains(t *testing.T, url string, want ...string) {
	t.Helper()

	require.Eventually(t, func() bool {
		body, err := scrape(t.Context(), url)
		if err != nil {
			return false
		}

		for _, w := range want {
			if !strings.Contains(body, w) {
				return false
			}
		}

		return true
	}, scrapeTimeout, scrapeTick, "%s never reported %q", url, want)
}

func scrape(ctx context.Context, url string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}

	defer func() {
		_ = resp.Body.Close()
	}()

	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	return string(b), nil
}
