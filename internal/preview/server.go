package preview

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"
)

func Serve(ctx context.Context, dir string) (ServeResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return ServeResult{}, err
	}

	server := &http.Server{Handler: http.FileServer(http.Dir(dir))}
	var once sync.Once
	var stopErr error
	stop := func() error {
		once.Do(func() {
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			stopErr = server.Shutdown(shutdownCtx)
			if errors.Is(stopErr, http.ErrServerClosed) {
				stopErr = nil
			}
		})
		return stopErr
	}

	errCh := make(chan error, 1)
	go func() {
		if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
			return
		}
		errCh <- nil
	}()
	go func() {
		<-ctx.Done()
		_ = stop()
	}()

	select {
	case err := <-errCh:
		if err != nil {
			return ServeResult{}, err
		}
	default:
	}

	return ServeResult{
		URL:  fmt.Sprintf("http://%s/", listener.Addr().String()),
		Stop: stop,
	}, nil
}
