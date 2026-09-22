// Command server runs the Capture Trace Room web service.
package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"time"

	"capturetraceroom/internal/server"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:5274", "listen address")
	db := flag.String("db", "capturetraceroom.db", "SQLite database path (use :memory: for ephemeral)")
	flag.Parse()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	srv, err := server.New(ctx, *db)
	if err != nil {
		log.Fatalf("server init: %v", err)
	}
	defer srv.Close()

	log.Printf("捕获轨迹室 listening on http://%s", *addr)
	httpSrv := &http.Server{
		Addr:              *addr,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}
