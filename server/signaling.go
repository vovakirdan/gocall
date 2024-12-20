package server

import (
	"log"
	"net/http"
	"encoding/json"
	"sync"

	"github.com/gorilla/websocket"
)

type Message struct {
	Type string `json:"type"`  // message type: "join", "offer", "answer", "ice"
	RoomID string `json:"roomId"`  // room identifier
	Payload json.RawMessage `json:"payload"`  // message body (SDP, ICE etc)
}

var (
	// store rooms: each room has a list of connections
	rooms = make(map[string]map[*websocket.Conn]bool)  // Rooms: roomId -> set of connections
	roomsMu sync.Mutex
	
	// connRooms maps a connection to its room ID (if any)
	connRooms   = make(map[*websocket.Conn]string)  // connRooms: connection -> roomId
	connRoomsMu sync.Mutex
)

// websocket 
var signalUpgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

// HandleSignal handles signaling WebSocket connections
func HandleSignal(w http.ResponseWriter, r *http.Request) {
	conn, err := signalUpgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println("Error upgrading HTTP to WebSocket:", err)
		return
	}

	defer func() {
		// On connection close, remove it from its room if it has one
		connRoomsMu.Lock()
		if roomId, ok := connRooms[conn]; ok && roomId != "" {
			removeConnectionFromRoom(roomId, conn)
			delete(connRooms, conn)
		}
		connRoomsMu.Unlock()

		conn.Close()
	}()

	log.Println("New connection for signal")

	for {
		_, message, err := conn.ReadMessage()
		if err != nil {
			log.Println("Error reading message:", err)
			break
		}

		var msg Message
		if err := json.Unmarshal(message, &msg); err != nil {
			log.Println("Invalid message format:", err)
			continue
		}

		switch msg.Type {
		case "join":
			// client asking for connection
			handleJoin(conn, msg)
		case "offer", "answer", "ice":
			// this messages sends between clients
			handleRelayMessage(conn, msg)
		default:
			log.Println("Invalid message type:", msg.Type)
		}
	}
}

func handleJoin(conn *websocket.Conn, msg Message) {
	roomId := msg.RoomID
	if roomId == "" {
		log.Println("No room provided in join message")
		return
	}
	
	// add connection in room
	addConnectionToRoom(roomId, conn)

	// Remember which room this connection belongs to
	connRoomsMu.Lock()
	connRooms[conn] = roomId
	connRoomsMu.Unlock()

	log.Printf("Connection joined room: %s", roomId)
}

// send messages to other room members
func handleRelayMessage(conn *websocket.Conn, msg Message) {
	roomId := msg.RoomID
	if roomId == "" {
		log.Println("No room provided in relay message")
		return
	}

	roomsMu.Lock()
	connections, exists := rooms[roomId]
	roomsMu.Unlock()
	if !exists {
		log.Println("Room not found:", roomId)
		return
	}

	// send message to all connections in room
	for c := range connections {
		if c != conn {
			err := c.WriteJSON(msg)
			if err != nil {
				log.Println("Error sending message to peer:", err)
			}
		}
	}
}

// adding connection to room
func addConnectionToRoom(roomId string, conn *websocket.Conn) {
	roomsMu.Lock()
	defer roomsMu.Unlock()

	if _, ok := rooms[roomId]; !ok {
		rooms[roomId] = make(map[*websocket.Conn]bool)
	}
	rooms[roomId][conn] = true
}

// removing connection from room
func removeConnectionFromRoom(roomId string, conn *websocket.Conn) {
	roomsMu.Lock()
	defer roomsMu.Unlock()

	if connections, ok := rooms[roomId]; ok {
		delete(rooms[roomId], conn)
		if len(connections) == 0 {
			delete(rooms, roomId)
		}
	}
}