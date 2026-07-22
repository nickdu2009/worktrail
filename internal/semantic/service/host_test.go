package service

import (
	"context"
	"encoding/json"
	"math"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/nickdu2009/worktrail/internal/semantic/daemon"
)

func TestHostStatusDoesNotStartWorker(t *testing.T) {
	controller := &serviceControllerStub{}
	host := &Host{BundleID: "bundle-a", RuntimeFingerprint: "fingerprint-a", Controller: controller}
	request := httptest.NewRequest(http.MethodGet, "/v1/status", nil)
	responseRecorder := httptest.NewRecorder()
	host.routes().ServeHTTP(responseRecorder, request)
	if responseRecorder.Code != http.StatusOK {
		t.Fatalf("status code = %d", responseRecorder.Code)
	}
	if controller.startCalls != 0 || controller.statusCalls != 1 {
		t.Fatalf("controller calls = status:%d start:%d", controller.statusCalls, controller.startCalls)
	}
	var payload response
	if err := json.Unmarshal(responseRecorder.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.ProtocolVersion != ProtocolVersion || payload.HostBuildID != HostBuildID || payload.BundleID != "bundle-a" {
		t.Fatalf("status payload = %#v", payload)
	}
}

func TestConvertEmbeddingEnforcesShapeFiniteAndNormalization(t *testing.T) {
	valid := make([]float64, embeddingDimension)
	valid[0] = 1
	if _, err := convertEmbedding(valid); err != nil {
		t.Fatalf("convertEmbedding(valid) error = %v", err)
	}
	if _, err := convertEmbedding(valid[:embeddingDimension-1]); err == nil {
		t.Fatal("convertEmbedding(short) error = nil")
	}
	nonFinite := append([]float64(nil), valid...)
	nonFinite[0] = math.NaN()
	if _, err := convertEmbedding(nonFinite); err == nil {
		t.Fatal("convertEmbedding(NaN) error = nil")
	}
	unnormalized := append([]float64(nil), valid...)
	unnormalized[0] = 2
	if _, err := convertEmbedding(unnormalized); err == nil {
		t.Fatal("convertEmbedding(unnormalized) error = nil")
	}
}

func TestHostRetriesWorkerTransportFailureOnlyOnce(t *testing.T) {
	const bundleID = "bundle-a"
	store, err := daemon.NewStore(serviceTestRoots(t), bundleID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.GenerateAPIKey(); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/tokenize" {
			http.NotFound(writer, request)
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"tokens":[1,2]}`))
	}))
	defer server.Close()
	dead := daemon.Descriptor{PID: 1, StartTime: time.Now().UTC(), Endpoint: "http://127.0.0.1:1", Alias: bundleID}
	if err := store.Save(dead); err != nil {
		t.Fatal(err)
	}
	controller := &serviceControllerStub{onStart: func(call int) {
		if call == 1 {
			live := dead
			live.Endpoint = server.URL
			if err := store.Save(live); err != nil {
				t.Fatal(err)
			}
		}
	}}
	workerClient, err := daemon.NewHTTPClient(bundleID)
	if err != nil {
		t.Fatal(err)
	}
	host := &Host{BundleID: bundleID, Controller: controller, Store: store, WorkerClient: workerClient}
	count, err := host.countTokens(context.Background(), "hello")
	if err != nil {
		t.Fatalf("countTokens() error = %v", err)
	}
	if count != 2 || controller.startCalls != 1 {
		t.Fatalf("recovery result = count:%d starts:%d", count, controller.startCalls)
	}
}

func TestHostWarmTokenizeDoesNotStartWorker(t *testing.T) {
	const bundleID = "bundle-a"
	store, err := daemon.NewStore(serviceTestRoots(t), bundleID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.GenerateAPIKey(); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"tokens":[1,2]}`))
	}))
	defer server.Close()
	if err := store.Save(daemon.Descriptor{PID: 1, StartTime: time.Now().UTC(), Endpoint: server.URL, Alias: bundleID}); err != nil {
		t.Fatal(err)
	}
	workerClient, err := daemon.NewHTTPClient(bundleID)
	if err != nil {
		t.Fatal(err)
	}
	controller := &serviceControllerStub{}
	host := &Host{BundleID: bundleID, Controller: controller, Store: store, WorkerClient: workerClient}
	count, err := host.countTokens(context.Background(), "hello")
	if err != nil {
		t.Fatalf("countTokens() error = %v", err)
	}
	if count != 2 || controller.startCalls != 0 {
		t.Fatalf("warm result = count:%d starts:%d", count, controller.startCalls)
	}
}

func TestHostColdTokenizeStartsWorkerOnce(t *testing.T) {
	const bundleID = "bundle-a"
	store, err := daemon.NewStore(serviceTestRoots(t), bundleID)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"tokens":[1]}`))
	}))
	defer server.Close()
	controller := &serviceControllerStub{onStart: func(call int) {
		if _, err := store.GenerateAPIKey(); err != nil {
			t.Fatal(err)
		}
		if err := store.Save(daemon.Descriptor{PID: 1, StartTime: time.Now().UTC(), Endpoint: server.URL, Alias: bundleID}); err != nil {
			t.Fatal(err)
		}
	}}
	workerClient, err := daemon.NewHTTPClient(bundleID)
	if err != nil {
		t.Fatal(err)
	}
	host := &Host{BundleID: bundleID, Controller: controller, Store: store, WorkerClient: workerClient}
	count, err := host.countTokens(context.Background(), "hello")
	if err != nil {
		t.Fatalf("countTokens() error = %v", err)
	}
	if count != 1 || controller.startCalls != 1 {
		t.Fatalf("cold result = count:%d starts:%d", count, controller.startCalls)
	}
}

func TestHostTokenizeProtocolFailureDoesNotStartWorker(t *testing.T) {
	const bundleID = "bundle-a"
	store, err := daemon.NewStore(serviceTestRoots(t), bundleID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.GenerateAPIKey(); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		http.Error(writer, "unavailable", http.StatusInternalServerError)
	}))
	defer server.Close()
	if err := store.Save(daemon.Descriptor{PID: 1, StartTime: time.Now().UTC(), Endpoint: server.URL, Alias: bundleID}); err != nil {
		t.Fatal(err)
	}
	workerClient, err := daemon.NewHTTPClient(bundleID)
	if err != nil {
		t.Fatal(err)
	}
	controller := &serviceControllerStub{}
	host := &Host{BundleID: bundleID, Controller: controller, Store: store, WorkerClient: workerClient}
	if _, err := host.countTokens(context.Background(), "hello"); err == nil {
		t.Fatal("countTokens() error = nil")
	}
	if controller.startCalls != 0 {
		t.Fatalf("start calls = %d, want 0", controller.startCalls)
	}
}

func TestHostWarmEmbeddingDoesNotStartWorker(t *testing.T) {
	const bundleID = "bundle-a"
	store, err := daemon.NewStore(serviceTestRoots(t), bundleID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.GenerateAPIKey(); err != nil {
		t.Fatal(err)
	}
	vector := make([]float64, embeddingDimension)
	vector[0] = 1
	responseBody, err := json.Marshal(map[string]any{
		"data": []map[string]any{{"index": 0, "embedding": vector}},
	})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write(responseBody)
	}))
	defer server.Close()
	if err := store.Save(daemon.Descriptor{PID: 1, StartTime: time.Now().UTC(), Endpoint: server.URL, Alias: bundleID}); err != nil {
		t.Fatal(err)
	}
	workerClient, err := daemon.NewHTTPClient(bundleID)
	if err != nil {
		t.Fatal(err)
	}
	controller := &serviceControllerStub{}
	host := &Host{BundleID: bundleID, Controller: controller, Store: store, WorkerClient: workerClient}
	embeddings, err := host.embed(context.Background(), []string{"hello"})
	if err != nil {
		t.Fatalf("embed() error = %v", err)
	}
	if len(embeddings) != 1 || controller.startCalls != 0 {
		t.Fatalf("warm result = embeddings:%d starts:%d", len(embeddings), controller.startCalls)
	}
}

func TestHostIdleLeaseDefersWorkerStopUntilRequestCompletes(t *testing.T) {
	stopped := make(chan struct{})
	controller := &serviceControllerStub{stopped: stopped}
	host := &Host{Controller: controller, IdleTimeout: 20 * time.Millisecond, done: make(chan struct{})}
	host.timer = time.AfterFunc(host.IdleTimeout, host.idle)
	release := host.lease()
	select {
	case <-stopped:
		t.Fatal("idle timeout stopped worker while request lease was active")
	case <-time.After(40 * time.Millisecond):
	}
	release()
	select {
	case <-stopped:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("worker was not stopped after the final lease completed")
	}
}

func TestRemoveSocketIfSamePreservesReplacementPath(t *testing.T) {
	directory, err := os.MkdirTemp("/tmp", "wt-semantic-socket-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(directory) })
	path := filepath.Join(directory, "semantic.sock")
	listener, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	expected, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("replacement"), 0o600); err != nil {
		t.Fatal(err)
	}
	removeSocketIfSame(path, expected)
	if data, err := os.ReadFile(path); err != nil || string(data) != "replacement" {
		t.Fatalf("replacement path changed: data=%q err=%v", data, err)
	}
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
}

type serviceControllerStub struct {
	statusCalls int
	startCalls  int
	onStart     func(int)
	stopped     chan struct{}
}

func (s *serviceControllerStub) Status(context.Context) (daemon.Report, error) {
	s.statusCalls++
	return daemon.Report{Schema: daemon.ReportSchema, Operation: "status", State: daemon.StateStopped}, nil
}

func (s *serviceControllerStub) Start(context.Context) (daemon.Report, error) {
	s.startCalls++
	if s.onStart != nil {
		s.onStart(s.startCalls)
	}
	return daemon.Report{Schema: daemon.ReportSchema, Operation: "start", State: daemon.StateReady}, nil
}

func (s *serviceControllerStub) Stop(context.Context) (daemon.Report, error) {
	if s.stopped != nil {
		close(s.stopped)
	}
	return daemon.Report{Schema: daemon.ReportSchema, Operation: "stop", State: daemon.StateStopped}, nil
}

func (s *serviceControllerStub) Restart(context.Context) (daemon.Report, error) {
	return daemon.Report{Schema: daemon.ReportSchema, Operation: "restart", State: daemon.StateReady}, nil
}
