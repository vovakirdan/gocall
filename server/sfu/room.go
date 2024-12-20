package sfu

import (
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/pion/webrtc/v3"
)

type Session interface {
	JoinRoom(id string)
	AddPeer(peer *Peer)
	RemovePeer(peer_id string)
	AddTrack(track *webrtc.TrackRemote) *webrtc.TrackLocalStaticRTP
	RemoveTrack(track *webrtc.TrackLocalStaticRTP)
	SendAnswer(message webrtc.SessionDescription, peer_id string)
	Signal()
}

type Room struct {
	id     string
	mutex  sync.RWMutex
	peers  map[string]*Peer
	tracks map[string]*webrtc.TrackLocalStaticRTP
}

func NewRoom(id string) *Room {
	return &Room{
		id:     id,
		mutex:  sync.RWMutex{},
		peers:  map[string]*Peer{},
		tracks: map[string]*webrtc.TrackLocalStaticRTP{},
	}
}

func (room *Room) AddPeer(peer *Peer) {
	room.mutex.Lock()
	defer room.mutex.Unlock()
	room.peers[peer.id] = peer
}

func (room *Room) RemovePeer(peer_id string) {
	room.mutex.Lock()
	defer func() {
		room.mutex.Unlock()
		room.Signal()
	}()

	delete(room.peers, peer_id)
}

func (room *Room) AddTrack(track *webrtc.TrackRemote) *webrtc.TrackLocalStaticRTP {
	room.mutex.Lock()
	defer func() {
		room.mutex.Unlock()
		room.Signal()
	}()
	trackLocal, err := webrtc.NewTrackLocalStaticRTP(track.Codec().RTPCodecCapability, track.ID(), track.StreamID())
	if err != nil {
		panic(err)
	}

	room.tracks[track.ID()] = trackLocal
	fmt.Println("Track ", track.ID(), " was added")
	return trackLocal
}

func (room *Room) RemoveTrack(track *webrtc.TrackLocalStaticRTP) {
	room.mutex.Lock()
	defer func() {
		room.mutex.Unlock()
		room.Signal()
	}()

	delete(room.tracks, track.ID())
}

func (room *Room) SendAnswer(message webrtc.SessionDescription, peer_id string) {
	room.mutex.RLock()
	peer, ok := room.peers[peer_id]
	room.mutex.RUnlock()
	if !ok || peer.socket == nil {
		fmt.Println("Peer not found or no socket:", peer_id)
		return
	}

	raw, err := json.Marshal(message)
	if err != nil {
		fmt.Println("Failed to marshal answer:", err)
		return
	}

	msg := WsMessage{Event: "answer", Data: json.RawMessage(raw)}
	if err := peer.socket.WriteJSON(msg); err != nil {
		fmt.Println("Failed to send answer:", err)
	}
}

func (room *Room) SendOffer(message webrtc.SessionDescription, peer_id string) {
	room.mutex.RLock()
	peer, ok := room.peers[peer_id]
	room.mutex.RUnlock()
	if !ok || peer.socket == nil {
		fmt.Println("Peer not found or no socket:", peer_id)
		return
	}

	raw, err := json.Marshal(message)
	if err != nil {
		fmt.Println("Failed to marshal offer:", err)
		return
	}

	msg := WsMessage{Event: "offer", Data: json.RawMessage(raw)}
	if err := peer.socket.WriteJSON(msg); err != nil {
		fmt.Println("Failed to send offer:", err)
	}
}

func (room *Room) SendICE(candidate *webrtc.ICECandidate, peer_id string) {
	room.mutex.RLock()
	peer, ok := room.peers[peer_id]
	room.mutex.RUnlock()
	if !ok || peer.socket == nil {
		fmt.Println("Peer not found or no socket:", peer_id)
		return
	}

	iceJSON := candidate.ToJSON()
	raw, err := json.Marshal(iceJSON)
	if err != nil {
		fmt.Println("Failed to marshal ICE candidate:", err)
		return
	}

	fmt.Println("SENDED |ICE|: ", iceJSON)
	msg := WsMessage{Event: "candidate", Data: json.RawMessage(raw)}
	if err := peer.socket.WriteJSON(msg); err != nil {
		fmt.Println("Failed to send ICE candidate:", err)
	}
}

func (room *Room) BroadCast(message WsMessage, self_id string) {
	room.mutex.RLock()
	defer room.mutex.RUnlock()

	for _, rec := range room.peers {
		if rec.id != self_id {
			if err := rec.socket.WriteJSON(message); err != nil {
				fmt.Println(err)
			}
		}
	}
}

func (room *Room) JoinRoom(id string) {
	room.mutex.Lock()
	defer room.mutex.Unlock()
	room.peers[id] = newPeer(id)
}

func (room *Room) Signal() {
	room.mutex.Lock()
	defer room.mutex.Unlock()

	attemptSync := func() (again bool) {
		for _, peer := range room.peers {
			// Если соединение закрыто, удаляем пира и пробуем ещё раз синхронизировать
			if peer.connection.ConnectionState() == webrtc.PeerConnectionStateClosed {
				fmt.Println("Peer with peer_id", peer.id, "was disconnected")
				room.RemovePeer(peer.id)
				return true
			}

			existingSenders := map[string]bool{}
			for _, sender := range peer.connection.GetSenders() {
				if sender.Track() == nil {
					continue
				}
				existingSenders[sender.Track().ID()] = true
				// Если трека больше нет в room.tracks - удаляем sender
				if _, ok := room.tracks[sender.Track().ID()]; !ok {
					if err := peer.connection.RemoveTrack(sender); err == nil {
						fmt.Println("Track", sender.Track().ID(), "was removed")
						return true
					}
				}
			}

			for _, receiver := range peer.connection.GetReceivers() {
				if receiver.Track() == nil {
					continue
				}
				existingSenders[receiver.Track().ID()] = true
			}

			// Добавляем все новые треки, которых нет в данном PeerConnection
			for trackID := range room.tracks {
				if _, ok := existingSenders[trackID]; !ok {
					if _, err := peer.connection.AddTrack(room.tracks[trackID]); err == nil {
						fmt.Println("New track is sending for peer", peer.id)
						return true
					} else {
						fmt.Println(err)
					}
				}
			}

			// Если есть необходимость (например после добавления треков) - создаём новый offer
			// Проверяем, есть ли незавершённый локальный дескриптор
			if peer.connection.PendingLocalDescription() != nil {
				offer, err := peer.connection.CreateOffer(&webrtc.OfferOptions{
					OfferAnswerOptions: webrtc.OfferAnswerOptions{},
					ICERestart:         true,
				})
				if err != nil {
					fmt.Println("Error in CreateOffer: ", err)
					return true
				}
				if err = peer.connection.SetLocalDescription(offer); err != nil {
					fmt.Println("Cannot set LocalDescription: ", err)
					return false
				}

				offerString, err := json.Marshal(offer)
				if err != nil {
					fmt.Println("Marshalling failed: ", err)
					return true
				}

				msg := &WsMessage{
					Event: "offer",
					Data:  json.RawMessage(offerString),
				}
				if err = peer.socket.WriteJSON(msg); err != nil {
					fmt.Println("Cannot write offer message:", err)
					return true
				}
			}
		}
		return false
	}

	for syncAttempt := 0; ; syncAttempt++ {
		if syncAttempt == 25 {
			// Если слишком долго пытаемся синхронизировать - подождём 3 секунды и повторим
			go func() {
				time.Sleep(time.Second * 3)
				room.Signal()
			}()
			return
		}

		if !attemptSync() {
			fmt.Println("Signalling finished")
			break
		}
	}
}
