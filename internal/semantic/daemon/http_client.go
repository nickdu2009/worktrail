package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const (
	defaultHTTPTimeout     = 5 * time.Second
	maxHTTPResponseBytes   = 1 << 20
	embeddingNormTolerance = 1e-3
	modelsPath             = "/v1/models"
	embeddingsPath         = "/v1/embeddings"
	tokenizePath           = "/tokenize"
)

// Embedding is one normalized embedding returned in input order.
type Embedding struct {
	Index  int
	Values []float64
}

// Embedder is the local llama.app embedding API boundary. It is intentionally
// independent of retrieval so generation and retrieval can consume it directly.
type Embedder interface {
	Embed(ctx context.Context, endpoint, apiKey, alias string, inputs []string) ([]Embedding, error)
}

// TokenCounter is the authenticated local llama.app token-counting boundary.
// It accepts only a root loopback endpoint and never includes credentials or
// endpoint query data in errors.
type TokenCounter interface {
	CountTokens(ctx context.Context, endpoint, apiKey, content string) (int, error)
}

// HTTPClient is the production loopback-only llama.app HTTP client.
// Its expected alias is fixed when constructed so readiness cannot accept an
// arbitrary model advertised by a local endpoint.
type HTTPClient struct {
	expectedAlias string
	client        *http.Client
}

var (
	_ ReadinessClient = (*HTTPClient)(nil)
	_ Embedder        = (*HTTPClient)(nil)
	_ TokenCounter    = (*HTTPClient)(nil)
)

// NewHTTPClient constructs a bounded production client for one trusted model
// alias. It creates no connection until Readiness or Embed is called.
func NewHTTPClient(expectedAlias string) (*HTTPClient, error) {
	if err := validateModelAlias(expectedAlias); err != nil {
		return nil, err
	}
	return newHTTPClient(expectedAlias, &http.Client{
		Timeout: defaultHTTPTimeout,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	})
}

func newHTTPClient(expectedAlias string, client *http.Client) (*HTTPClient, error) {
	if err := validateModelAlias(expectedAlias); err != nil {
		return nil, err
	}
	if client == nil {
		return nil, errors.New("semantic daemon HTTP client is required")
	}
	return &HTTPClient{expectedAlias: expectedAlias, client: client}, nil
}

// Readiness verifies the authenticated /v1/models contract for the client's
// expected alias and required 1024-dimensional embedding metadata.
func (c *HTTPClient) Readiness(ctx context.Context, endpoint, apiKey string) (RuntimeIdentity, error) {
	if err := validateAPIKey(apiKey); err != nil {
		return RuntimeIdentity{}, err
	}
	base, err := validateLoopbackEndpoint(endpoint)
	if err != nil {
		return RuntimeIdentity{}, err
	}

	response, err := c.request(ctx, http.MethodGet, base+modelsPath, apiKey, nil)
	if err != nil {
		return RuntimeIdentity{}, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		// llama.app can bind and answer before the model is ready; 503 is the
		// cold-start "not ready" signal and must be polled, not treated as a
		// permanent protocol failure.
		if response.StatusCode == http.StatusServiceUnavailable {
			return RuntimeIdentity{}, &TransportError{Err: httpStatusError(response.StatusCode)}
		}
		return RuntimeIdentity{}, httpStatusError(response.StatusCode)
	}

	var payload struct {
		Data []struct {
			ID   string `json:"id"`
			Meta struct {
				Dimension int `json:"n_embd"`
			} `json:"meta"`
		} `json:"data"`
	}
	if err := decodeResponse(response.Body, &payload); err != nil {
		return RuntimeIdentity{}, fmt.Errorf("decode semantic daemon models response: %w", err)
	}

	var match *struct {
		ID   string `json:"id"`
		Meta struct {
			Dimension int `json:"n_embd"`
		} `json:"meta"`
	}
	for index := range payload.Data {
		model := &payload.Data[index]
		if model.ID != c.expectedAlias {
			continue
		}
		if match != nil {
			return RuntimeIdentity{}, errors.New("semantic daemon models response contains duplicate expected alias")
		}
		match = model
	}
	if match == nil {
		return RuntimeIdentity{}, errors.New("semantic daemon models response is missing expected alias")
	}
	if match.Meta.Dimension != expectedEmbeddingDimension {
		return RuntimeIdentity{}, fmt.Errorf("semantic daemon model embedding dimension = %d, want %d", match.Meta.Dimension, expectedEmbeddingDimension)
	}
	return RuntimeIdentity{Alias: match.ID, Dimension: match.Meta.Dimension}, nil
}

// Embed posts inputs to the authenticated /v1/embeddings endpoint and verifies
// that the response remains ordered, complete, finite, and L2-normalized.
func (c *HTTPClient) Embed(ctx context.Context, endpoint, apiKey, alias string, inputs []string) ([]Embedding, error) {
	if err := validateAPIKey(apiKey); err != nil {
		return nil, err
	}
	if err := validateModelAlias(alias); err != nil {
		return nil, err
	}
	if alias != c.expectedAlias {
		return nil, errors.New("semantic daemon embedding alias does not match client")
	}
	if len(inputs) == 0 {
		return nil, errors.New("semantic daemon embedding inputs are required")
	}
	base, err := validateLoopbackEndpoint(endpoint)
	if err != nil {
		return nil, err
	}
	body, err := json.Marshal(struct {
		Model string   `json:"model"`
		Input []string `json:"input"`
	}{Model: alias, Input: inputs})
	if err != nil {
		return nil, errors.New("encode semantic daemon embedding request")
	}

	response, err := c.request(ctx, http.MethodPost, base+embeddingsPath, apiKey, body)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, httpStatusError(response.StatusCode)
	}

	var payload struct {
		Data []struct {
			Index     int       `json:"index"`
			Embedding []float64 `json:"embedding"`
		} `json:"data"`
	}
	if err := decodeResponse(response.Body, &payload); err != nil {
		return nil, fmt.Errorf("decode semantic daemon embeddings response: %w", err)
	}
	if len(payload.Data) != len(inputs) {
		return nil, errors.New("semantic daemon embeddings response count does not match inputs")
	}

	embeddings := make([]Embedding, len(payload.Data))
	for expectedIndex, value := range payload.Data {
		if value.Index != expectedIndex {
			return nil, errors.New("semantic daemon embeddings response is not in input order")
		}
		if err := validateEmbedding(value.Embedding); err != nil {
			return nil, err
		}
		embeddings[expectedIndex] = Embedding{
			Index:  value.Index,
			Values: append([]float64(nil), value.Embedding...),
		}
	}
	return embeddings, nil
}

// CountTokens posts content to the authenticated llama.app /tokenize endpoint
// and returns the number of token IDs it produces.
func (c *HTTPClient) CountTokens(ctx context.Context, endpoint, apiKey, content string) (int, error) {
	if err := validateAPIKey(apiKey); err != nil {
		return 0, err
	}
	base, err := validateLoopbackEndpoint(endpoint)
	if err != nil {
		return 0, err
	}
	body, err := json.Marshal(struct {
		Content string `json:"content"`
	}{Content: content})
	if err != nil {
		return 0, errors.New("encode semantic daemon tokenize request")
	}

	response, err := c.request(ctx, http.MethodPost, base+tokenizePath, apiKey, body)
	if err != nil {
		return 0, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return 0, httpStatusError(response.StatusCode)
	}

	var payload struct {
		Tokens json.RawMessage `json:"tokens"`
	}
	if err := decodeResponse(response.Body, &payload); err != nil {
		return 0, fmt.Errorf("decode semantic daemon tokenize response: %w", err)
	}
	if len(payload.Tokens) == 0 || bytes.Equal(payload.Tokens, []byte("null")) {
		return 0, errors.New("semantic daemon tokenize response is missing tokens")
	}
	var tokens []int
	if err := json.Unmarshal(payload.Tokens, &tokens); err != nil || tokens == nil {
		return 0, errors.New("semantic daemon tokenize response tokens are malformed")
	}
	return len(tokens), nil
}

func (c *HTTPClient) request(ctx context.Context, method, target, apiKey string, body []byte) (*http.Response, error) {
	request, err := http.NewRequestWithContext(ctx, method, target, bytes.NewReader(body))
	if err != nil {
		return nil, errors.New("create semantic daemon HTTP request")
	}
	request.Header.Set("Authorization", "Bearer "+apiKey)
	if len(body) != 0 {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := c.client.Do(request)
	if err != nil {
		if isNetworkTransportError(err) {
			return nil, &TransportError{Err: errors.New("semantic daemon HTTP transport unavailable")}
		}
		return nil, errors.New("send semantic daemon HTTP request")
	}
	return response, nil
}

func validateLoopbackEndpoint(endpoint string) (string, error) {
	parsed, err := url.ParseRequestURI(endpoint)
	if err != nil ||
		parsed.Scheme != "http" ||
		parsed.User != nil ||
		parsed.Hostname() != "127.0.0.1" ||
		(parsed.Path != "" && parsed.Path != "/") ||
		parsed.RawQuery != "" ||
		parsed.Fragment != "" {
		return "", errors.New("semantic daemon endpoint must be an HTTP 127.0.0.1 root URL")
	}
	port, err := strconv.Atoi(parsed.Port())
	if err != nil || port < 1 || port > 65535 {
		return "", errors.New("semantic daemon endpoint must include a valid port")
	}
	return "http://127.0.0.1:" + strconv.Itoa(port), nil
}

func validateModelAlias(alias string) error {
	if alias == "" || strings.TrimSpace(alias) != alias || strings.ContainsAny(alias, "/\\") {
		return errors.New("semantic daemon model alias is invalid")
	}
	return nil
}

func decodeResponse(body io.Reader, destination any) error {
	limited := io.LimitReader(body, maxHTTPResponseBytes+1)
	data, err := io.ReadAll(limited)
	if err != nil {
		return errors.New("read semantic daemon HTTP response")
	}
	if len(data) > maxHTTPResponseBytes {
		return errors.New("semantic daemon HTTP response exceeds size limit")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	if err := decoder.Decode(destination); err != nil {
		return errors.New("semantic daemon HTTP response is malformed")
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		return errors.New("semantic daemon HTTP response has trailing data")
	}
	return nil
}

func httpStatusError(status int) error {
	return fmt.Errorf("semantic daemon HTTP status %d", status)
}

func isNetworkTransportError(err error) bool {
	if errors.Is(err, syscall.ECONNREFUSED) {
		return true
	}
	var networkError net.Error
	if errors.As(err, &networkError) && networkError.Timeout() {
		return true
	}
	var operationError *net.OpError
	return errors.As(err, &operationError) && operationError.Op == "dial"
}

func validateEmbedding(values []float64) error {
	if len(values) != expectedEmbeddingDimension {
		return fmt.Errorf("semantic daemon embedding dimension = %d, want %d", len(values), expectedEmbeddingDimension)
	}
	var squaredLength float64
	for _, value := range values {
		if math.IsNaN(value) || math.IsInf(value, 0) {
			return errors.New("semantic daemon embedding contains a non-finite value")
		}
		squaredLength += value * value
	}
	length := math.Sqrt(squaredLength)
	if math.IsNaN(length) || math.IsInf(length, 0) || math.Abs(length-1) > embeddingNormTolerance {
		return errors.New("semantic daemon embedding is not L2-normalized")
	}
	return nil
}
