package sfu

import (
    "encoding/json"

    "github.com/pion/webrtc/v3"
)

type JOIN_ROOM struct {
    SelfID string `json:"self_id"`
    RoomID string `json:"room_id"`
}

type LEAVE_ROOM struct {
    SelfID string `json:"self_id"`
    RoomID string `json:"room_id"`
}

type OFFER struct {
    SelfID string                     `json:"self_id"`
    RoomID string                     `json:"room_id"`
    Offer  webrtc.SessionDescription  `json:"offer"`
}

type ANSWER struct {
    SelfID string                    `json:"self_id"`
    RoomID string                    `json:"room_id"`
    Answer webrtc.SessionDescription `json:"answer"`
}

type CANDIDATE struct {
    SelfID    string               `json:"self_id"`
    RoomID    string               `json:"room_id"`
    Candidate webrtc.ICECandidateInit `json:"candidate"`
}

// WsMessage представляет общее сообщение из WebSocket.
// В зависимости от поля Event, Data можно распарсить в один из типов выше.
type WsMessage struct {
    Event string          `json:"event"`
    Data  json.RawMessage `json:"data"`
}
