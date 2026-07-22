package service

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"time"

	"github.com/nickdu2009/worktrail/internal/paths"
	"github.com/nickdu2009/worktrail/internal/semantic/contracts"
	"github.com/nickdu2009/worktrail/internal/semantic/daemon"
	"github.com/nickdu2009/worktrail/internal/semantic/generation"
	"github.com/nickdu2009/worktrail/internal/semantic/retrieve"
)

const (
	clientTimeout              = 60 * time.Second
	hostActivationTimeout      = 5 * time.Second
	hostActivationPollInterval = 100 * time.Millisecond
	embeddingBatchSize         = 16
	embeddingDimension         = 1024
	embeddingNormTolerance     = 1e-3
)

type Client struct {
	Roots              paths.SemanticRoots
	BundleID           string
	RuntimeFingerprint string
	SupportLevel       string
	Chip               string
	Manager            Manager
	http               *http.Client
}

var (
	_ daemon.Controller      = (*Client)(nil)
	_ contracts.TokenCounter = (*Client)(nil)
	_ generation.Embedder    = (*Client)(nil)
	_ retrieve.QueryEmbedder = (*Client)(nil)
)

func NewClient(roots paths.SemanticRoots, bundleID, runtimeFingerprint, supportLevel, chip string) (*Client, error) {
	manager, err := NewManager(roots)
	if err != nil {
		return nil, err
	}
	transport := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", roots.ServiceSocket())
		},
	}
	return &Client{
		Roots:              roots,
		BundleID:           bundleID,
		RuntimeFingerprint: runtimeFingerprint,
		SupportLevel:       supportLevel,
		Chip:               chip,
		Manager:            manager,
		http:               &http.Client{Transport: transport, Timeout: clientTimeout},
	}, nil
}

func (c *Client) Status(ctx context.Context) (daemon.Report, error) {
	payload, err := c.call(ctx, http.MethodGet, "/v1/status", nil, false)
	if err == nil {
		return c.report(payload.Report), nil
	}
	if !recoverable(err) {
		return unavailableClientReport("status", reasonOf(err), c), nil
	}
	inspection, inspectErr := c.Manager.Inspect(ctx)
	if inspectErr != nil {
		return unavailableClientReport("status", reasonOf(inspectErr), c), nil
	}
	if !inspection.Installed {
		return unavailableClientReport("status", contracts.ReasonServiceNotInstalled, c), nil
	}
	return c.report(daemon.Report{
		Schema:                   daemon.ReportSchema,
		Operation:                "status",
		State:                    daemon.StateStopped,
		ServiceRegistrationState: "registered",
		ServiceDomain:            inspection.Domain,
		HostState:                "stopped",
		WorkerState:              "idle_stopped",
	}), nil
}

// Activate asks launchd to start the Host and verifies only the Host protocol.
// It intentionally does not start the worker or load the model.
func (c *Client) Activate(ctx context.Context) error {
	if err := c.Manager.Activate(ctx); err != nil {
		return err
	}
	return c.waitForHost(ctx)
}

func (c *Client) Start(ctx context.Context) (daemon.Report, error) {
	payload, err := c.lifecycle(ctx, "/v1/runtime/start", true)
	if err != nil {
		return daemon.Report{}, err
	}
	return c.report(payload.Report), nil
}

func (c *Client) Stop(ctx context.Context) (daemon.Report, error) {
	payload, err := c.lifecycle(ctx, "/v1/runtime/stop", false)
	if recoverable(err) {
		return c.report(daemon.Report{Schema: daemon.ReportSchema, Operation: "stop", State: daemon.StateStopped, HostState: "stopped", WorkerState: "idle_stopped"}), nil
	}
	if err != nil {
		return daemon.Report{}, err
	}
	return c.report(payload.Report), nil
}

func (c *Client) Restart(ctx context.Context) (daemon.Report, error) {
	payload, err := c.lifecycle(ctx, "/v1/runtime/restart", false)
	if err == nil {
		return c.report(payload.Report), nil
	}
	if !recoverable(err) && reasonOf(err) != contracts.ReasonServiceIncompatible {
		return daemon.Report{}, err
	}
	if err := c.Manager.Restart(ctx); err != nil {
		return daemon.Report{}, err
	}
	if err := c.waitForHost(ctx); err != nil {
		return daemon.Report{}, err
	}
	payload, err = c.lifecycle(ctx, "/v1/runtime/restart", false)
	if err != nil {
		return daemon.Report{}, err
	}
	return c.report(payload.Report), nil
}

func (c *Client) CountTokens(ctx context.Context, text string) (int, error) {
	if c.BundleID == "" {
		return 0, serviceError(contracts.ReasonRuntimeIdentityMismatch, "semantic service bundle identity is required", nil)
	}
	if err := c.ensureCompatible(ctx); err != nil {
		return 0, err
	}
	payload, err := c.call(ctx, http.MethodPost, "/v1/tokenize", tokenizeRequest{
		ProtocolVersion: ProtocolVersion,
		RequestID:       requestID(),
		BundleID:        c.BundleID,
		Text:            text,
	}, true)
	if err != nil {
		return 0, err
	}
	return payload.TokenCount, nil
}

func (c *Client) Embed(ctx context.Context, input string) ([]float32, error) {
	vectors, err := c.EmbedBatch(ctx, []string{input})
	if err != nil {
		return nil, err
	}
	return vectors[0], nil
}

func (c *Client) EmbedQuery(ctx context.Context, query string) ([]float32, error) {
	return c.Embed(ctx, query)
}

func (c *Client) EmbedBatch(ctx context.Context, inputs []string) ([][]float32, error) {
	if len(inputs) == 0 {
		return [][]float32{}, nil
	}
	if c.BundleID == "" {
		return nil, serviceError(contracts.ReasonRuntimeIdentityMismatch, "semantic service bundle identity is required", nil)
	}
	if err := c.ensureCompatible(ctx); err != nil {
		return nil, err
	}
	vectors := make([][]float32, len(inputs))
	for start := 0; start < len(inputs); start += embeddingBatchSize {
		end := start + embeddingBatchSize
		if end > len(inputs) {
			end = len(inputs)
		}
		payload, err := c.call(ctx, http.MethodPost, "/v1/embeddings", embeddingsRequest{
			ProtocolVersion: ProtocolVersion,
			RequestID:       requestID(),
			BundleID:        c.BundleID,
			Inputs:          inputs[start:end],
		}, true)
		if err != nil {
			return nil, err
		}
		if len(payload.Embeddings) != end-start {
			return nil, errors.New("semantic service embedding response count does not match inputs")
		}
		for index, embedding := range payload.Embeddings {
			if embedding.Index != index {
				return nil, errors.New("semantic service embedding response is not in input order")
			}
			vector, err := convertEmbedding(embedding.Values)
			if err != nil {
				return nil, err
			}
			vectors[start+index] = vector
		}
	}
	return vectors, nil
}

func (c *Client) lifecycle(ctx context.Context, path string, activate bool) (response, error) {
	if _, err := c.call(ctx, http.MethodGet, "/v1/status", nil, activate); err != nil {
		return response{}, err
	}
	return c.call(ctx, http.MethodPost, path, lifecycleRequest{
		ProtocolVersion: ProtocolVersion,
		RequestID:       requestID(),
		BundleID:        c.BundleID,
	}, activate)
}

func (c *Client) ensureCompatible(ctx context.Context) error {
	payload, err := c.call(ctx, http.MethodGet, "/v1/status", nil, true)
	if err != nil {
		return err
	}
	return c.validate(payload)
}

func (c *Client) call(ctx context.Context, method, path string, body any, activate bool) (response, error) {
	payload, err := c.request(ctx, method, path, body)
	if err == nil || !activate || !recoverable(err) {
		return payload, err
	}
	err = withActivationLock(ctx, c.Roots.ServiceActivationLock(), func() error {
		if _, retryErr := c.request(ctx, http.MethodGet, "/v1/status", nil); retryErr == nil {
			return nil
		}
		if activateErr := c.Manager.Activate(ctx); activateErr != nil {
			return activateErr
		}
		return c.waitForHost(ctx)
	})
	if err != nil {
		return response{}, err
	}
	return c.request(ctx, method, path, body)
}

func (c *Client) waitForHost(ctx context.Context) error {
	deadline := time.Now().Add(hostActivationTimeout)
	for {
		if _, err := c.request(ctx, http.MethodGet, "/v1/status", nil); err == nil {
			return nil
		}
		if !time.Now().Before(deadline) {
			return serviceError(contracts.ReasonServiceUnavailable, "semantic service did not become ready", nil)
		}
		timer := time.NewTimer(hostActivationPollInterval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

func (c *Client) request(ctx context.Context, method, path string, body any) (response, error) {
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return response{}, err
		}
		if len(data) > maxBodyBytes {
			return response{}, errors.New("semantic service request is too large")
		}
		reader = bytes.NewReader(data)
	}
	request, err := http.NewRequestWithContext(ctx, method, "http://unix"+path, reader)
	if err != nil {
		return response{}, err
	}
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	httpResponse, err := c.http.Do(request)
	if err != nil {
		return response{}, &transportError{err: err}
	}
	defer httpResponse.Body.Close()
	limited := io.LimitReader(httpResponse.Body, maxBodyBytes+1)
	data, err := io.ReadAll(limited)
	if err != nil {
		return response{}, err
	}
	if len(data) > maxBodyBytes {
		return response{}, errors.New("semantic service response is too large")
	}
	var payload response
	if err := json.Unmarshal(data, &payload); err != nil {
		return response{}, serviceError(contracts.ReasonServiceIncompatible, "semantic service response is incompatible", err)
	}
	if err := c.validate(payload); err != nil {
		return response{}, err
	}
	if httpResponse.StatusCode < 200 || httpResponse.StatusCode >= 300 || payload.ErrorCode != "" {
		code := payload.ErrorCode
		if code == "" {
			code = contracts.ReasonServiceUnavailable
		}
		return response{}, serviceError(code, string(code), nil)
	}
	return payload, nil
}

func (c *Client) validate(payload response) error {
	if payload.ProtocolVersion != ProtocolVersion || payload.HostBuildID != HostBuildID {
		return serviceError(contracts.ReasonServiceIncompatible, "semantic service protocol is incompatible", nil)
	}
	if payload.BundleID != "" && c.BundleID != "" && payload.BundleID != c.BundleID {
		return serviceError(contracts.ReasonRuntimeIdentityMismatch, "semantic service bundle identity mismatch", nil)
	}
	if payload.RuntimeFingerprint != "" && c.RuntimeFingerprint != "" && payload.RuntimeFingerprint != c.RuntimeFingerprint {
		return serviceError(contracts.ReasonRuntimeIdentityMismatch, "semantic service runtime fingerprint mismatch", nil)
	}
	return nil
}

func (c *Client) report(report daemon.Report) daemon.Report {
	if report.Schema == "" {
		report.Schema = daemon.ReportSchema
	}
	if report.SupportLevel == "" {
		report.SupportLevel = c.SupportLevel
	}
	if report.Chip == "" {
		report.Chip = c.Chip
	}
	return report
}

func unavailableClientReport(operation string, code contracts.ReasonCode, client *Client) daemon.Report {
	return client.report(daemon.Report{
		Schema:    daemon.ReportSchema,
		Operation: operation,
		State:     daemon.StateUnavailable,
		Reason:    code,
		NextStep:  nextStep(code),
	})
}

func nextStep(code contracts.ReasonCode) string {
	switch code {
	case contracts.ReasonServiceNotInstalled, contracts.ReasonServiceIncompatible:
		return "worktrail init --semantic"
	case contracts.ReasonPlatformUnsupported:
		return "semantic runtime requires a supported macOS GUI login session"
	default:
		return "worktrail semantic restart"
	}
}

func convertEmbedding(values []float64) ([]float32, error) {
	if len(values) != embeddingDimension {
		return nil, fmt.Errorf("semantic service embedding dimension = %d, want %d", len(values), embeddingDimension)
	}
	vector := make([]float32, len(values))
	var squaredLength float64
	for index, value := range values {
		if math.IsNaN(value) || math.IsInf(value, 0) {
			return nil, errors.New("semantic service embedding contains a non-finite value")
		}
		converted := float32(value)
		if math.IsNaN(float64(converted)) || math.IsInf(float64(converted), 0) {
			return nil, errors.New("semantic service embedding conversion contains a non-finite value")
		}
		vector[index] = converted
		squaredLength += float64(converted) * float64(converted)
	}
	length := math.Sqrt(squaredLength)
	if math.IsNaN(length) || math.IsInf(length, 0) || math.Abs(length-1) > embeddingNormTolerance {
		return nil, errors.New("semantic service embedding is not L2-normalized")
	}
	return vector, nil
}

func requestID() string {
	buffer := make([]byte, 16)
	if _, err := rand.Read(buffer); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(buffer)
}

type transportError struct{ err error }

func (e *transportError) Error() string { return "semantic service transport unavailable" }
func (e *transportError) Unwrap() error { return e.err }

func recoverable(err error) bool {
	var transport *transportError
	return errors.As(err, &transport)
}

func reasonOf(err error) contracts.ReasonCode {
	var typed *daemon.Error
	if errors.As(err, &typed) && typed.Code != "" {
		return typed.Code
	}
	return contracts.ReasonServiceUnavailable
}
