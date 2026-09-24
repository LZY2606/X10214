// Command server runs the 捕获轨迹室 web service.
package main

import (
	"flag"
	"log"
	"net/http"

	"pairwise-gsb/internal/store"
	"pairwise-gsb/internal/web"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:5274", "listen address")
	dbPath := flag.String("db", "capture-trace.db", "SQLite database path")
	flag.Parse()

	st, err := store.Open(*dbPath)
	if err != nil {
		log.Fatalf("open store: %v", err)
	}
	defer st.Close()

	srv := web.New(st)
	log.Printf("捕获轨迹室 listening on http://%s", *addr)
	log.Fatal(http.ListenAndServe(*addr, srv.Handler()))
}
