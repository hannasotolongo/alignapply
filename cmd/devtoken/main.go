package main

import (
	"fmt"
	"log"
	"os"

	"github.com/hannasotolongo/casemade-backend/internal/auth"
)

func main() {
	if len(os.Args) != 2 {
		log.Fatal("usage: go run ./cmd/devtoken <user-id>")
	}

	manager, err := auth.NewSessionManager(
		os.Getenv("SESSION_SECRET"),
	)
	if err != nil {
		log.Fatal(err)
	}

	token, err := manager.Create(os.Args[1])
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println(token)
}
