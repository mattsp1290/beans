package main

import (
	"log"
	"os"

	"github.com/mattsp1290/bean-counter/internal/server"
)

func main() {
	addr := os.Getenv("BN_ADDR")
	if addr == "" {
		addr = ":8080"
	}

	app := server.New(server.Config{
		CORSOrigin: os.Getenv("BN_CORS_ORIGIN"),
	})
	log.Printf("bean-counter listening on %s", addr)
	if err := app.Listen(addr); err != nil {
		log.Fatal(err)
	}
}
