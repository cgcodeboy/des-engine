package metrics

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/cgcodeboy/des-engine/engine"
	"github.com/cgcodeboy/des-engine/internal/asynclog"
)

// Source supplies a consistent engine snapshot for each request.
type Source interface {
	Snapshot() engine.Snapshot
}

// Listen serves GET /healthz, /status, and /metrics until ctx is canceled.
// A nil logger uses slog.Default. Log records are queued and written
// asynchronously. Canceling ctx stops this server even if the engine is
// still running.
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
	router := gin.New()
	router.GET("/healthz", func(c *gin.Context) {
		c.Data(http.StatusOK, "text/plain; charset=utf-8", []byte("ok"))
	})
	router.GET("/status", func(c *gin.Context) {
		c.Header("Content-Type", "application/json")
		_ = json.NewEncoder(c.Writer).Encode(toStatus(src.Snapshot()))
	})
	router.GET("/metrics", func(c *gin.Context) {
		c.Data(http.StatusOK, "text/plain; version=0.0.4; charset=utf-8", []byte(prometheus(src.Snapshot())))
	})
	return router
}
