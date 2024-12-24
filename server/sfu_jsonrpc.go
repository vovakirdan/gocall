package server

// It provides a handler "/sfu" that upgrades HTTP to WebSocket and exchanges
// JSON-RPC messages (join, offer, answer, trickle, etc.) with the client.

import (
    "context"
    "encoding/json"
    "log"
    "net/http"

    "github.com/gorilla/websocket"
    "github.com/pion/ion-sfu/pkg/logger"
    "github.com/pion/ion-sfu/pkg/sfu"
    "github.com/pion/webrtc/v3"
    "github.com/sourcegraph/jsonrpc2"
    websocketjsonrpc2 "github.com/sourcegraph/jsonrpc2/websocket"
)

// ionSFU is a global instance of the SFU for JSON-RPC.
var ionSFU *sfu.SFU

// JSONSignal is a simple struct to handle JSON-RPC requests (join/offer/answer/trickle).
type JSONSignal struct {
    // PeerLocal is the Ion SFU peer for this connection.
    *sfu.PeerLocal
}

// InitSFUJSONRPC initializes the Ion SFU with a default config.
func InitSFUJSONRPC() {
    // Set log verbosity (0 = minimal logs, higher for more detail).
    logger.SetGlobalOptions(logger.GlobalConfig{V: 0})

    // Basic SFU config. In production, load from config file or environment if needed.
    sfuConf := sfu.Config{
        WebRTC: sfu.WebRTCConfig{
            ICEServers: []sfu.ICEServerConfig{
                {
                    // Google STUN server by default
                    URLs: []string{"stun:stun.l.google.com:19302"},
                },
            },
        },
    }

    // Create the Ion SFU instance.
    ionSFU = sfu.NewSFU(sfuConf)
    log.Println("Ion SFU (JSON-RPC) initialized.")
}

// HandleSFUJSONRPC is our HTTP handler for the "/sfu" endpoint.
// It upgrades to WebSocket, creates an SFU peer, and serves JSON-RPC.
func HandleSFUJSONRPC(w http.ResponseWriter, r *http.Request) {
    upgrader := websocket.Upgrader{
        CheckOrigin: func(r *http.Request) bool {
            return true // For demo purposes, allow any origin
        },
    }

    conn, err := upgrader.Upgrade(w, r, nil)
    if err != nil {
        log.Println("Error upgrading to WebSocket (JSON-RPC):", err)
        return
    }
    log.Println("New SFU (JSON-RPC) WebSocket connection")

    // Create a new SFU PeerLocal for this connection
    peer := sfu.NewPeer(ionSFU)

    // Create a JSONSignal to handle RPC methods (join/offer/answer/trickle).
    jsonSignal := &JSONSignal{PeerLocal: peer}

    // Create a jsonrpc2 connection
    jc := jsonrpc2.NewConn(r.Context(), websocketjsonrpc2.NewObjectStream(conn), jsonSignal)

    // Wait until the WebSocket (JSON-RPC) is disconnected
    <-jc.DisconnectNotify()
    log.Println("SFU (JSON-RPC) connection closed")
}

// Handle implements the jsonrpc2.Handler interface. This method is called
// when the client sends an RPC request (e.g. {method:"join", params:{...}}).
func (j *JSONSignal) Handle(ctx context.Context, conn *jsonrpc2.Conn, req *jsonrpc2.Request) {
    // Helper to send an error back
    replyError := func(err error) {
        _ = conn.ReplyWithError(ctx, req.ID, &jsonrpc2.Error{
            Code:    500,
            Message: err.Error(),
        })
    }

    switch req.Method {
    // The client is requesting to join a room with an offer
    case "join":
        var joinMsg struct {
            SID   string                    `json:"sid"`   // Room/session ID
            UID   string                    `json:"uid"`   // User ID
            Offer webrtc.SessionDescription `json:"offer"` // Local offer
        }
        if err := json.Unmarshal(*req.Params, &joinMsg); err != nil {
            log.Println("Error parsing join message:", err)
            replyError(err)
            return
        }

        // Called when SFU has an ICE candidate
        j.OnIceCandidate = func(candidate *webrtc.ICECandidateInit, target int) {
            err := conn.Notify(ctx, "trickle", map[string]interface{}{
                "candidate": candidate,
                "target":    target,
            })
            if err != nil {
                log.Println("Error sending ICE candidate:", err)
            }
        }

        // Called when SFU needs to send an offer to the client
        j.OnOffer = func(offer *webrtc.SessionDescription) {
            err := conn.Notify(ctx, "offer", offer)
            if err != nil {
                log.Println("Error sending offer:", err)
            }
        }

        // Join the SFU session
        err := j.Join(joinMsg.SID, joinMsg.UID, sfu.JoinConfig{})
        if err != nil {
            replyError(err)
            return
        }

        // Create the server-side answer
        answer, err := j.Answer(joinMsg.Offer)
        if err != nil {
            replyError(err)
            return
        }

        // Reply with the answer SDP
        _ = conn.Reply(ctx, req.ID, answer)

    // The client sends an updated offer or wants an answer
    case "offer":
        var offerMsg struct {
            Desc webrtc.SessionDescription `json:"desc"`
        }
        if err := json.Unmarshal(*req.Params, &offerMsg); err != nil {
            log.Println("Error parsing offer:", err)
            replyError(err)
            return
        }

        answer, err := j.Answer(offerMsg.Desc)
        if err != nil {
            replyError(err)
            return
        }
        _ = conn.Reply(ctx, req.ID, answer)

    // The client sends an answer (if the SFU had offered)
    case "answer":
        var ansMsg struct {
            Desc webrtc.SessionDescription `json:"desc"`
        }
        if err := json.Unmarshal(*req.Params, &ansMsg); err != nil {
            log.Println("Error parsing answer:", err)
            replyError(err)
            return
        }
        err := j.SetRemoteDescription(ansMsg.Desc)
        if err != nil {
            replyError(err)
        }

    // The client sends a new ICE candidate
    case "trickle":
        var trickleMsg struct {
            Candidate webrtc.ICECandidateInit `json:"candidate"`
            Target    int                     `json:"target"`
        }
        if err := json.Unmarshal(*req.Params, &trickleMsg); err != nil {
            log.Println("Error parsing trickle:", err)
            replyError(err)
            return
        }
        err := j.Trickle(trickleMsg.Candidate, trickleMsg.Target)
        if err != nil {
            replyError(err)
        }

    default:
        log.Println("Unknown JSON-RPC method:", req.Method)
        replyError(errUnknownMethod(req.Method))
    }
}

func errUnknownMethod(method string) error {
    return &unknownMethodErr{method}
}

type unknownMethodErr struct {
    method string
}

func (u *unknownMethodErr) Error() string {
    return "unknown method: " + u.method
}
