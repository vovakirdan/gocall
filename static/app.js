/* static/app.js
 * This file connects to Ion SFU (JSON-RPC) at /sfu.
 * It creates a local RTCPeerConnection, sends a "join" request with local offer,
 * receives remote answer, and handles ICE/trickle from both sides.
 */

let wsProtocol = (window.location.protocol === 'https:') ? 'wss:' : 'ws:';
let baseHost = window.location.host;
const sfuUrl = wsProtocol + '//' + baseHost + '/sfu';

let localVideo = document.getElementById('localVideo');
let remoteVideo = document.getElementById('remoteVideo');

let startButton = document.getElementById('startButton');
let callButton = document.getElementById('callButton');
let muteButton = document.getElementById('muteButton');
let cameraButton = document.getElementById('cameraButton');
let hangupButton = document.getElementById('hangupButton');
let videoSourceSelect = document.getElementById('videoSource');

let localStream = null;
let pc = null;

// SFU WebSocket for JSON-RPC
let sfuSocket = null;

// Simple state toggles
let isMuted = false;
let isCameraOff = false;

startButton.onclick = start;
callButton.onclick = joinSFU;
muteButton.onclick = toggleMute;
cameraButton.onclick = toggleCamera;
hangupButton.onclick = hangUp;

// Used to match "requests" with "responses". For simple testing, increment an ID.
let rpcRequestId = 1;

/**
 * Get local media (camera or screen), attach to localVideo,
 * create RTCPeerConnection, add tracks, enable call controls.
 */
async function start() {
    let source = videoSourceSelect.value; // "camera" or "screen"

    try {
        if (source === 'camera') {
            localStream = await navigator.mediaDevices.getUserMedia({
                video: true,
                audio: true
            });
        } else {
            localStream = await navigator.mediaDevices.getDisplayMedia({
                video: true,
                audio: true
            });
        }
    } catch (err) {
        console.error("Failed to get media:", err);
        return;
    }

    localVideo.srcObject = localStream;

    pc = new RTCPeerConnection({
        iceServers: [{ urls: "stun:stun.l.google.com:19302" }]
    });

    // Add local tracks to PeerConnection
    localStream.getTracks().forEach(track => {
        pc.addTrack(track, localStream);
    });

    // When remote tracks arrive from SFU
    pc.ontrack = event => {
        console.log("Got remote track from SFU:", event.track);
        remoteVideo.srcObject = event.streams[0];
    };

    // Send local ICE candidates to SFU via JSON-RPC "trickle"
    pc.onicecandidate = event => {
        if (event.candidate && sfuSocket) {
            let candidateMsg = {
                jsonrpc: "2.0",
                method: "trickle",
                params: {
                    candidate: event.candidate,
                    target: 0 // 0 = publisher side
                }
            };
            sfuSocket.send(JSON.stringify(candidateMsg));
        }
    };

    callButton.disabled = false;
    muteButton.disabled = false;
    cameraButton.disabled = false;
    hangupButton.disabled = false;
}

/**
 * Join the SFU room "main" by sending a JSON-RPC "join" request with local offer.
 */
async function joinSFU() {
    if (!pc) {
        console.error("PC not created yet!");
        return;
    }

    sfuSocket = new WebSocket(sfuUrl);

    sfuSocket.onopen = async () => {
        console.log("SFU (JSON-RPC) WebSocket connected.");

        // Create local offer
        let offer = await pc.createOffer();
        await pc.setLocalDescription(offer);

        // Send "join" request to Ion SFU
        let joinRequestId = rpcRequestId++;
        let joinMsg = {
            jsonrpc: "2.0",
            id: joinRequestId,
            method: "join",
            params: {
                sid: "main", // default room name
                uid: "user" + Math.floor(Math.random() * 1000), // random user ID
                offer: {
                    type: offer.type,
                    sdp: offer.sdp
                }
            }
        };
        sfuSocket.send(JSON.stringify(joinMsg));
    };

    // Handle incoming JSON-RPC messages from the SFU
    sfuSocket.onmessage = async event => {
        let msg = JSON.parse(event.data);

        // If it's a response to our request (has an "id" field)
        if (msg.id) {
            if (msg.result) {
                // We expect this to be the "answer" to our "join" request
                console.log("Got 'join' result from SFU:", msg.result);
                let remoteDesc = {
                    type: msg.result.type,
                    sdp: msg.result.sdp
                };
                await pc.setRemoteDescription(new RTCSessionDescription(remoteDesc));
            } else if (msg.error) {
                console.error("JSON-RPC error:", msg.error);
            }
            return;
        }

        // If it's a notification with a "method"
        if (msg.method === "offer") {
            console.log("SFU is renegotiating with 'offer':", msg.params);
            // If SFU sends an updated offer, set it and create an answer
            let offerDesc = {
                type: msg.params.type,
                sdp: msg.params.sdp
            };
            await pc.setRemoteDescription(new RTCSessionDescription(offerDesc));

            let localAnswer = await pc.createAnswer();
            await pc.setLocalDescription(localAnswer);

            // Send it back as "answer" method
            let answerMsg = {
                jsonrpc: "2.0",
                method: "answer",
                params: {
                    desc: {
                        type: localAnswer.type,
                        sdp: localAnswer.sdp
                    }
                }
            };
            sfuSocket.send(JSON.stringify(answerMsg));
        }
        else if (msg.method === "trickle") {
            // SFU is sending an ICE candidate
            let candidateInit = msg.params.candidate;
            console.log("Got remote ICE candidate from SFU:", candidateInit);
            await pc.addIceCandidate(new RTCIceCandidate(candidateInit));
        }
        else {
            console.log("Unknown JSON-RPC message from SFU:", msg);
        }
    };

    sfuSocket.onerror = err => {
        console.error("SFU socket error:", err);
    };

    sfuSocket.onclose = () => {
        console.log("SFU socket closed.");
    };
}

/**
 * Toggle local microphone on/off.
 */
function toggleMute() {
    if (localStream) {
        isMuted = !isMuted;
        localStream.getAudioTracks().forEach(track => {
            track.enabled = !isMuted;
        });
        muteButton.textContent = isMuted ? 'Enable microphone' : 'Mute microphone';
    }
}

/**
 * Toggle local camera (or screen) on/off.
 */
function toggleCamera() {
    if (localStream) {
        isCameraOff = !isCameraOff;
        localStream.getVideoTracks().forEach(track => {
            track.enabled = !isCameraOff;
        });
        cameraButton.textContent = isCameraOff ? 'Turn on camera/screen' : 'Turn off camera/screen';
    }
}

/**
 * Hang up the call.
 */
function hangUp() {
    console.log("Ending call");

    if (pc) {
        pc.close();
        pc = null;
    }

    if (sfuSocket) {
        sfuSocket.close();
        sfuSocket = null;
    }

    if (localStream) {
        localStream.getTracks().forEach(track => {
            track.stop();
        });
        localStream = null;
    }

    localVideo.srcObject = null;
    remoteVideo.srcObject = null;
    callButton.disabled = true;
    muteButton.disabled = true;
    cameraButton.disabled = true;
    hangupButton.disabled = true;
}
