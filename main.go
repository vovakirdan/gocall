package main

import (
	"log"
	"net/http"
	"flag"
	"fmt"

	"GoCall/server"
)

func main() {
	defaultIP := "127.0.0.1"
	defaultPort := "8080"

	ip := flag.String("ip", defaultIP, "IP address to bind the server to")
	port := flag.String("port", defaultPort, "Port to bind the server to")
	flag.Parse()
	address := fmt.Sprintf("%s:%s", *ip, *port)

	certFile := fmt.Sprintf("%s.pem", *ip)
	keyFile := fmt.Sprintf("%s-key.pem", *ip)
	// init WebRTC
	server.InitWebRTC()

	// routes
	http.HandleFunc("/signal", server.HandleSignal)
	http.HandleFunc("/webrtc", server.HandleWebRTC)

	// Static
	fs := http.FileServer(http.Dir("./static"))
	http.Handle("/", fs)

	log.Printf("Server started on %s\n", address)
	err := http.ListenAndServeTLS(address, certFile, keyFile, nil)
	if err != nil {
		log.Fatalf("Error starting server: %v", err)
	}
}
