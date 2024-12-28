package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	_ "net/http/pprof"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/inlivedev/sfu"
	// "github.com/inlivedev/sfu/pkg/fakeclient"
	"github.com/inlivedev/sfu/pkg/interceptors/voiceactivedetector"
	"github.com/inlivedev/sfu/pkg/networkmonitor"
	"github.com/pion/logging"
	"github.com/pion/webrtc/v4"
	"golang.org/x/net/websocket"
)

type Request struct {
	Type string      `json:"type"`
	Data interface{} `json:"data"`
}

type Response struct {
	Status bool        `json:"status"`
	Type   string      `json:"type"`
	Data   interface{} `json:"data"`
}

type VAD struct {
	SSRC     uint32                                `json:"ssrc"`
	TrackID  string                                `json:"track_id"`
	StreamID string                                `json:"stream_id"`
	Packets  []voiceactivedetector.VoicePacketData `json:"packets"`
}

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
	TypeJoinRoom			 = "join_room"
	TypeLeaveRoom			 = "leave_room"
	TypeClientID			 = "client_id"
)

var (
	logger logging.LeveledLogger
	roomManager *sfu.Manager
	roomsNameMap = struct {  // unfortunately we should create map:
		sync.Mutex  // roomName -> roomID
		rooms map[string]string  // because of roomManager does not have it
	}{rooms: make(map[string]string)}
	isDebug = os.Getenv("DEBUG") == "true"
)

func main() {
	_ = os.Setenv("logtostderr", "true")
	os.Setenv("stderrthreshold", "TRACE")

	os.Setenv("PIONS_LOG_TRACE", "sfu,vad,bitratecontroller")
	os.Setenv("PIONS_LOG_DEBUG", "sfu,vad,bitratecontroller")
	os.Setenv("PIONS_LOG_INFO", "sfu,vad,bitratecontroller")
	os.Setenv("PIONS_LOG_WARN", "sfu,vad,bitratecontroller")
	os.Setenv("PIONS_LOG_ERROR", "sfu,vad,bitratecontroller")
	logger = logging.NewDefaultLoggerFactory().NewLogger("sfu")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Настройки SFU
	sfuOpts := sfu.DefaultOptions()
	sfuOpts.EnableBandwidthEstimator = true

	// Создаём менеджер комнат
	roomManager = sfu.NewManager(ctx, "SFU-Server", sfuOpts)

	fs := http.FileServer(http.Dir("./static"))
	http.Handle("/", fs)
	http.Handle("/ws", websocket.Handler(handleWebSocket))

	logger.Info("SFU Server is running at ws://localhost:8000/ws")
	if err := http.ListenAndServe(":8000", nil); err != nil {
		log.Panic(err)
	}
}

func handleWebSocket(conn *websocket.Conn) {
	ctx, cancel := context.WithCancel(conn.Request().Context())
	defer cancel()

	messageChan := make(chan Request)
	go readMessages(conn, messageChan)

	var room *sfu.Room
	var clientID string

	select {
	case msg := <-messageChan:
		if msg.Type == TypeJoinRoom {
			data, ok := msg.Data.(map[string]interface{})
			if !ok {
				sendError(conn, "Invalid data format in join_room message")
				return
			}

			// Проверяем наличие roomID и roomName
			roomID, ok := data["roomID"].(string)
			if !ok {
				sendError(conn, "Missing or invalid roomID")
				return
			}

			roomName, ok := data["roomName"].(string)
			if !ok {
				sendError(conn, "Missing or invalid roomName")
				return
			}

			room = getOrCreateRoom(roomID, roomName)
			if room == nil {
				sendError(conn, "Failed to join or create room")
				return
			}

			clientID = room.CreateClientID()
			sendClientID(conn, clientID)
		} else {
			sendError(conn, "First message must be a join room message")
			return
		}
	case <-ctx.Done():
		return
	}
	go func() {
		<-ctx.Done()
		if room != nil && clientID != "" {
			logger.Infof("Cleaning up client %s from room %s", clientID, room.ID())
			room.StopClient(clientID)
		}
	}()

	clientHandler(isDebug, conn, messageChan, room, clientID)
}

func getOrCreateRoom(roomID string, roomName string) *sfu.Room {
	room, _ := roomManager.GetRoom(roomID)
	if room != nil {
		return room
	}

	roomOpts := sfu.DefaultRoomOptions()
	roomOpts.Bitrates.InitialBandwidth = 1_000_000

	newRoom, err := roomManager.NewRoom(roomID, roomName, sfu.RoomTypeLocal, roomOpts)
	if err != nil {
		logger.Errorf("Failed to create room: %v", err)
		return nil
	}

	logger.Infof("Created new room: %s (ID: %s)", roomName, roomID)
	return newRoom
}

func statsHandler(w http.ResponseWriter, r *http.Request, room *sfu.Room) {
	stats := room.Stats()

	statsJSON, _ := json.Marshal(stats)

	w.Header().Set("Content-Type", "application/json")

	_, _ = w.Write([]byte(statsJSON))
}

func sendError(conn *websocket.Conn, message string) {
	if conn == nil {
		logger.Errorf("Attempted to write to a nil connection")
		return
	}
	
	if conn.Request().Context().Err() != nil {
		logger.Errorf("Connection context error, cannot send message")
		return
	}

	resp := Response{
		Status: false,
		Type:   "error",
		Data:   message,
	}
	respBytes, _ := json.Marshal(resp)
	conn.Write(respBytes)
}

func readMessages(conn *websocket.Conn, messageChan chan Request) {
	defer close(messageChan)

	for {
		var req Request
		if err := json.NewDecoder(conn).Decode(&req); err != nil {
			if errors.Is(err, io.EOF) || conn.Request().Context().Err() != nil {
				logger.Infof("Connection closed by client")
				sendError(conn, err.Error())
				return
			}
			logger.Errorf("Error decoding message: %v", err)
			return
		}
		logger.Infof("Received WebSocket message: %+v", req)
		messageChan <- req
	}
}

func reader(conn *websocket.Conn, messageChan chan Request) {
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
				var req Request
				err := decoder.Decode(&req)
				if err != nil {
					if err.Error() == "EOF" {
						continue
					}

					logger.Infof("error decoding message", err)
				}
				messageChan <- req
			}
		}
	}
}

func clientHandler(isDebug bool, conn *websocket.Conn, messageChan chan Request, r *sfu.Room, clientID string) {
	ctx, cancel := context.WithCancel(conn.Request().Context())
	defer cancel()

	// create new client id, you can pass a unique int value to this function
	// or just use the SFU client counter
	// clientID := r.CreateClientID()

	// add a new client to room
	// you can also get the client by using r.GetClient(clientID)
	opts := sfu.DefaultClientOptions()
	opts.EnableVoiceDetection = true
	opts.ReorderPackets = false
	client, err := r.AddClient(clientID, clientID, opts)
	if err != nil {
		log.Panic(err)
		return
	}

	if isDebug {
		client.EnableDebug()
	}

	defer r.StopClient(client.ID())
	sendClientID(conn, clientID)

	// _, _ = conn.Write([]byte("{\"type\":\"clientid\",\"data\":\"" + clientID + "\"}"))

	answerChan := make(chan webrtc.SessionDescription)

	// client.SubscribeAllTracks()

	client.OnTracksAdded(func(tracks []sfu.ITrack) {
		tracksAdded := map[string]map[string]string{}
		for _, track := range tracks {
			tracksAdded[track.ID()] = map[string]string{"id": track.ID()}
		}
		resp := Response{
			Status: true,
			Type:   TypeTrackAdded,
			Data:   tracksAdded,
		}

		trackAddedResp, _ := json.Marshal(resp)

		_, _ = conn.Write(trackAddedResp)
	})

	client.OnTracksAvailable(func(tracks []sfu.ITrack) {
		if client.IsDebugEnabled() {
			logger.Infof("tracks available", tracks)
		}
		tracksAvailable := map[string]map[string]interface{}{}
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

		trackAddedResp, _ := json.Marshal(resp)

		_, _ = conn.Write(trackAddedResp)
	})

	client.OnRenegotiation(func(ctx context.Context, offer webrtc.SessionDescription) (webrtc.SessionDescription, error) {
		// SFU request a renegotiation, send the offer to client
		logger.Infof("receive renegotiation offer from SFU")

		resp := Response{
			Status: true,
			Type:   TypeOffer,
			Data:   offer,
		}

		sdpBytes, _ := json.Marshal(resp)

		_, _ = conn.Write(sdpBytes)

		// wait for answer from client
		ctxTimeout, cancelTimeout := context.WithTimeout(client.Context(), 30*time.Second)

		defer cancelTimeout()

		// this will wait for answer from client in 30 seconds or timeout
		select {
		case <-ctxTimeout.Done():
			logger.Errorf("timeout on renegotiation")
			return webrtc.SessionDescription{}, errors.New("timeout on renegotiation")
		case answer := <-answerChan:
			logger.Infof("received answer from client ", client.Type(), client.ID())
			return answer, nil
		}
	})

	client.OnAllowedRemoteRenegotiation(func() {
		// SFU allow a remote renegotiation
		logger.Infof("receive allow remote renegotiation from SFU")

		resp := Response{
			Status: true,
			Type:   TypeAllowRenegotiation,
			Data:   "ok",
		}

		respBytes, _ := json.Marshal(resp)

		_, _ = conn.Write(respBytes)
	})

	type Bitrates struct {
		Min         uint32 `json:"min"`
		Max         uint32 `json:"max"`
		TotalClient uint32 `json:"total_client"`
	}

	client.OnIceCandidate(func(ctx context.Context, candidate *webrtc.ICECandidate) {
		// SFU send an ICE candidate to client
		resp := Response{
			Status: true,
			Type:   TypeCandidate,
			Data:   candidate,
		}
		candidateBytes, _ := json.Marshal(resp)

		_, _ = conn.Write(candidateBytes)
	})

	client.OnNetworkConditionChanged(func(condition networkmonitor.NetworkConditionType) {
		resp := Response{
			Status: true,
			Type:   TypeNetworkCondition,
			Data:   condition,
		}
		respBytes, _ := json.Marshal(resp)

		_, _ = conn.Write(respBytes)
	})

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

		case req := <-messageChan:
			// handle as SDP if no error
			if req.Type == TypeOffer || req.Type == TypeAnswer {
				var resp Response

				sdp, _ := req.Data.(string)

				if req.Type == TypeOffer {
					dataMap, ok := req.Data.(map[string]interface{})
					if !ok {
						sendError(conn, "Invalid data format in offer message")
						continue
					}

					sdp, ok := dataMap["sdp"].(string)
					if !ok {
						sendError(conn, "Missing or invalid sdp in offer message")
						continue
					}

					sdpTypeStr, ok := dataMap["type"].(string)
					if !ok {
						sendError(conn, "Missing or invalid type in offer message")
						continue
					}

					var sdpType webrtc.SDPType
					switch sdpTypeStr {
					case "offer":
						sdpType = webrtc.SDPTypeOffer
					case "answer":
						sdpType = webrtc.SDPTypeAnswer
					default:
						sendError(conn, "Invalid SDP type")
						continue
					}

					description := webrtc.SessionDescription{Type: sdpType, SDP: sdp}
					answer, err := client.Negotiate(description)
					if err != nil {
						sendError(conn, "Failed to negotiate: "+err.Error())
						continue
					}

					resp = Response{
						Status: true,
						Type:   TypeAnswer,
						Data:   answer,
					}
					respBytes, _ := json.Marshal(resp)
					conn.Write(respBytes)
				} else {
					logger.Infof("receive renegotiation answer from client")
					// handle as answer SDP as part of renegotiation request from SFU
					// pass the answer to onRenegotiation handler above
					answerChan <- webrtc.SessionDescription{SDP: sdp, Type: webrtc.SDPTypeAnswer}
				}

				// don't continue execution
				continue
			} else if req.Type == TypeCandidate {
				dataMap, ok := req.Data.(map[string]interface{})
                if !ok {
                    sendError(conn, "Invalid data format in candidate message")
                    continue
                }

                candidateStr, ok := dataMap["candidate"].(string)
                if !ok {
                    sendError(conn, "Missing or invalid candidate in candidate message")
                    continue
                }

                sdpMLineIndexFloat, ok := dataMap["sdpMLineIndex"].(float64)
                if !ok {
                    sendError(conn, "Missing or invalid sdpMLineIndex in candidate message")
                    continue
                }
                sdpMLineIndex := uint16(sdpMLineIndexFloat)

                sdpMid, ok := dataMap["sdpMid"].(string)
                if !ok {
                    sendError(conn, "Missing or invalid sdpMid in candidate message")
                    continue
                }

                candidate := webrtc.ICECandidateInit{
                    Candidate:    candidateStr,
                    SDPMLineIndex: &sdpMLineIndex,
                    SDPMid:       &sdpMid,
                }

                err := client.AddICECandidate(candidate)
                if err != nil {
                    sendError(conn, "Failed to add ICE candidate: "+err.Error())
                    continue
                }
			} else if req.Type == TypeTrackAdded {
				setTracks := make(map[string]sfu.TrackType, 0)
				for id, trackType := range req.Data.(map[string]interface{}) {
					if trackType.(string) == "media" {
						setTracks[id] = sfu.TrackTypeMedia
					} else {
						setTracks[id] = sfu.TrackTypeScreen
					}
				}
				client.SetTracksSourceType(setTracks)
			} else if req.Type == TypeSubscribeTracks {
				subTracks := make([]sfu.SubscribeTrackRequest, 0)
				tracks, ok := req.Data.([]interface{})
				if ok {
					for _, track := range tracks {
						trackData, ok := track.(map[string]interface{})
						if ok {
							subTrack := sfu.SubscribeTrackRequest{
								ClientID: trackData["client_id"].(string),
								TrackID:  trackData["track_id"].(string),
							}

							subTracks = append(subTracks, subTrack)
						}
					}

					if err := client.SubscribeTracks(subTracks); err != nil {
						logger.Errorf("error on subscribe tracks", err)
					}
				} else {
					logger.Errorf("error on subscribe tracks wrong data format ", req.Data)
				}

			} else if req.Type == TypeSwitchQuality {
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
					log.Println("switch to high quality")
					client.SetQuality(sfu.QualityNone)
				}
			} else if req.Type == TypeUpdateBandwidth {
				bandwidth := uint32(req.Data.(float64))
				client.UpdatePublisherBandwidth(bandwidth)
			} else if req.Type == TypeSetBandwidthLimit {
				bandwidth, _ := strconv.ParseUint(req.Data.(string), 10, 32)
				client.SetReceivingBandwidthLimit(uint32(bandwidth))
			} else if req.Type == TypeIsAllowRenegotiation {
				resp := Response{
					Status: true,
					Type:   TypeAllowRenegotiation,
					Data:   client.IsAllowNegotiation(),
				}

				respBytes, _ := json.Marshal(resp)

				conn.Write(respBytes)

			} else {
				logger.Errorf("unknown message type", req)
			}
		}
	}
}

func sendClientID(conn *websocket.Conn, clientID string) {
	resp := Response{
		Status: true,
		Type: TypeClientID,
		Data: clientID,
	}
	respBytes, _ := json.Marshal(resp)
	conn.Write(respBytes)
}