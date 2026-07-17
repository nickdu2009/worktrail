package daemon

import (
	"context"
	"errors"
	"fmt"
	"math"

	"github.com/nickdu2009/worktrail/internal/semantic/contracts"
	"github.com/nickdu2009/worktrail/internal/semantic/generation"
)

const documentEmbeddingBatchSize = 16

// StoreCredentials supplies the current local daemon descriptor and API key.
// Store satisfies this interface without starting or stopping a process.
type StoreCredentials interface {
	Load() (Descriptor, error)
	APIKey() (string, error)
}

var _ StoreCredentials = Store{}

// DaemonTokenCounter adapts the local daemon token-counting boundary to
// contracts.TokenCounter. It loads credentials for every request and does not
// retain document text or API keys.
type DaemonTokenCounter struct {
	Counter     TokenCounter
	Credentials StoreCredentials
}

var _ contracts.TokenCounter = DaemonTokenCounter{}

// CountTokens counts text with the currently stored daemon descriptor.
func (a DaemonTokenCounter) CountTokens(ctx context.Context, text string) (int, error) {
	if a.Counter == nil {
		return 0, errors.New("semantic daemon token counter is not configured")
	}
	if a.Credentials == nil {
		return 0, errors.New("semantic daemon credentials are not configured")
	}

	descriptor, err := a.Credentials.Load()
	if err != nil {
		return 0, fmt.Errorf("load semantic daemon descriptor: %w", err)
	}
	apiKey, err := a.Credentials.APIKey()
	if err != nil {
		return 0, fmt.Errorf("load semantic daemon API key: %w", err)
	}
	count, err := a.Counter.CountTokens(ctx, descriptor.Endpoint, apiKey, text)
	if err != nil {
		return 0, fmt.Errorf("count semantic daemon tokens: %w", err)
	}
	return count, nil
}

// DaemonGenerationEmbedder adapts batched local daemon embeddings to the
// generation.Embedder contract. It loads credentials per request and does not
// start or stop a daemon.
type DaemonGenerationEmbedder struct {
	Embedder    Embedder
	Credentials StoreCredentials
}

var _ generation.Embedder = DaemonGenerationEmbedder{}

// Embed embeds one document input through the same bounded batch path used by
// callers that have multiple document inputs.
func (a DaemonGenerationEmbedder) Embed(ctx context.Context, input string) ([]float32, error) {
	vectors, err := a.EmbedBatch(ctx, []string{input})
	if err != nil {
		return nil, err
	}
	return vectors[0], nil
}

// EmbedBatch embeds document inputs in bounded batches while preserving input
// order. It reads the descriptor and API key exactly once per call.
func (a DaemonGenerationEmbedder) EmbedBatch(ctx context.Context, inputs []string) ([][]float32, error) {
	if a.Embedder == nil {
		return nil, errors.New("semantic daemon embedder is not configured")
	}
	if a.Credentials == nil {
		return nil, errors.New("semantic daemon credentials are not configured")
	}
	if len(inputs) == 0 {
		return [][]float32{}, nil
	}

	descriptor, err := a.Credentials.Load()
	if err != nil {
		return nil, fmt.Errorf("load semantic daemon descriptor: %w", err)
	}
	apiKey, err := a.Credentials.APIKey()
	if err != nil {
		return nil, fmt.Errorf("load semantic daemon API key: %w", err)
	}

	vectors := make([][]float32, len(inputs))
	for start := 0; start < len(inputs); start += documentEmbeddingBatchSize {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("embed semantic daemon documents: %w", err)
		}
		end := start + documentEmbeddingBatchSize
		if end > len(inputs) {
			end = len(inputs)
		}
		embeddings, err := a.Embedder.Embed(ctx, descriptor.Endpoint, apiKey, descriptor.Alias, inputs[start:end])
		if err != nil {
			return nil, fmt.Errorf("embed semantic daemon documents: %w", err)
		}
		if len(embeddings) != end-start {
			return nil, errors.New("semantic daemon document response count does not match inputs")
		}
		for index, embedding := range embeddings {
			if embedding.Index != index {
				return nil, errors.New("semantic daemon document response is not in input order")
			}
			vector, err := convertDocumentEmbedding(embedding.Values)
			if err != nil {
				return nil, err
			}
			vectors[start+index] = vector
		}
	}
	return vectors, nil
}

func convertDocumentEmbedding(values []float64) ([]float32, error) {
	if err := validateEmbedding(values); err != nil {
		return nil, fmt.Errorf("validate semantic daemon document embedding: %w", err)
	}

	vector := make([]float32, len(values))
	var squaredLength float64
	for index, value := range values {
		converted := float32(value)
		if math.IsNaN(float64(converted)) || math.IsInf(float64(converted), 0) {
			return nil, errors.New("semantic daemon document embedding conversion contains a non-finite value")
		}
		vector[index] = converted
		squaredLength += float64(converted) * float64(converted)
	}
	length := math.Sqrt(squaredLength)
	if math.IsNaN(length) || math.IsInf(length, 0) || math.Abs(length-1) > embeddingNormTolerance {
		return nil, errors.New("semantic daemon document embedding is not L2-normalized after conversion")
	}
	return vector, nil
}
