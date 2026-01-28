package main

import (
	"flag"
	"log"

	"github.com/On-Jun9/ShutterPipe/internal/web"
)

var (
	version = "dev" // set by ldflags during build
)

func main() {
	addr := flag.String("addr", "localhost:8080", "HTTP server address")
	flag.Parse()

	server := web.NewServer()
	server.SetVersion(version)

	if err := server.Start(*addr); err != nil {
		log.Fatal(err)
	}
}
