package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	_ "net/http/pprof"
	"time"

	"GoCall/database"
	"GoCall/server"

	"golang.org/x/net/websocket"
)

var tokenExpires time.Duration = 24 * time.Hour

func main() {
	defaultIP := "127.0.0.1"
	defaultPort := "8080"
	fakeClientCount := 0
	ip := flag.String("ip", defaultIP, "IP address to bind the server to")
	port := flag.String("port", defaultPort, "Port to bind the server to")
	fakeClientNumber := flag.Int("fakeclientn", fakeClientCount, "Number of fake clients")
	flag.Parse()
	address := fmt.Sprintf("%s:%s", *ip, *port)

	certFile := fmt.Sprintf("%s.pem", *ip)
	keyFile := fmt.Sprintf("%s-key.pem", *ip)

	database.InitDatabase()

	ctx, cancel := context.WithCancel(context.Background())

	defer cancel()

	server.InitServer(ctx, fakeClientNumber, true)

	fs := http.FileServer(http.Dir("./static"))
	http.Handle("/", fs)

	http.Handle("/ws", websocket.Handler(server.HandleSFU))

	// http.HandleFunc("/stats", func(w http.ResponseWriter, r *http.Request) {
	// 	statsHandler(w, r, defaultRoom)
	// })

	logger := server.GetLogger()
	logger.Infof("Listening on https://%s ...", address)

	err := http.ListenAndServeTLS(address, certFile, keyFile, nil)
	if err != nil {
		log.Panic(err)
	}
}
