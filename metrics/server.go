package metrics

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/cgcodeboy/des-engine/engine"
	"github.com/cgcodeboy/des-engine/internal/asynclog"
)

// Source is an engine snapshot provider.
type Source interface {
	Snapshot() engine.Snapshot
}

// Listen serves /healthz, /status, and /metrics until ctx is canceled.
// A nil logger uses slog.Default. The logger is always written asynchronously.
func Listen(ctx context.Context, addr string, src Source, logger *slog.Logger) error {
	if ctx == nil {
		return errors.New("nil context")
	}
	var sink slog.Handler
	if logger != nil {
		sink = logger.Handler()
	}
	logs := asynclog.New(sink)
	log := slog.New(logs.Handler())
	defer logs.Close()

	ln, err := net.Listen("tcp", addr)
	if err != nil {
		log.Error("metrics listen failed", slog.String("error", err.Error()))
		return err
	}
	srv := &http.Server{Handler: newMux(src)}
	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.Serve(ln)
	}()
	log.Info("metrics listening", slog.String("addr", ln.Addr().String()))

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
		<-errCh
		log.Info("metrics stopped", slog.String("reason", "context canceled"))
		return nil
	case err := <-errCh:
		if err == http.ErrServerClosed {
			log.Info("metrics stopped", slog.String("reason", "closed"))
			return nil
		}
		log.Error("metrics stopped", slog.String("reason", err.Error()))
		return err
	}
}

func newMux(src Source) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte("ok"))
	})
	mux.HandleFunc("GET /status", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		enc := json.NewEncoder(w)
		_ = enc.Encode(toStatus(src.Snapshot()))
	})
	mux.HandleFunc("GET /metrics", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		_, _ = w.Write([]byte(prometheus(src.Snapshot())))
	})
	return mux
}
