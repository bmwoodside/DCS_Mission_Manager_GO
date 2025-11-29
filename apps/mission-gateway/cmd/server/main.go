package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"time"

	"dcs-mission-manager/mission-gateway/internal/agent"
	"dcs-mission-manager/mission-gateway/internal/config"
	v1 "dcs-mission-manager/mission-gateway/internal/httpapi/v1"
	"dcs-mission-manager/mission-gateway/internal/upload"
	"dcs-mission-manager/mission-gateway/internal/version"
)

func main() {
	cfg := config.FromEnv()
	mux := http.NewServeMux()
	agentMgr := agent.NewManager(cfg.AgentSecret)

	uploadHandler := &upload.Handler{
		Agent:   agentMgr,
		MaxSize: 2 << 30, // e.g. 2 GiB max (need to long-term sample how much is acceptable)
	}

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		fmt.Fprintf(w, "%s %s (%s)\n", version.AppName, version.Version, version.Commit)
		fmt.Fprintln(w, "API available at /api/v1")
	})

	// Static file-serve under /app/ from cfg.PublicDir - e.g. "localhost:8000/app/index.html"
	fs := http.FileServer(http.Dir(cfg.PublicDir))
	mux.Handle("/app/", http.StripPrefix("/app/", fs))

	// API v1 routes
	mux.HandleFunc("/api/v1/health", v1.HealthHandler)
	mux.HandleFunc("/api/v1/agent/health", v1.NewAgentStatusHandler(agentMgr))
	mux.Handle("/api/v1/upload", uploadHandler)

	mux.Handle("/agent/ws", agentMgr)

	srv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           logRequests(mux),
		ReadHeaderTimeout: 10 * time.Second,
	}

	//serve as goroutine so i can kill it easier >.>
	go func() {
		log.Printf("listening on %s", cfg.Addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("server error: %v", err)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt)
	<-stop
	log.Println("shutting down...")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		log.Printf("graceful shutdown failed: %v", err)
	} else {
		log.Println("shutdown complete")
	}
}

func logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		log.Printf("%s %s (%s)", r.Method, r.URL.Path, time.Since(start))
	})
}
