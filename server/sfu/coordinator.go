package sfu

import (
    "encoding/json"
    "fmt"
    "log"
    "sync"

    "github.com/gorilla/websocket"
    "github.com/pion/webrtc/v3"
)

type Coordinator struct {
    mutex    sync.RWMutex
    sessions map[string]*Room
}

func NewCoordinator() *Coordinator {
    return &Coordinator{sessions: map[string]*Room{}}
}

func (coordinator *Coordinator) ShowSessions() map[string]*Room {
    coordinator.mutex.RLock()
    defer coordinator.mutex.RUnlock()
    return coordinator.sessions
}

func (coordinator *Coordinator) CreateRoom(id string) {
    coordinator.mutex.Lock()
    defer coordinator.mutex.Unlock()
    coordinator.sessions[id] = NewRoom(id)
}

func (coordinator *Coordinator) RemoveRoom(id string) {
    coordinator.mutex.Lock()
    defer coordinator.mutex.Unlock()
    delete(coordinator.sessions, id)
}

func (coordinator *Coordinator) AddUserToRoom(selfID string, roomID string, socket *websocket.Conn) {
    coordinator.mutex.Lock()
    if _, ok := coordinator.sessions[roomID]; !ok {
        fmt.Println("New Room was created: ", roomID)
        coordinator.sessions[roomID] = NewRoom(roomID)
    }
    room := coordinator.sessions[roomID]
    coordinator.mutex.Unlock()

    peer := newPeer(selfID)
    room.AddPeer(peer)
    fmt.Println("Peer ", selfID, "was added to room ", roomID)

    // Set socket connection to Peer
    peer.SetSocket(socket)

    cfg := webrtc.Configuration{
        ICEServers: []webrtc.ICEServer{
            {URLs: []string{"stun:stun.l.google.com:19302"}},
        },
    }

    // Create Peer Connection
    conn, err := webrtc.NewPeerConnection(cfg)
    if err != nil {
        fmt.Println("Failed to establish peer connection")
        return
    }

    peer.SetPeerConnection(conn)
    fmt.Println("Peer connection was established")

    // Accept one audio and one video track incoming
    for _, typ := range []webrtc.RTPCodecType{webrtc.RTPCodecTypeVideo, webrtc.RTPCodecTypeAudio} {
        if _, err := peer.connection.AddTransceiverFromKind(typ, webrtc.RTPTransceiverInit{
            Direction: webrtc.RTPTransceiverDirectionRecvonly,
        }); err != nil {
            log.Print(err)
            return
        }
    }

    // If PeerConnection is closed remove it from room
    peer.connection.OnConnectionStateChange(func(p webrtc.PeerConnectionState) {
        switch p {
        case webrtc.PeerConnectionStateConnected:
            fmt.Printf("Peer %s connected successfully\n", selfID)
        case webrtc.PeerConnectionStateFailed, webrtc.PeerConnectionStateDisconnected, webrtc.PeerConnectionStateClosed:
            fmt.Printf("Peer %s disconnected\n", selfID)
            room.RemovePeer(selfID)
        }
    })    

    // When PeerConnection gets ICE candidates, send them to the client
    peer.connection.OnICECandidate(func(i *webrtc.ICECandidate) {
        if i == nil {
            fmt.Println("ICEGatheringState: connected")
            return
        }
        fmt.Println("Ice: ", i)
        room.SendICE(i, selfID)
    })

    // When a remote track is received, add it to the room
    peer.connection.OnTrack(func(t *webrtc.TrackRemote, _ *webrtc.RTPReceiver) {
        fmt.Println("Track added from peer:", selfID)
        trackLocal := room.AddTrack(t)
        go room.Signal()
        defer room.RemoveTrack(trackLocal)
    
        // Relay incoming packets to all other peers in the room
        buf := make([]byte, 1500)
        for {
            i, _, err := t.Read(buf)
            if err != nil {
                fmt.Printf("Error reading track from peer %s: %v\n", selfID, err)
                return
            }
            room.BroadcastRTP(trackLocal, buf[:i])
        }
    })    
}

func (coordinator *Coordinator) RemoveUserFromRoom(selfID string, roomID string) {
    coordinator.mutex.RLock()
    room, ok := coordinator.sessions[roomID]
    coordinator.mutex.RUnlock()
    if ok {
        room.RemovePeer(selfID)
    }
}

func (coordinator *Coordinator) ObtainEvent(message WsMessage, socket *websocket.Conn) {
    switch message.Event {
    case "joinRoom":
        var join JOIN_ROOM
        if err := json.Unmarshal(message.Data, &join); err != nil {
            fmt.Println("Failed to parse joinRoom data:", err)
            return
        }
        coordinator.AddUserToRoom(join.SelfID, join.RoomID, socket)
    case "leaveRoom":
        var leave LEAVE_ROOM
        if err := json.Unmarshal(message.Data, &leave); err != nil {
            fmt.Println("Failed to parse leaveRoom data:", err)
            return
        }
        coordinator.RemoveUserFromRoom(leave.SelfID, leave.RoomID)
    case "offer":
        var offerMsg OFFER
        if err := json.Unmarshal(message.Data, &offerMsg); err != nil {
            fmt.Println("Failed to parse offer data:", err)
            return
        }
    
        coordinator.mutex.RLock()
        room, okRoom := coordinator.sessions[offerMsg.RoomID]
        coordinator.mutex.RUnlock()
        if !okRoom {
            fmt.Println("Room not found:", offerMsg.RoomID)
            return
        }
    
        // Находим Peer, который отправил offer.
        room.mutex.RLock()
        peer, okPeer := room.peers[offerMsg.SelfID]
        room.mutex.RUnlock()
        if !okPeer {
            fmt.Println("Peer not found:", offerMsg.SelfID)
            return
        }
    
        // Вызываем peer.ReactOnOffer, чтобы:
        //   1) Установить remoteDescription
        //   2) Создать answer
        //   3) Установить localDescription
        localAnswer, err := peer.ReactOnOffer(offerMsg.Offer)
        if err != nil {
            fmt.Println("Failed to react on offer:", err)
            return
        }
    
        // Шлём answer обратно тому же клиенту
        room.SendAnswer(localAnswer, offerMsg.SelfID)    
    // case "answer":
    //     var ans ANSWER
    //     if err := json.Unmarshal(message.Data, &ans); err != nil {
    //         fmt.Println("Failed to parse answer data:", err)
    //         return
    //     }
    //     coordinator.mutex.RLock()
    //     room, okRoom := coordinator.sessions[ans.RoomID]
    //     coordinator.mutex.RUnlock()
    //     if !okRoom {
    //         fmt.Println("Room not found:", ans.RoomID)
    //         return
    //     }

    //     // Отправляем answer обратно отправителю offer
    //     room.SendAnswer(ans.Answer, ans.SelfID)
    case "ice-candidate":
        var candidate CANDIDATE
        if err := json.Unmarshal(message.Data, &candidate); err != nil {
            fmt.Println("Failed to parse candidate data:", err)
            return
        }
        coordinator.mutex.RLock()
        room, okRoom := coordinator.sessions[candidate.RoomID]
        coordinator.mutex.RUnlock()
        if !okRoom {
            fmt.Println("Room not found:", candidate.RoomID)
            return
        }

        // Отправляем ICECandidateInit всем пирами кроме отправителя
        room.mutex.RLock()
        for _, peer := range room.peers {
            if peer.id != candidate.SelfID {
                sendCandidate := WsMessage{
                    Event: "candidate",
                    Data:  json.RawMessage(toJSONStringIce(candidate.Candidate)),
                }
                if err := peer.WriteJSON(sendCandidate); err != nil {
                    fmt.Println("Failed to send ICE candidate to peer:", peer.id, ":", err)
                }
            }
        }
        room.mutex.RUnlock()
    default:
        fmt.Println("Unknown event:", message.Event)
    }
}

// Helper function to convert SessionDescription to JSON string
func toJSONString(sdp webrtc.SessionDescription) string {
    b, err := json.Marshal(sdp)
    if err != nil {
        fmt.Println("Error marshalling SessionDescription:", err)
        return "{}"
    }
    return string(b)
}

// Helper function to convert ICECandidateInit to JSON string
func toJSONStringIce(c webrtc.ICECandidateInit) string {
    b, err := json.Marshal(c)
    if err != nil {
        fmt.Println("Error marshalling ICECandidateInit:", err)
        return "{}"
    }
    return string(b)
}