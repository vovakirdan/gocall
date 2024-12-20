package server

import (
    "encoding/json"
    "fmt"
    "net/http"
    "sync"

    "github.com/gorilla/websocket"
    "GoCall/server/sfu"
)

type WsServer struct {
    mutex       sync.RWMutex
    clients     map[*websocket.Conn]bool
    coordinator *sfu.Coordinator
}

var wsUpgrader = websocket.Upgrader{
    CheckOrigin: func(r *http.Request) bool {
        return true
    },
    ReadBufferSize:  1024,
    WriteBufferSize: 1024,
}

func InitWsServer() *WsServer {
    wsServer := &WsServer{
        clients:     make(map[*websocket.Conn]bool),
        coordinator: sfu.NewCoordinator(),
    }

    http.HandleFunc("/ws", wsServer.wsInit)
    return wsServer
}

func (ws *WsServer) wsInit(w http.ResponseWriter, r *http.Request) {
    conn, err := wsUpgrader.Upgrade(w, r, nil)
    if err != nil {
        fmt.Printf("Upgrade error: %s\n", err)
        return
    }

    ws.mutex.Lock()
    ws.clients[conn] = true
    ws.mutex.Unlock()

    fmt.Println("Client connected successfully")

    for {
        messageType, bmessage, err := conn.ReadMessage()
        if err != nil {
            fmt.Println("Read error:", err)
            ws.mutex.Lock()
            delete(ws.clients, conn)
            ws.mutex.Unlock()

            conn.Close()
            return
        }

        if messageType == websocket.CloseMessage {
            ws.mutex.Lock()
            delete(ws.clients, conn)
            ws.mutex.Unlock()

            conn.Close()
            break
        }

        var message sfu.WsMessage
        if err := json.Unmarshal(bmessage, &message); err != nil {
            fmt.Println("Failed to unmarshal incoming message:", err)
            continue
        }

        // Передаём событие в coordinator для обработки
        ws.coordinator.ObtainEvent(message, conn)
    }
}
