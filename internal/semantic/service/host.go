package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/nickdu2009/worktrail/internal/paths"
	"github.com/nickdu2009/worktrail/internal/semantic/contracts"
	"github.com/nickdu2009/worktrail/internal/semantic/daemon"
)

type Host struct {
	Roots              paths.SemanticRoots
	BundleID           string
	RuntimeFingerprint string
	Controller         daemon.Controller
	Store              daemon.Store
	WorkerClient       *daemon.HTTPClient
	IdleTimeout        time.Duration

	mu            sync.Mutex
	active        int
	lastCompleted time.Time
	startedAt     time.Time
	timer         *time.Timer
	server        *http.Server
	done          chan struct{}
	shutdownOnce  sync.Once
	shutdownErr   error
}

func (h *Host) Run(ctx context.Context) error {
	if h.Controller == nil || h.WorkerClient == nil || h.BundleID == "" {
		return errors.New("semantic service Host is not configured")
	}
	if h.IdleTimeout == 0 {
		h.IdleTimeout = defaultIdleTimeout
	}
	runtimeDir := h.Roots.ServiceRuntime()
	if err := os.MkdirAll(runtimeDir, 0o700); err != nil {
		return fmt.Errorf("create semantic service runtime directory: %w", err)
	}
	if err := os.Chmod(runtimeDir, 0o700); err != nil {
		return fmt.Errorf("secure semantic service runtime directory: %w", err)
	}
	listener, err := listenOwnedSocket(h.Roots.ServiceSocket(), os.Getuid())
	if err != nil {
		return fmt.Errorf("listen on semantic service socket: %w", err)
	}
	defer listener.Close()
	socketInfo, err := os.Lstat(h.Roots.ServiceSocket())
	if err != nil {
		return fmt.Errorf("inspect semantic service socket: %w", err)
	}
	defer removeSocketIfSame(h.Roots.ServiceSocket(), socketInfo)

	h.server = &http.Server{Handler: h.routes(), ReadHeaderTimeout: 2 * time.Second}
	h.done = make(chan struct{})
	h.startedAt = time.Now().UTC()
	defer h.finish(true)
	h.mu.Lock()
	h.timer = time.AfterFunc(h.IdleTimeout, h.idle)
	h.mu.Unlock()
	go func() {
		<-ctx.Done()
		h.finish(true)
	}()
	err = h.server.Serve(listener)
	if errors.Is(err, http.ErrServerClosed) {
		<-h.done
		h.mu.Lock()
		shutdownErr := h.shutdownErr
		h.mu.Unlock()
		return shutdownErr
	}
	return err
}

func removeSocketIfSame(path string, expected os.FileInfo) {
	current, err := os.Lstat(path)
	if err != nil || current.Mode()&os.ModeSocket == 0 || !os.SameFile(expected, current) {
		return
	}
	_ = os.Remove(path)
}

func (h *Host) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/status", h.status)
	mux.HandleFunc("POST /v1/runtime/start", h.lifecycle("start"))
	mux.HandleFunc("POST /v1/runtime/stop", h.lifecycle("stop"))
	mux.HandleFunc("POST /v1/runtime/restart", h.lifecycle("restart"))
	mux.HandleFunc("POST /v1/tokenize", h.tokenize)
	mux.HandleFunc("POST /v1/embeddings", h.embeddings)
	return mux
}

func (h *Host) status(writer http.ResponseWriter, request *http.Request) {
	report, err := h.Controller.Status(request.Context())
	if err != nil {
		h.writeError(writer, err)
		return
	}
	h.enrich(&report)
	h.write(writer, http.StatusOK, responseFor(h, report))
}

func (h *Host) lifecycle(operation string) http.HandlerFunc {
	return func(writer http.ResponseWriter, request *http.Request) {
		var payload lifecycleRequest
		if !h.decode(writer, request, &payload) || !h.valid(writer, payload.ProtocolVersion, payload.RequestID, payload.BundleID) {
			return
		}
		release := h.lease()
		defer release()
		var report daemon.Report
		var err error
		switch operation {
		case "start":
			report, err = h.Controller.Start(request.Context())
		case "stop":
			report, err = h.Controller.Stop(request.Context())
		case "restart":
			report, err = h.Controller.Restart(request.Context())
		}
		if err != nil {
			h.writeError(writer, err)
			return
		}
		h.enrich(&report)
		h.write(writer, http.StatusOK, responseFor(h, report))
		if operation == "stop" {
			go h.finish(false)
		}
	}
}

func (h *Host) tokenize(writer http.ResponseWriter, request *http.Request) {
	var payload tokenizeRequest
	if !h.decode(writer, request, &payload) || !h.valid(writer, payload.ProtocolVersion, payload.RequestID, payload.BundleID) {
		return
	}
	release := h.lease()
	defer release()
	count, err := h.countTokens(request.Context(), payload.Text)
	if err != nil {
		h.writeError(writer, err)
		return
	}
	h.write(writer, http.StatusOK, response{ProtocolVersion: ProtocolVersion, HostBuildID: HostBuildID, BundleID: h.BundleID, RuntimeFingerprint: h.RuntimeFingerprint, TokenCount: count})
}

func (h *Host) embeddings(writer http.ResponseWriter, request *http.Request) {
	var payload embeddingsRequest
	if !h.decode(writer, request, &payload) || !h.valid(writer, payload.ProtocolVersion, payload.RequestID, payload.BundleID) {
		return
	}
	if len(payload.Inputs) == 0 || len(payload.Inputs) > embeddingBatchSize {
		h.write(writer, http.StatusBadRequest, response{ProtocolVersion: ProtocolVersion, HostBuildID: HostBuildID, BundleID: h.BundleID, ErrorCode: contracts.ReasonRuntimeUnavailable})
		return
	}
	release := h.lease()
	defer release()
	embeddings, err := h.embed(request.Context(), payload.Inputs)
	if err != nil {
		h.writeError(writer, err)
		return
	}
	h.write(writer, http.StatusOK, response{ProtocolVersion: ProtocolVersion, HostBuildID: HostBuildID, BundleID: h.BundleID, RuntimeFingerprint: h.RuntimeFingerprint, Embeddings: embeddings})
}

func (h *Host) countTokens(ctx context.Context, text string) (int, error) {
	for attempt := 0; attempt < 2; attempt++ {
		descriptor, key, err := h.credentials()
		if err != nil {
			if attempt == 0 && errors.Is(err, daemon.ErrDescriptorNotFound) {
				if _, startErr := h.Controller.Start(ctx); startErr != nil {
					return 0, startErr
				}
				continue
			}
			return 0, err
		}
		count, err := h.WorkerClient.CountTokens(ctx, descriptor.Endpoint, key, text)
		if err == nil || attempt == 1 || !workerTransportRecoverable(err) || ctx.Err() != nil {
			return count, err
		}
		if _, err := h.Controller.Start(ctx); err != nil {
			return 0, err
		}
	}
	return 0, errors.New("semantic worker recovery exhausted")
}

func (h *Host) embed(ctx context.Context, inputs []string) ([]daemon.Embedding, error) {
	for attempt := 0; attempt < 2; attempt++ {
		descriptor, key, err := h.credentials()
		if err != nil {
			if attempt == 0 && errors.Is(err, daemon.ErrDescriptorNotFound) {
				if _, startErr := h.Controller.Start(ctx); startErr != nil {
					return nil, startErr
				}
				continue
			}
			return nil, err
		}
		embeddings, err := h.WorkerClient.Embed(ctx, descriptor.Endpoint, key, h.BundleID, inputs)
		if err == nil || attempt == 1 || !workerTransportRecoverable(err) || ctx.Err() != nil {
			return embeddings, err
		}
		if _, err := h.Controller.Start(ctx); err != nil {
			return nil, err
		}
	}
	return nil, errors.New("semantic worker recovery exhausted")
}

func workerTransportRecoverable(err error) bool {
	var recoverable interface{ RecoverableTransport() bool }
	return errors.As(err, &recoverable) && recoverable.RecoverableTransport()
}

func (h *Host) credentials() (daemon.Descriptor, string, error) {
	descriptor, err := h.Store.Load()
	if err != nil {
		return daemon.Descriptor{}, "", err
	}
	key, err := h.Store.APIKey()
	return descriptor, key, err
}

func (h *Host) valid(writer http.ResponseWriter, version int, requestID, bundleID string) bool {
	if version != ProtocolVersion {
		h.write(writer, http.StatusConflict, response{ProtocolVersion: ProtocolVersion, HostBuildID: HostBuildID, ErrorCode: contracts.ReasonServiceIncompatible})
		return false
	}
	if requestID == "" || (bundleID != "" && bundleID != h.BundleID) {
		h.write(writer, http.StatusBadRequest, response{ProtocolVersion: ProtocolVersion, HostBuildID: HostBuildID, ErrorCode: contracts.ReasonRuntimeIdentityMismatch})
		return false
	}
	return true
}

func (h *Host) decode(writer http.ResponseWriter, request *http.Request, target any) bool {
	request.Body = http.MaxBytesReader(writer, request.Body, maxBodyBytes)
	decoder := json.NewDecoder(request.Body)
	if err := decoder.Decode(target); err != nil {
		h.write(writer, http.StatusBadRequest, response{ProtocolVersion: ProtocolVersion, HostBuildID: HostBuildID, ErrorCode: contracts.ReasonRuntimeUnavailable})
		return false
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		h.write(writer, http.StatusBadRequest, response{ProtocolVersion: ProtocolVersion, HostBuildID: HostBuildID, ErrorCode: contracts.ReasonRuntimeUnavailable})
		return false
	}
	return true
}

func (h *Host) lease() func() {
	h.mu.Lock()
	h.active++
	if h.timer != nil {
		h.timer.Stop()
	}
	h.mu.Unlock()
	return func() {
		h.mu.Lock()
		h.active--
		h.lastCompleted = time.Now().UTC()
		if h.active == 0 && h.timer != nil {
			h.timer.Reset(h.IdleTimeout)
		}
		h.mu.Unlock()
	}
}

func (h *Host) idle() {
	h.mu.Lock()
	if h.active != 0 {
		h.mu.Unlock()
		return
	}
	h.mu.Unlock()
	h.finish(true)
}

func (h *Host) finish(stopWorker bool) {
	h.shutdownOnce.Do(func() {
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
		if h.server != nil {
			_ = h.server.Shutdown(shutdownCtx)
		}
		shutdownCancel()
		if stopWorker {
			stopCtx, stopCancel := context.WithTimeout(context.Background(), 10*time.Second)
			_, stopErr := h.Controller.Stop(stopCtx)
			stopCancel()
			if stopErr != nil {
				h.mu.Lock()
				h.shutdownErr = stopErr
				h.mu.Unlock()
			}
		}
		if h.done != nil {
			close(h.done)
		}
	})
}

func (h *Host) enrich(report *daemon.Report) {
	h.mu.Lock()
	active := h.active
	last := h.lastCompleted
	h.mu.Unlock()
	report.ServiceRegistrationState = "registered"
	report.ServiceDomain = fmt.Sprintf("gui/%d", os.Getuid())
	report.HostProtocolVersion = ProtocolVersion
	report.HostBuildID = HostBuildID
	report.HostState = "ready"
	report.HostPID = os.Getpid()
	report.HostStartTime = h.startedAt.Format(time.RFC3339Nano)
	report.ActiveBundleID = h.BundleID
	report.ActiveRequests = active
	report.IdleTimeout = h.IdleTimeout.String()
	if !last.IsZero() {
		report.LastRequestCompletedAt = last.Format(time.RFC3339Nano)
		report.IdleDeadline = last.Add(h.IdleTimeout).Format(time.RFC3339Nano)
	}
	if descriptor, err := h.Store.Load(); err == nil {
		report.WorkerPID = descriptor.PID
		report.WorkerStartTime = descriptor.StartTime.Format(time.RFC3339Nano)
		report.WorkerState = descriptor.Readiness
	} else if report.State == daemon.StateStopped {
		report.WorkerState = "idle_stopped"
	}
}

func responseFor(h *Host, report daemon.Report) response {
	return response{ProtocolVersion: ProtocolVersion, HostBuildID: HostBuildID, BundleID: h.BundleID, RuntimeFingerprint: h.RuntimeFingerprint, Report: report}
}

func (h *Host) writeError(writer http.ResponseWriter, err error) {
	code := contracts.ReasonRuntimeUnavailable
	var typed *daemon.Error
	if errors.As(err, &typed) && typed.Code != "" {
		code = typed.Code
	}
	h.write(writer, http.StatusServiceUnavailable, response{ProtocolVersion: ProtocolVersion, HostBuildID: HostBuildID, BundleID: h.BundleID, ErrorCode: code})
}

func (h *Host) write(writer http.ResponseWriter, status int, payload response) {
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(payload)
}
