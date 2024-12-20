package sfu

import (
    "fmt"
    "sync"

    "github.com/gorilla/websocket"
    "github.com/pion/webrtc/v3"
)

type PeerInterface interface {
    SetSocket(ws_conn *websocket.Conn)
    AddRemoteTrack(track *webrtc.TrackRemote)
    RemoveRemoteTrack(track *webrtc.TrackRemote)
    SetPeerConnection(conn *webrtc.PeerConnection)
    ReactOnOffer(offer webrtc.SessionDescription) (webrtc.SessionDescription, error)
    ReactOnAnswer(answer webrtc.SessionDescription) error
    WriteJSON(v interface{}) error
}

type Peer struct {
    id         string
    connection *webrtc.PeerConnection
    streams    map[string]*webrtc.TrackRemote
    socket     *websocket.Conn
    mutex      sync.RWMutex
    writeMutex sync.Mutex // Для записи в socket
}

func newPeer(id string) *Peer {
    return &Peer{
        id:      id,
        streams: make(map[string]*webrtc.TrackRemote),
    }
}

func (peer *Peer) SetPeerConnection(conn *webrtc.PeerConnection) {
    peer.mutex.Lock()
    defer peer.mutex.Unlock()
    peer.connection = conn
}

func (peer *Peer) AddRemoteTrack(track *webrtc.TrackRemote) {
    peer.mutex.Lock()
    defer peer.mutex.Unlock()
    peer.streams[track.ID()] = track
}

func (peer *Peer) RemoveRemoteTrack(track *webrtc.TrackRemote) {
    peer.mutex.Lock()
    defer peer.mutex.Unlock()
    delete(peer.streams, track.ID())
}

func (peer *Peer) SetSocket(socket *websocket.Conn) {
    peer.mutex.Lock()
    defer peer.mutex.Unlock()
    peer.socket = socket
}

func (peer *Peer) ReactOnOffer(offer webrtc.SessionDescription) (webrtc.SessionDescription, error) {
    peer.mutex.Lock()
    defer peer.mutex.Unlock()

    if err := peer.connection.SetRemoteDescription(offer); err != nil {
        fmt.Println("Failed to set remote description for peer", peer.id, ":", err)
        return webrtc.SessionDescription{}, err
    }
    fmt.Println("Remote Description was set for peer", peer.id)

    answer, err := peer.connection.CreateAnswer(nil)
    if err != nil {
        fmt.Println("Failed to create answer for peer", peer.id, ":", err)
        return webrtc.SessionDescription{}, err
    }

    if err = peer.connection.SetLocalDescription(answer); err != nil {
        fmt.Println("Failed to set local description for peer", peer.id, ":", err)
        return webrtc.SessionDescription{}, err
    }
    fmt.Println("Local Description was set for peer", peer.id)
    fmt.Println("Answer was created in peer", peer.id)

    return *peer.connection.LocalDescription(), nil
}

func (peer *Peer) ReactOnAnswer(answer webrtc.SessionDescription) error {
    peer.mutex.Lock()
    defer peer.mutex.Unlock()

    if err := peer.connection.SetRemoteDescription(answer); err != nil {
        fmt.Println("Failed to set remote description (answer) for peer", peer.id, ":", err)
        return err
    }
    fmt.Println("Remote Description (answer) was set for peer", peer.id)
    return nil
}

func (peer *Peer) WriteJSON(v interface{}) error {
    peer.writeMutex.Lock()
    defer peer.writeMutex.Unlock()

    if peer.socket == nil {
        return fmt.Errorf("no socket for peer %s", peer.id)
    }

    return peer.socket.WriteJSON(v)
}
