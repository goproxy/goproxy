package main

import (
	"flag"
	"log"
	"net/http"

	"github.com/goproxy/goproxy"
)

func main() {
	var listenAddr string
	flag.StringVar(&listenAddr, "a", "0.0.0.0:8080", "listening address")
	flag.Parse()

	log.Printf("listening on: %s\n", listenAddr)
	log.Fatal(http.ListenAndServe(listenAddr, goproxy.New()))
}
