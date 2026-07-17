package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

func TestHTTPClientReadinessAuthenticatesAndFindsExpectedModel(t *testing.T) {
	const alias = "bundle-a"
	const key = "test-secret-key"
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodGet || request.URL.Path != modelsPath {
			t.Fatalf("request = %s %s", request.Method, request.URL.Path)
		}
		if got := request.Header.Get("Authorization"); got != "Bearer "+key {
			t.Fatalf("Authorization = %q", got)
		}
		_, _ = io.WriteString(writer, `{"data":[{"id":"other","meta":{"n_embd":768}},{"id":"bundle-a","meta":{"n_embd":1024}}]}`)
	}))
	defer server.Close()

	client, err := NewHTTPClient(alias)
	if err != nil {
		t.Fatal(err)
	}
	identity, err := client.Readiness(context.Background(), server.URL, key)
	if err != nil {
		t.Fatalf("Readiness() error = %v", err)
	}
	if identity != (RuntimeIdentity{Alias: alias, Dimension: expectedEmbeddingDimension}) {
		t.Fatalf("Readiness() = %#v", identity)
	}
	if client.client.Timeout != defaultHTTPTimeout {
		t.Fatalf("HTTP timeout = %s, want %s", client.client.Timeout, defaultHTTPTimeout)
	}
}

func TestHTTPClientReadinessRejectsHTTPFailuresWithoutKeyDisclosure(t *testing.T) {
	const key = "must-not-appear-in-errors"
	for _, status := range []int{http.StatusUnauthorized, http.StatusBadRequest, http.StatusInternalServerError} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				writer.WriteHeader(status)
				_, _ = io.WriteString(writer, key)
			}))
			defer server.Close()

			client, err := NewHTTPClient("bundle-a")
			if err != nil {
				t.Fatal(err)
			}
			_, err = client.Readiness(context.Background(), server.URL, key)
			if err == nil {
				t.Fatal("Readiness() error = nil")
			}
			if isRecoverableTransport(err) {
				t.Fatalf("Readiness() error = %T %[1]v, must not be transport", err)
			}
			if strings.Contains(err.Error(), key) {
				t.Fatalf("Readiness() leaked API key in error %q", err)
			}
		})
	}
}

func TestHTTPClientReadinessTreatsServiceUnavailableAsRecoverable(t *testing.T) {
	const key = "must-not-appear-in-errors"
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.WriteHeader(http.StatusServiceUnavailable)
		_, _ = io.WriteString(writer, key)
	}))
	defer server.Close()

	client, err := NewHTTPClient("bundle-a")
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Readiness(context.Background(), server.URL, key)
	if !isRecoverableTransport(err) {
		t.Fatalf("Readiness() error = %T %[1]v, want recoverable transport for 503", err)
	}
	if strings.Contains(err.Error(), key) {
		t.Fatalf("Readiness() leaked API key in error %q", err)
	}
}

func TestHTTPClientReadinessRejectsMalformedModelResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		_, _ = io.WriteString(writer, `{"data":[`)
	}))
	defer server.Close()
	client, err := NewHTTPClient("bundle-a")
	if err != nil {
		t.Fatal(err)
	}

	_, err = client.Readiness(context.Background(), server.URL, "key")
	if err == nil || isRecoverableTransport(err) {
		t.Fatalf("Readiness() error = %v, want unrecoverable malformed response", err)
	}
}

func TestHTTPClientReadinessDoesNotFollowRedirects(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		http.Redirect(writer, request, "http://example.invalid/v1/models", http.StatusFound)
	}))
	defer server.Close()
	client, err := NewHTTPClient("bundle-a")
	if err != nil {
		t.Fatal(err)
	}

	_, err = client.Readiness(context.Background(), server.URL, "key")
	if err == nil || isRecoverableTransport(err) {
		t.Fatalf("Readiness() error = %v, want unrecoverable local redirect", err)
	}
}

func TestHTTPClientReadinessClassifiesOnlyDialTransportFailures(t *testing.T) {
	client, err := newHTTPClient("bundle-a", &http.Client{
		Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return nil, &net.OpError{Op: "dial", Err: errors.New("refused")}
		}),
	})
	if err != nil {
		t.Fatal(err)
	}

	_, err = client.Readiness(context.Background(), "http://127.0.0.1:41414", "key")
	if !isRecoverableTransport(err) {
		t.Fatalf("Readiness() error = %T %[1]v, want recoverable transport", err)
	}
	if strings.Contains(err.Error(), "key") {
		t.Fatalf("Readiness() leaked API key in error %q", err)
	}
}

func TestHTTPClientRejectsUnsafeEndpoints(t *testing.T) {
	client, err := NewHTTPClient("bundle-a")
	if err != nil {
		t.Fatal(err)
	}
	for _, endpoint := range []string{
		"https://127.0.0.1:8080",
		"http://localhost:8080",
		"http://user:secret@127.0.0.1:8080",
		"http://127.0.0.1:8080/v1/models",
		"http://127.0.0.1:8080?x=1",
	} {
		t.Run(endpoint, func(t *testing.T) {
			if _, err := client.Readiness(context.Background(), endpoint, "key"); err == nil {
				t.Fatalf("Readiness(%q) accepted unsafe endpoint", endpoint)
			}
		})
	}
}

func TestHTTPClientEmbedValidatesOrderedNormalizedEmbeddings(t *testing.T) {
	const alias = "bundle-a"
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost || request.URL.Path != embeddingsPath {
			t.Fatalf("request = %s %s", request.Method, request.URL.Path)
		}
		if request.Header.Get("Authorization") != "Bearer key" {
			t.Fatal("embedding request was not authenticated")
		}
		body, err := io.ReadAll(request.Body)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(body), "key") {
			t.Fatal("embedding request body contains API key")
		}
		_, _ = io.WriteString(writer, embeddingResponse([]int{0, 1}, []float64{1, 1}))
	}))
	defer server.Close()
	client, err := NewHTTPClient(alias)
	if err != nil {
		t.Fatal(err)
	}

	embeddings, err := client.Embed(context.Background(), server.URL, "key", alias, []string{"first", "second"})
	if err != nil {
		t.Fatalf("Embed() error = %v", err)
	}
	if len(embeddings) != 2 || embeddings[0].Index != 0 || embeddings[1].Index != 1 ||
		len(embeddings[0].Values) != expectedEmbeddingDimension {
		t.Fatalf("Embed() = %#v", embeddings)
	}
}

func TestHTTPClientEmbedRejectsMalformedVectors(t *testing.T) {
	const alias = "bundle-a"
	cases := []struct {
		name string
		body string
	}{
		{name: "NaN", body: `{"data":[{"index":0,"embedding":[NaN]}]}`},
		{name: "not-normalized", body: embeddingResponse([]int{0}, []float64{2})},
		{name: "out-of-order", body: embeddingResponse([]int{1}, []float64{1})},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				_, _ = io.WriteString(writer, test.body)
			}))
			defer server.Close()
			client, err := NewHTTPClient(alias)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := client.Embed(context.Background(), server.URL, "key", alias, []string{"input"}); err == nil {
				t.Fatal("Embed() accepted malformed embedding response")
			}
		})
	}
}

func TestHTTPClientEmbedRejectsEmptyInputs(t *testing.T) {
	client, err := NewHTTPClient("bundle-a")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Embed(context.Background(), "http://127.0.0.1:8080", "key", "bundle-a", nil); err == nil {
		t.Fatal("Embed() accepted empty inputs")
	}
}

func TestHTTPClientCountTokensAuthenticatesAndSendsContentOnly(t *testing.T) {
	const key = "test-secret-key"
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost || request.URL.Path != tokenizePath {
			t.Fatalf("request = %s %s", request.Method, request.URL.Path)
		}
		if got := request.Header.Get("Authorization"); got != "Bearer "+key {
			t.Fatalf("Authorization = %q", got)
		}
		if got := request.Header.Get("Content-Type"); got != "application/json" {
			t.Fatalf("Content-Type = %q", got)
		}
		var payload map[string]string
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		if len(payload) != 1 || payload["content"] != "authentication gate" {
			t.Fatalf("tokenize payload = %#v", payload)
		}
		_, _ = io.WriteString(writer, `{"tokens":[1,2,3]}`)
	}))
	defer server.Close()

	client, err := NewHTTPClient("bundle-a")
	if err != nil {
		t.Fatal(err)
	}
	count, err := client.CountTokens(context.Background(), server.URL, key, "authentication gate")
	if err != nil {
		t.Fatalf("CountTokens() error = %v", err)
	}
	if count != 3 {
		t.Fatalf("CountTokens() = %d, want 3", count)
	}
}

func TestHTTPClientCountTokensRejectsUnsafeEndpointsWithoutDisclosure(t *testing.T) {
	client, err := NewHTTPClient("bundle-a")
	if err != nil {
		t.Fatal(err)
	}
	const key = "must-not-appear-in-errors"
	const endpoint = "http://127.0.0.1:8080?query-must-not-appear-in-errors"
	_, err = client.CountTokens(context.Background(), endpoint, key, "content")
	if err == nil {
		t.Fatal("CountTokens() error = nil")
	}
	if strings.Contains(err.Error(), key) || strings.Contains(err.Error(), "query-must-not-appear-in-errors") {
		t.Fatalf("CountTokens() leaked sensitive endpoint data: %q", err)
	}
}

func TestHTTPClientCountTokensRejectsRedirectsAndHTTPFailures(t *testing.T) {
	const key = "must-not-appear-in-errors"
	for _, status := range []int{http.StatusFound, http.StatusUnauthorized, http.StatusInternalServerError} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				if status == http.StatusFound {
					http.Redirect(writer, request, "http://example.invalid/tokenize", status)
					return
				}
				writer.WriteHeader(status)
				_, _ = io.WriteString(writer, key)
			}))
			defer server.Close()

			client, err := NewHTTPClient("bundle-a")
			if err != nil {
				t.Fatal(err)
			}
			_, err = client.CountTokens(context.Background(), server.URL, key, "content")
			if err == nil || isRecoverableTransport(err) {
				t.Fatalf("CountTokens() error = %v, want unrecoverable HTTP failure", err)
			}
			if strings.Contains(err.Error(), key) {
				t.Fatalf("CountTokens() leaked API key in error %q", err)
			}
		})
	}
}

func TestHTTPClientCountTokensRejectsMalformedTokenArrays(t *testing.T) {
	for _, body := range []string{
		`{}`,
		`{"tokens":null}`,
		`{"tokens":"not-an-array"}`,
		`{"tokens":[1,"two"]}`,
		`{"tokens":[1.5]}`,
		`{"tokens":[1]} trailing`,
	} {
		t.Run(body, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				_, _ = io.WriteString(writer, body)
			}))
			defer server.Close()
			client, err := NewHTTPClient("bundle-a")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := client.CountTokens(context.Background(), server.URL, "key", "content"); err == nil {
				t.Fatalf("CountTokens() accepted malformed response %q", body)
			}
		})
	}
}

func TestValidateEmbeddingRejectsNaN(t *testing.T) {
	values := make([]float64, expectedEmbeddingDimension)
	values[0] = math.NaN()
	if err := validateEmbedding(values); err == nil || !strings.Contains(err.Error(), "non-finite") {
		t.Fatalf("validateEmbedding(NaN) error = %v, want non-finite rejection", err)
	}
}

func embeddingResponse(indices []int, firstValues []float64) string {
	var response strings.Builder
	response.WriteString(`{"data":[`)
	for i, index := range indices {
		if i != 0 {
			response.WriteByte(',')
		}
		value := firstValues[i]
		response.WriteString(`{"index":`)
		response.WriteString(stringInt(index))
		response.WriteString(`,"embedding":[`)
		response.WriteString(floatString(value))
		for dimension := 1; dimension < expectedEmbeddingDimension; dimension++ {
			response.WriteString(`,0`)
		}
		response.WriteString(`]}`)
	}
	response.WriteString(`]}`)
	return response.String()
}

func stringInt(value int) string {
	return strconv.Itoa(value)
}

func floatString(value float64) string {
	return strconv.FormatFloat(value, 'g', -1, 64)
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}
