package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	_ "net/http/pprof"
	"os"
	"strconv"
	"time"

	"github.com/inlivedev/sfu"
	"github.com/inlivedev/sfu/pkg/fakeclient"
	"github.com/inlivedev/sfu/pkg/interceptors/voiceactivedetector"
	"github.com/inlivedev/sfu/pkg/networkmonitor"
	"github.com/pion/logging"
	"github.com/pion/webrtc/v4"
	"golang.org/x/net/websocket"
)

// RequestMessage represents incoming messages from the WebSocket client.
type RequestMessage struct {
	Type string      `json:"type"`
	Data interface{} `json:"data"`
}

// Response represents messages sent to the WebSocket client.
type Response struct {
	Status bool        `json:"status"`
	Type   string      `json:"type"`
	Data   interface{} `json:"data"`
}

// VADData holds voice-activated detection data.
type VADData struct {
	SSRC     uint32                                `json:"ssrc"`
	TrackID  string                                `json:"track_id"`
	StreamID string                                `json:"stream_id"`
	Packets  []voiceactivedetector.VoicePacketData `json:"packets"`
}

// AvailableTrack describes an available track for subscription.
type AvailableTrack struct {
	ClientID   string `json:"client_id"`
	ClientName string `json:"client_name"`
	TrackID    string `json:"track_id"`
	StreamID   string `json:"stream_id"`
	Source     string `json:"source"`
}

const (
	TypeOffer                = "offer"
	TypeAnswer               = "answer"
	TypeCandidate            = "candidate"
	TypeNetworkCondition     = "network_condition"
	TypeError                = "error"
	TypeAllowRenegotiation   = "allow_renegotiation"
	TypeIsAllowRenegotiation = "is_allow_renegotiation"
	TypeTrackAdded           = "tracks_added"
	TypeTracksAvailable      = "tracks_available"
	TypeSubscribeTracks      = "subscribe_tracks"
	TypeSwitchQuality        = "switch_quality"
	TypeUpdateBandwidth      = "update_bandwidth"
	TypeSetBandwidthLimit    = "set_bandwidth_limit"
	TypeBitrateAdjusted      = "bitrate_adjusted"
	TypeTrackStats           = "track_stats"
	TypeVoiceDetected        = "voice_detected"
)

var logger logging.LeveledLogger

func main() {
	// Set environment variables for logging.
	_ = os.Setenv("logtostderr", "true")
	os.Setenv("stderrthreshold", "TRACE")
	os.Setenv("PIONS_LOG_TRACE", "sfu,vad,bitratecontroller")
	os.Setenv("PIONS_LOG_DEBUG", "sfu,vad,bitratecontroller")
	os.Setenv("PIONS_LOG_INFO", "sfu,vad,bitratecontroller")
	os.Setenv("PIONS_LOG_WARN", "sfu,vad,bitratecontroller")
	os.Setenv("PIONS_LOG_ERROR", "sfu,vad,bitratecontroller")

	logger = logging.NewDefaultLoggerFactory().NewLogger("sfu")

	// Create context with cancel to manage lifecycle.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Load default SFU options.
	sfuOpts := sfu.DefaultOptions()
	sfuOpts.EnableBandwidthEstimator = true

	// Fake client count can be set here if desired; it's zero by default.
	fakeClientCount := 0

	// Enable TURN if needed.
	_, turnEnabled := os.LookupEnv("TURN_ENABLED")
	if turnEnabled || fakeClientCount > 0 {
		sfu.StartStunServer(ctx, "127.0.0.1")
		sfuOpts.IceServers = append(sfuOpts.IceServers, webrtc.ICEServer{
			URLs: []string{"stun:127.0.0.1:3478"},
		})
	}

	// Create a room manager for SFU.
	roomManager := sfu.NewManager(ctx, "server-name-here", sfuOpts)

	// Generate a new room ID (extend for multiple rooms if needed).
	roomID := roomManager.CreateRoomID()
	roomName := "test-room"

	// Create a new room with custom options.
	roomOpts := sfu.DefaultRoomOptions()
	roomOpts.Bitrates.InitialBandwidth = 1_000_000
	defaultRoom, _ := roomManager.NewRoom(roomID, roomName, sfu.RoomTypeLocal, roomOpts)

	// ICE servers for connections.
	iceServers := []webrtc.ICEServer{
		{URLs: []string{"stun:127.0.0.1:3478"}},
	}

	// Create any fake clients if needed.
	for i := 0; i < fakeClientCount; i++ {
		fc := fakeclient.Create(ctx, roomManager.Log(), defaultRoom, iceServers, fmt.Sprintf("fake-client-%d", i), true)
		fc.Client.OnTracksAdded(func(addedTracks []sfu.ITrack) {
			setTracks := make(map[string]sfu.TrackType)
			for _, track := range addedTracks {
				setTracks[track.ID()] = sfu.TrackTypeMedia
			}
			fc.Client.SetTracksSourceType(setTracks)
		})
	}

	// Serve static files.
	fs := http.FileServer(http.Dir("./"))
	http.Handle("/", fs)

	// WebSocket handler.
	http.Handle("/ws", websocket.Handler(func(conn *websocket.Conn) {
		msgCh := make(chan RequestMessage)
		isDebug := conn.Request().URL.Query().Get("debug") != ""

		go wsClientHandler(isDebug, conn, msgCh, defaultRoom)
		wsReader(conn, msgCh)
	}))

	// Stats handler endpoint.
	http.HandleFunc("/stats", func(w http.ResponseWriter, r *http.Request) {
		statsHandler(w, r, defaultRoom)
	})

	logger.Info("Listening on http://localhost:8000 ...")

	if err := http.ListenAndServe(":8000", nil); err != nil {
		log.Panic(err)
	}
}

// statsHandler sends room stats in JSON format.
func statsHandler(w http.ResponseWriter, _ *http.Request, room *sfu.Room) {
	stats := room.Stats()
	statsJSON, _ := json.Marshal(stats)

	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(statsJSON)
}

// wsReader reads messages from the WebSocket and pushes them to a channel.
func wsReader(conn *websocket.Conn, msgCh chan RequestMessage) {
	ctx, cancel := context.WithCancel(conn.Request().Context())
	defer cancel()

MessageLoop:
	for {
		select {
		case <-ctx.Done():
			break MessageLoop
		default:
			for {
				decoder := json.NewDecoder(conn)
				var req RequestMessage

				if err := decoder.Decode(&req); err != nil {
					if err.Error() == "EOF" {
						continue
					}
					logger.Errorf("error decoding message: %v", err)
				}
				msgCh <- req
			}
		}
	}
}

// wsClientHandler manages the lifecycle and message handling for a single client connection.
func wsClientHandler(isDebug bool, conn *websocket.Conn, msgCh chan RequestMessage, room *sfu.Room) {
	ctx, cancel := context.WithCancel(conn.Request().Context())
	defer cancel()

	// Create new client ID and add client to the room.
	clientID := room.CreateClientID()
	opts := sfu.DefaultClientOptions()
	opts.EnableVoiceDetection = true
	opts.ReorderPackets = false

	client, err := room.AddClient(clientID, clientID, opts)
	if err != nil {
		log.Panic(err)
		return
	}
	defer room.StopClient(client.ID())

	if isDebug {
		client.EnableDebug()
	}

	// Notify client of its ID.
	_, _ = conn.Write([]byte("{\"type\":\"clientid\",\"data\":\"" + clientID + "\"}"))

	// Channel used to handle answers during renegotiation.
	answerCh := make(chan webrtc.SessionDescription)

	// Inform the client of new tracks added.
	client.OnTracksAdded(func(tracks []sfu.ITrack) {
		tracksAdded := make(map[string]map[string]string)
		for _, track := range tracks {
			tracksAdded[track.ID()] = map[string]string{"id": track.ID()}
		}
		resp := Response{
			Status: true,
			Type:   TypeTrackAdded,
			Data:   tracksAdded,
		}
		data, _ := json.Marshal(resp)
		_, _ = conn.Write(data)
	})

	// Inform the client of tracks available for subscription.
	client.OnTracksAvailable(func(tracks []sfu.ITrack) {
		if client.IsDebugEnabled() {
			logger.Infof("tracks available: %v", tracks)
		}
		tracksAvailable := make(map[string]map[string]interface{})
		for _, track := range tracks {
			tracksAvailable[track.ID()] = map[string]interface{}{
				"id":           track.ID(),
				"client_id":    track.ClientID(),
				"source_type":  track.SourceType().String(),
				"kind":         track.Kind().String(),
				"is_simulcast": track.IsSimulcast(),
			}
		}
		resp := Response{
			Status: true,
			Type:   TypeTracksAvailable,
			Data:   tracksAvailable,
		}
		data, _ := json.Marshal(resp)
		_, _ = conn.Write(data)
	})

	// Renegotiation offer from SFU.
	client.OnRenegotiation(func(ctx context.Context, offer webrtc.SessionDescription) (webrtc.SessionDescription, error) {
		logger.Infof("receive renegotiation offer from SFU")

		resp := Response{
			Status: true,
			Type:   TypeOffer,
			Data:   offer,
		}
		sdpBytes, _ := json.Marshal(resp)
		_, _ = conn.Write(sdpBytes)

		ctxTimeout, cancelTimeout := context.WithTimeout(client.Context(), 30*time.Second)
		defer cancelTimeout()

		// Wait for client answer or timeout.
		select {
		case <-ctxTimeout.Done():
			logger.Errorf("timeout on renegotiation")
			return webrtc.SessionDescription{}, errors.New("timeout on renegotiation")
		case answer := <-answerCh:
			logger.Infof("received answer from client %s %s", client.Type(), client.ID())
			return answer, nil
		}
	})

	// Allow remote renegotiation from SFU.
	client.OnAllowedRemoteRenegotiation(func() {
		logger.Infof("receive allow remote renegotiation from SFU")

		resp := Response{
			Status: true,
			Type:   TypeAllowRenegotiation,
			Data:   "ok",
		}
		data, _ := json.Marshal(resp)
		_, _ = conn.Write(data)
	})

	// Send ICE candidates to the client.
	client.OnIceCandidate(func(ctx context.Context, candidate *webrtc.ICECandidate) {
		resp := Response{
			Status: true,
			Type:   TypeCandidate,
			Data:   candidate,
		}
		candidateBytes, _ := json.Marshal(resp)
		_, _ = conn.Write(candidateBytes)
	})

	// Notify client about changes in network conditions.
	client.OnNetworkConditionChanged(func(condition networkmonitor.NetworkConditionType) {
		resp := Response{
			Status: true,
			Type:   TypeNetworkCondition,
			Data:   condition,
		}
		data, _ := json.Marshal(resp)
		_, _ = conn.Write(data)
	})

	// Periodically send track stats to the client.
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			stats := client.Stats()
			resp := Response{
				Status: true,
				Type:   TypeTrackStats,
				Data:   stats,
			}
			respBytes, _ := json.Marshal(resp)
			_, _ = conn.Write(respBytes)

		case req := <-msgCh:
			switch req.Type {
			// Handle offer/answer from the client.
			case TypeOffer, TypeAnswer:
				sdp, _ := req.Data.(string)
				if req.Type == TypeOffer {
					answer, err := client.Negotiate(webrtc.SessionDescription{SDP: sdp, Type: webrtc.SDPTypeOffer})
					resp := Response{}
					if err != nil {
						logger.Errorf("error on negotiate: %v", err)
						resp.Status = false
						resp.Type = TypeError
						resp.Data = err.Error()
					} else {
						resp.Status = true
						resp.Type = TypeAnswer
						resp.Data = answer
					}
					respBytes, _ := json.Marshal(resp)
					_, _ = conn.Write(respBytes)
				} else {
					logger.Infof("receive renegotiation answer from client")
					answerCh <- webrtc.SessionDescription{SDP: sdp, Type: webrtc.SDPTypeAnswer}
				}

			// Handle ICE candidate from the client.
			case TypeCandidate:
				candidate := webrtc.ICECandidateInit{
					Candidate: req.Data.(string),
				}
				if err := client.AddICECandidate(candidate); err != nil {
					log.Panic("error on add ice candidate: ", err)
				}

			// Handle new local tracks set by the client.
			case TypeTrackAdded:
				setTracks := make(map[string]sfu.TrackType)
				for id, trackType := range req.Data.(map[string]interface{}) {
					if trackType.(string) == "media" {
						setTracks[id] = sfu.TrackTypeMedia
					} else {
						setTracks[id] = sfu.TrackTypeScreen
					}
				}
				client.SetTracksSourceType(setTracks)

			// Handle track subscription requests.
			case TypeSubscribeTracks:
				subTracks := []sfu.SubscribeTrackRequest{}
				tracks, ok := req.Data.([]interface{})
				if ok {
					for _, t := range tracks {
						if trackData, ok := t.(map[string]interface{}); ok {
							subTrack := sfu.SubscribeTrackRequest{
								ClientID: trackData["client_id"].(string),
								TrackID:  trackData["track_id"].(string),
							}
							subTracks = append(subTracks, subTrack)
						}
					}
					if err := client.SubscribeTracks(subTracks); err != nil {
						logger.Errorf("error on subscribe tracks: %v", err)
					}
				} else {
					logger.Errorf("error on subscribe tracks wrong data format: %v", req.Data)
				}

			// Handle quality switching.
			case TypeSwitchQuality:
				quality := req.Data.(string)
				switch quality {
				case "low":
					log.Println("switch to low quality")
					client.SetQuality(sfu.QualityLow)
				case "lowmid":
					log.Println("switch to low mid quality")
					client.SetQuality(sfu.QualityLowMid)
				case "lowlow":
					log.Println("switch to low low quality")
					client.SetQuality(sfu.QualityLowLow)
				case "mid":
					log.Println("switch to mid quality")
					client.SetQuality(sfu.QualityMid)
				case "midmid":
					log.Println("switch to mid mid quality")
					client.SetQuality(sfu.QualityMidMid)
				case "midlow":
					log.Println("switch to mid low quality")
					client.SetQuality(sfu.QualityMidLow)
				case "high":
					log.Println("switch to high quality")
					client.SetQuality(sfu.QualityHigh)
				case "highmid":
					log.Println("switch to high mid quality")
					client.SetQuality(sfu.QualityHighMid)
				case "highlow":
					log.Println("switch to high low quality")
					client.SetQuality(sfu.QualityHighLow)
				case "none":
					log.Println("switch to no quality")
					client.SetQuality(sfu.QualityNone)
				}

			// Handle dynamic publisher bandwidth updates.
			case TypeUpdateBandwidth:
				bandwidth := uint32(req.Data.(float64))
				client.UpdatePublisherBandwidth(bandwidth)

			// Handle receiving bandwidth limit settings.
			case TypeSetBandwidthLimit:
				bandwidth, _ := strconv.ParseUint(req.Data.(string), 10, 32)
				client.SetReceivingBandwidthLimit(uint32(bandwidth))

			// Check if SFU allows renegotiation.
			case TypeIsAllowRenegotiation:
				resp := Response{
					Status: true,
					Type:   TypeAllowRenegotiation,
					Data:   client.IsAllowNegotiation(),
				}
				respBytes, _ := json.Marshal(resp)
				_, _ = conn.Write(respBytes)

			// Unknown message type.
			default:
				logger.Errorf("unknown message type: %v", req)
			}
		}
	}
}
