package main

import (
	"log"
	"net/http"
)

func main() {
	var err error

	mux := http.NewServeMux()
	server := http.Server{
		Handler: mux,
		Addr:    ":8080",
	}

	fileHandler := http.FileServer(http.Dir("."))
	mux.Handle("/", fileHandler)

	err = server.ListenAndServe()
	if err != nil {
		log.Fatal("Error running server: ", err)
	}
}
