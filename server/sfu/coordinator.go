package sfu

import (
    "encoding/json"
    "fmt"
    "log"
    "sync"

    "github.com/gorilla/websocket"
    "github.com/pion/webrtc/v3"
)

type Lobby interface {
    CreateRoom(id string)
    RemoveRoom(id string)
    AddUserToRoom(selfID string, roomID string, socket *websocket.Conn)
    RemoveUserFromRoom(selfID string, roomID string, socket *websocket.Conn)
}

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

    room.AddPeer(newPeer(selfID))
    fmt.Println("Peer ", selfID, "was added to room ", roomID)
    if peer, ok := room.peers[selfID]; ok {
        // Set socket connection to Peer
        peer.SetSocket(socket)

        // Create Peer Connection
        conn, err := webrtc.NewPeerConnection(webrtc.Configuration{})
        if err != nil {
            fmt.Println("Failed to establish peer connection")
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

        // If PeerConnection is closed remove it from global list
        peer.connection.OnConnectionStateChange(func(p webrtc.PeerConnectionState) {
            switch p {
            case webrtc.PeerConnectionStateFailed:
                if err := peer.connection.Close(); err != nil {
                    log.Print(err)
                }
            case webrtc.PeerConnectionStateClosed:
                room.Signal()
            default:
            }
        })

        // When peer connection is getting ICE -> send ICE to client
        peer.connection.OnICECandidate(func(i *webrtc.ICECandidate) {
            if i == nil {
                fmt.Println("ICEGatheringState: connected")
                return
            }
            fmt.Println("Ice: ", i)
            room.SendICE(i, selfID)
        })

        peer.connection.OnTrack(func(t *webrtc.TrackRemote, _ *webrtc.RTPReceiver) {
            fmt.Println("Track added from peer: ", selfID)
            // defer room.Signal()
            // Create a track to fan out our incoming video to all peers
            trackLocal := room.AddTrack(t)
            defer room.RemoveTrack(trackLocal)
            defer fmt.Println("Track", trackLocal, "was removed")
            buf := make([]byte, 1500)
            for {
                i, _, err := t.Read(buf)
                if err != nil {
                    return
                }

                if _, err = trackLocal.Write(buf[:i]); err != nil {
                    return
                }
            }
        })
    }

}

func (coordinator *Coordinator) RemoveUserFromRoom(selfID string, roomID string) {
    coordinator.mutex.Lock()
    defer coordinator.mutex.Unlock()
    if room, ok := coordinator.sessions[roomID]; ok {
        if _, ok := room.peers[selfID]; ok {
            delete(room.peers, selfID)
        }
    }
}

func (coordinator *Coordinator) ObtainEvent(message WsMessage, socket *websocket.Conn) {
    // В зависимости от поля Event парсим Data в конкретную структуру
    switch message.Event {
    case "joinRoom":
        go func() {
            var join JOIN_ROOM
            if err := json.Unmarshal(message.Data, &join); err != nil {
                fmt.Println("Failed to parse joinRoom data:", err)
                return
            }
            coordinator.AddUserToRoom(join.SelfID, join.RoomID, socket)
        }()
    case "leaveRoom":
        go func() {
            var leave LEAVE_ROOM
            if err := json.Unmarshal(message.Data, &leave); err != nil {
                fmt.Println("Failed to parse leaveRoom data:", err)
                return
            }
            coordinator.RemoveUserFromRoom(leave.SelfID, leave.RoomID)
        }()
    case "offer":
        go func() {
            var offer OFFER
            if err := json.Unmarshal(message.Data, &offer); err != nil {
                fmt.Println("Failed to parse offer data:", err)
                return
            }
            coordinator.mutex.RLock()
            room, okRoom := coordinator.sessions[offer.RoomID]
            coordinator.mutex.RUnlock()
            if !okRoom {
                fmt.Println("Room not found:", offer.RoomID)
                return
            }

            peer, okPeer := room.peers[offer.SelfID]
            if !okPeer {
                fmt.Println("Peer not found:", offer.SelfID)
                return
            }

            answer, err := peer.ReactOnOffer(offer.Offer)
            if err != nil {
                fmt.Println(err)
                return
            }
            room.SendAnswer(answer, offer.SelfID)
        }()
    case "answer":
        go func() {
            var ans ANSWER
            if err := json.Unmarshal(message.Data, &ans); err != nil {
                fmt.Println("Failed to parse answer data:", err)
                return
            }
            coordinator.mutex.RLock()
            room, okRoom := coordinator.sessions[ans.RoomID]
            coordinator.mutex.RUnlock()
            if !okRoom {
                fmt.Println("Room not found:", ans.RoomID)
                return
            }

            peer, okPeer := room.peers[ans.SelfID]
            if !okPeer {
                fmt.Println("Peer not found:", ans.SelfID)
                return
            }

            err := peer.ReactOnAnswer(ans.Answer)
            if err != nil {
                fmt.Println(err)
                return
            }
        }()
    case "ice-candidate":
        go func() {
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

            peer, okPeer := room.peers[candidate.SelfID]
            if !okPeer {
                fmt.Println("Peer not found:", candidate.SelfID)
                return
            }

            if err := peer.connection.AddICECandidate(candidate.Candidate); err != nil {
                log.Println(err)
                return
            }
            fmt.Println("ICE-CANDIDATE added for peer", peer.id)
            fmt.Println(peer.connection.ICEConnectionState())
            fmt.Println(peer.connection.ICEGatheringState())
        }()
    default:
        fmt.Println("DEFAULT")
        fmt.Println(message)
    }
}
