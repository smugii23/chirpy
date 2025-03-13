package main

import (
	"net/http"
)

func main() {
	mux := http.NewServeMux()
	server := &http.Server{
		Handler: mux,
		Addr:    "localhost:8080",
	}
	fileServer := http.FileServer(http.Dir("."))
	mux.Handle("/", fileServer)
	server.ListenAndServe()
}
