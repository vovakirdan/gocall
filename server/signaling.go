package server

import (
	"log"
	"net/http"

	"github.com/gorilla/websocket"
)

// websocket 
var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

// manage SDP and ICE
func HandleSignal(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println("Error connecting to WebSocket:", err)
		return
	}
	defer conn.Close()

	log.Println("New connection for signal")

	for {
		messageType, message, err := conn.ReadMessage()
		if err != nil {
			log.Println("Error reading message:", err)
		}

		// echo back to client
		if err := conn.WriteMessage(messageType, message); err != nil {
			log.Println("Error sending message:", err)
			break
		}
	}
}