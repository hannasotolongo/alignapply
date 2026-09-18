package main

import (
	"log"
	"net/http"
	"time"

	"github.com/hannasotolongo/casemade-backend/internal/api"
)

func main() {
	server := api.NewServer()

	httpServer := &http.Server{
		Addr:              ":8080",
		Handler:           server.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	log.Println("CaseMade API listening on http://localhost:8080")

	if err := httpServer.ListenAndServe(); err != nil &&
		err != http.ErrServerClosed {
		log.Fatal(err)
	}
}
