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

	err = server.ListenAndServe()
	if err != nil {
		log.Fatal("Error running server: ", err)
	}
}
