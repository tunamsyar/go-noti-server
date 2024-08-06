package server

import (
	"fmt"
	"net/http"

	"go-noti-server/internal/log"
)

func RunHttpServer() {
	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {

		w.Header().Set("Content-Type", "text/plain")
		fmt.Fprintf(w, "Server is healthy\n")

		log.InfoLogger.Printf("Server is Running")
	})

	http.ListenAndServe(":8080", nil)
}
