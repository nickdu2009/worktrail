package bundle

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// HTTPDownloader streams immutable bundle artifacts over HTTPS.
//
// Client optionally supplies the transport and other HTTP client settings. Its
// redirect policy is replaced so HTTPS and MaxRedirects are always enforced.
// MaxRedirects is the number of redirects that may be followed; zero rejects
// every redirect.
// MaxAttempts retries transient upstream failures (5xx / transport errors).
// Zero means a single attempt.
type HTTPDownloader struct {
	Client       *http.Client
	MaxRedirects int
	MaxAttempts  int
}

// Download fetches rawURL over HTTPS and streams the response body to
// destination. It does not create or write files itself.
func (d HTTPDownloader) Download(ctx context.Context, rawURL string, destination io.Writer) error {
	if destination == nil {
		return errors.New("download destination is required")
	}
	if err := validateDownloadURL(rawURL); err != nil {
		return err
	}
	if d.MaxRedirects < 0 {
		return errors.New("maximum redirects cannot be negative")
	}
	attempts := d.MaxAttempts
	if attempts <= 0 {
		attempts = 1
	}

	var lastErr error
	for attempt := 1; attempt <= attempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		response, err := d.openDownload(ctx, rawURL)
		if err != nil {
			lastErr = err
			if !isTransientDownloadError(err) || attempt == attempts {
				return err
			}
		} else {
			err = streamDownload(ctx, response, destination)
			response.Body.Close()
			if err == nil {
				return nil
			}
			// Body streaming already wrote bytes; do not retry into the same writer.
			return err
		}
		timer := time.NewTimer(time.Duration(attempt) * time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
	return lastErr
}

func (d HTTPDownloader) openDownload(ctx context.Context, rawURL string) (*http.Response, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create download request: %w", err)
	}
	client := d.httpClient()
	client.CheckRedirect = d.checkRedirect(client.CheckRedirect)

	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		_ = response.Body.Close()
		return nil, fmt.Errorf("download request returned HTTP status %d", response.StatusCode)
	}
	return response, nil
}

func streamDownload(ctx context.Context, response *http.Response, destination io.Writer) error {
	_, err := io.Copy(destination, response.Body)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return err
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return nil
}

func isTransientDownloadError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	for _, code := range []string{"HTTP status 500", "HTTP status 502", "HTTP status 503", "HTTP status 504"} {
		if strings.Contains(msg, code) {
			return true
		}
	}
	return false
}

func (d HTTPDownloader) httpClient() *http.Client {
	if d.Client == nil {
		client := *http.DefaultClient
		return &client
	}
	client := *d.Client
	return &client
}

func (d HTTPDownloader) checkRedirect(configured func(*http.Request, []*http.Request) error) func(*http.Request, []*http.Request) error {
	return func(request *http.Request, via []*http.Request) error {
		if err := validateDownloadURL(request.URL.String()); err != nil {
			return err
		}
		if len(via) > d.MaxRedirects {
			return errors.New("download redirect limit exceeded")
		}
		if configured != nil {
			if err := configured(request, via); err != nil {
				return err
			}
		}
		return validateDownloadURL(request.URL.String())
	}
}

func validateDownloadURL(rawURL string) error {
	parsed, err := url.ParseRequestURI(rawURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return errors.New("download URL must be an absolute HTTPS URL")
	}
	if !strings.EqualFold(parsed.Scheme, "https") {
		return errors.New("download URL must use HTTPS")
	}
	if parsed.User != nil {
		return errors.New("download URL must not contain credentials")
	}
	return nil
}
