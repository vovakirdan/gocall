package main

import (
	"log"
	"net/http"

	"../../server/signaling"
) 

func main() {
	http.HandleFunc("/signal", signaling.HandleSignal)

	fs := http.FileServer(http.Dir("./static"))
	http.Handle("/", fs)

	log.Println("Server started on :8080")
	err := http.ListenAndServe(":8080", nil)
	if err != nil {
		log.Fatalf("Server error %v", err)
	}
}