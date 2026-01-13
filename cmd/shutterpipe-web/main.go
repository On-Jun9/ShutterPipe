package main

import (
	"flag"
	"log"

	"github.com/On-Jun9/ShutterPipe/internal/web"
)

func main() {
	addr := flag.String("addr", "localhost:8080", "HTTP server address")
	flag.Parse()

	server := web.NewServer()

	if err := server.Start(*addr); err != nil {
		log.Fatal(err)
	}
}
