package observability

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"time"
)

// StatusFunc returns the latest immutable status.
type StatusFunc func() Status

// ServeStatus starts an optional read-only HTTP endpoint. It blocks until the
// context is cancelled or the listener fails. It exposes no credentials.
func ServeStatus(ctx context.Context, addr string, status StatusFunc, log *slog.Logger) error {
	if log == nil {
		log = slog.Default()
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		st := status()
		code, text := health(st)
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(code)
		if r.Method != http.MethodHead {
			_, _ = w.Write([]byte(text + "\n"))
		}
	})
	mux.HandleFunc("/status", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", "GET")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		_ = enc.Encode(status())
	})
	srv := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	log.Info("servidor de status ativo", "endereco", ln.Addr().String(), "endpoints", "/healthz,/status")
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()
	if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func health(st Status) (int, string) {
	if st.ShuttingDown {
		return http.StatusServiceUnavailable, "shutting down"
	}
	if st.Paused {
		return http.StatusOK, "paused"
	}
	interval := st.Poll.Interval
	if interval <= 0 {
		interval = 2 * time.Minute
	}
	if st.Poll.LastSuccess.IsZero() {
		if time.Since(st.StartedAt) < 3*interval {
			return http.StatusOK, "starting"
		}
		return http.StatusServiceUnavailable, "no successful collection"
	}
	maxAge := 3 * interval
	if maxAge < 5*time.Minute {
		maxAge = 5 * time.Minute
	}
	if time.Since(st.Poll.LastSuccess) > maxAge {
		return http.StatusServiceUnavailable, "stale collection"
	}
	return http.StatusOK, "ok"
}
