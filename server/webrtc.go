package server

import (
	"encoding/json"
	"log"
	"net/http"

	"github.com/pion/webrtc/v3"
)

// WebRTC-connections
var peerConnection *webrtc.PeerConnection

// InitWebRTC init WebRTC connection
func InitWebRTC() {
	iceServers := webrtc.Configuration{
		ICEServers: []webrtc.ICEServer{
			{
				URLs: []string{"stun:stun.l.google.com:19302"}, // public STUN-stun
			},
		},
	}

	var err error
	peerConnection, err = webrtc.NewPeerConnection(iceServers)
	if err != nil {
		log.Fatalf("Error creating PeerConnection: %v", err)
	}

	peerConnection.OnICECandidate(func(candidate *webrtc.ICECandidate) {
		if candidate != nil {
			log.Println("ICE Candidate found:", candidate.ToJSON())
		}
	})

	log.Println("WebRTC inited")
}

// HandleWebRTC managing SDP
func HandleWebRTC(w http.ResponseWriter, r *http.Request) {
	var sdp webrtc.SessionDescription

	if err := json.NewDecoder(r.Body).Decode(&sdp); err != nil {
		http.Error(w, "Wrong SDP", http.StatusBadRequest)
		return
	}

	log.Printf("Got SDP type %s", sdp.Type)

	if sdp.Type == webrtc.SDPTypeOffer {
		if err := peerConnection.SetRemoteDescription(sdp); err != nil {
			http.Error(w, "Error installing SDP", http.StatusInternalServerError)
			return
		}

		answer, err := peerConnection.CreateAnswer(nil)
		if err != nil {
			http.Error(w, "Error creating SDP answer", http.StatusInternalServerError)
			return
		}

		if err := peerConnection.SetLocalDescription(answer); err != nil {
			http.Error(w, "Error installing Local SDP", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(answer); err != nil {
			http.Error(w, "Error sending SDP answer", http.StatusInternalServerError)
			return
		}
		log.Println("Sent SDP Answer")
	}
}
