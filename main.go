package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	_ "net/http/pprof"
	"os"
	"strconv"
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

const APIBaseURL = "http://localhost:8080/api"

type RoomExistsResponse struct {
	Exists bool   `json:"exists"`
	Error  string `json:"error,omitempty"`
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

var (
	logger logging.LeveledLogger
	roomManager *sfu.Manager
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

	sfuOpts := sfu.DefaultOptions()

	sfuOpts.EnableBandwidthEstimator = true

	fakeClientCount := 0

	_, turnEnabled := os.LookupEnv("TURN_ENABLED")
	if turnEnabled || fakeClientCount > 0 {
		sfu.StartStunServer(ctx, "127.0.0.1")
		sfuOpts.IceServers = append(sfuOpts.IceServers, webrtc.ICEServer{
			URLs: []string{"stun:127.0.0.1:3478"},
		})
	}

	// create room manager first before create new room
	roomManager = sfu.NewManager(ctx, "gocall-room-manager", sfuOpts)

	// generate a new room id. You can extend this example into a multiple room by use this in it's own API endpoint
	// roomID := roomManager.CreateRoomID()
	// roomName := "test-room"

	// create new room
	// roomsOpts := sfu.DefaultRoomOptions()
	// roomsOpts.Bitrates.InitialBandwidth = 1_000_000
	// roomsOpts.PLIInterval = 3 * time.Second
	// defaultRoom, _ := roomManager.NewRoom(roomID, roomName, sfu.RoomTypeLocal, roomsOpts)
	// turnServer := sfu.StartTurnServer(ctx, localIp.String())
	// defer turnServer.Close()

	// iceServers := []webrtc.ICEServer{
	// 	{
	// 		URLs: []string{"stun:127.0.0.1:3478"},
	// 	},
	// }

	// for i := 0; i < fakeClientCount; i++ {
	// 	// create a fake client
	// 	fc := fakeclient.Create(ctx, roomManager.Log(), defaultRoom, iceServers, fmt.Sprintf("fake-client-%d", i), true)

	// 	fc.Client.OnTracksAdded(func(addedTracks []sfu.ITrack) {
	// 		setTracks := make(map[string]sfu.TrackType, 0)
	// 		for _, track := range addedTracks {
	// 			setTracks[track.ID()] = sfu.TrackTypeMedia
	// 		}
	// 		fc.Client.SetTracksSourceType(setTracks)
	// 	})
	// }

	fs := http.FileServer(http.Dir("./"))
	http.Handle("/", fs)

	http.Handle("/ws", websocket.Handler(func(conn *websocket.Conn) {
		// 1. Считываем room_id
		roomID := conn.Request().URL.Query().Get("room_id")
		if roomID == "" {
			// Отправить ошибку клиенту и разорвать соединение
			log.Println("No room_id provided, closing connection")
			conn.Close()
			return
		}
	
		// 2. Проверяем по API, что такая комната действительно есть
		exists, err := checkRoomExistsAPI(roomID)
		if err != nil {
			log.Fatal(err)
			return
		}
		if !exists {
			log.Println("Room not found in API, closing connection")
			conn.Close()
			return
		}
	
		// 3. Пытаемся получить комнату из sfu.Manager
		room, err := roomManager.GetRoom(roomID)
		if err != nil {
			log.Println(roomID)
			log.Println(err)
			// Комнаты ещё нет в SFU, значит создаём:
			// Тут можем задать любое name, roomType, roomOpts
			roomName := "PublicRoom" // todo get from query
			roomOpts := sfu.DefaultRoomOptions()
			room, err = roomManager.NewRoom(roomID, roomName, sfu.RoomTypeLocal, roomOpts)
			if err != nil {
				log.Printf("Error creating room: %v\n", err)
				conn.Close()
				return
			}
		}
		var clientId string
		clientId = conn.Request().URL.Query().Get("client_id")
		if clientId == "" {
			clientId = room.CreateClientID()
		}
		messageChan := make(chan Request)
		isDebug := false
		if conn.Request().URL.Query().Get("debug") != "" {
			isDebug = true
		}
		go clientHandler(isDebug, conn, messageChan, room, clientId) // todo add client name
		reader(conn, messageChan)
	}))

	// http.HandleFunc("/stats", func(w http.ResponseWriter, r *http.Request) {
	// 	statsHandler(w, r, room)
	// })

	logger.Info("Listening on http://localhost:8000 ...")

	err := http.ListenAndServe(":8000", nil)
	if err != nil {
		log.Panic(err)
	}
}

func checkRoomExistsAPI(roomID string) (bool, error) {
	url := fmt.Sprintf("%s/rooms/%s/exists", APIBaseURL, roomID)

	// Выполняем GET-запрос
	resp, err := http.Get(url)
	if err != nil {
		return false, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	// Читаем тело ответа
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return false, fmt.Errorf("failed to read response body: %w", err)
	}

	// Парсим JSON-ответ
	var response RoomExistsResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return false, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	// Проверяем наличие ошибки
	if !response.Exists {
		if response.Error != "" {
			log.Printf("Room does not exist: %s", response.Error)
		}
		return false, nil
	}

	return true, nil
}

func statsHandler(w http.ResponseWriter, r *http.Request, room *sfu.Room) {
	stats := room.Stats()

	statsJSON, _ := json.Marshal(stats)

	w.Header().Set("Content-Type", "application/json")

	_, _ = w.Write([]byte(statsJSON))
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

	_, _ = conn.Write([]byte("{\"type\":\"clientid\",\"data\":\"" + clientID + "\"}"))

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

	client.OnIceCandidate(func(ctx context.Context, cand *webrtc.ICECandidate) {
		if cand == nil {
			return
		}

		candidate := cand.ToJSON()
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
					// handle as offer SDP
					answer, err := client.Negotiate(webrtc.SessionDescription{SDP: sdp, Type: webrtc.SDPTypeOffer})
					if err != nil {
						logger.Errorf("error on negotiate", err)

						resp = Response{
							Status: false,
							Type:   TypeError,
							Data:   err.Error(),
						}
					} else {
						// send the answer to client
						resp = Response{
							Status: true,
							Type:   TypeAnswer,
							Data:   answer,
						}
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
				candidate := webrtc.ICECandidateInit{
					Candidate: req.Data.(string),
				}
				err := client.AddICECandidate(candidate)
				if err != nil {
					log.Panic("error on add ice candidate", err)
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