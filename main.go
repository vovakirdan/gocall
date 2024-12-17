package main

import (
	"log"
	"net/http"

	"GoCall/server"
)

func main() {
	// init WebRTC
	server.InitWebRTC()

	// routes
	http.HandleFunc("/signal", server.HandleSignal)
	http.HandleFunc("/webrtc", server.HandleWebRTC)

	// Static
	fs := http.FileServer(http.Dir("./static"))
	http.Handle("/", fs)

	log.Println("Server started on :8080")
	err := http.ListenAndServe(":8080", nil)
	if err != nil {
		log.Fatalf("Error starting server: %v", err)
	}
}
