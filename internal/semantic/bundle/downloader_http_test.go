package bundle

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestHTTPDownloaderDownloadSuccess(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		_, _ = writer.Write([]byte("bundle bytes"))
	}))
	t.Cleanup(server.Close)

	var destination bytes.Buffer
	downloader := HTTPDownloader{Client: server.Client()}
	if err := downloader.Download(context.Background(), server.URL+"/artifact", &destination); err != nil {
		t.Fatalf("Download() error = %v", err)
	}
	if got, want := destination.String(), "bundle bytes"; got != want {
		t.Fatalf("downloaded bytes = %q, want %q", got, want)
	}
}

func TestHTTPDownloaderRejectsHTTPAndCredentialURLs(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requests.Add(1)
		writer.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(server.Close)

	tests := []struct {
		name string
		url  string
	}{
		{name: "HTTP", url: "http://example.test/artifact"},
		{name: "credentials", url: "https://user:secret@example.test/artifact"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			downloader := HTTPDownloader{Client: server.Client()}
			err := downloader.Download(context.Background(), test.url, io.Discard)
			if err == nil {
				t.Fatal("Download() error = nil, want rejected URL")
			}
		})
	}
	if got := requests.Load(); got != 0 {
		t.Fatalf("received %d HTTP requests for rejected URLs", got)
	}
}

func TestHTTPDownloaderRejectsNon2xxResponse(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(server.Close)

	err := (HTTPDownloader{Client: server.Client()}).Download(context.Background(), server.URL+"/missing", io.Discard)
	if err == nil {
		t.Fatal("Download() error = nil, want HTTP status error")
	}
}

func TestHTTPDownloaderEnforcesRedirectLimit(t *testing.T) {
	var finalRequests atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/start":
			http.Redirect(writer, request, "/middle", http.StatusFound)
		case "/middle":
			http.Redirect(writer, request, "/final", http.StatusFound)
		case "/final":
			finalRequests.Add(1)
			_, _ = writer.Write([]byte("bundle bytes"))
		default:
			writer.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)

	err := (HTTPDownloader{Client: server.Client(), MaxRedirects: 1}).Download(context.Background(), server.URL+"/start", io.Discard)
	if err == nil {
		t.Fatal("Download() error = nil, want redirect limit error")
	}
	if got := finalRequests.Load(); got != 0 {
		t.Fatalf("final URL received %d requests, want 0", got)
	}
}

func TestHTTPDownloaderPropagatesContextCancellation(t *testing.T) {
	headersSent := make(chan struct{})
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.WriteHeader(http.StatusOK)
		writer.(http.Flusher).Flush()
		close(headersSent)
		<-request.Context().Done()
	}))
	t.Cleanup(server.Close)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	errs := make(chan error, 1)
	go func() {
		errs <- (HTTPDownloader{Client: server.Client()}).Download(ctx, server.URL+"/slow", io.Discard)
	}()

	<-headersSent
	cancel()
	if err := <-errs; !errors.Is(err, context.Canceled) {
		t.Fatalf("Download() error = %v, want context.Canceled", err)
	}
}

func TestHTTPDownloaderPropagatesWriterError(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		_, _ = writer.Write([]byte("bundle bytes"))
	}))
	t.Cleanup(server.Close)

	want := errors.New("write failed")
	err := (HTTPDownloader{Client: server.Client()}).Download(context.Background(), server.URL+"/artifact", errorWriter{err: want})
	if !errors.Is(err, want) {
		t.Fatalf("Download() error = %v, want writer error %v", err, want)
	}
}

type errorWriter struct {
	err error
}

func (w errorWriter) Write([]byte) (int, error) {
	return 0, w.err
}
