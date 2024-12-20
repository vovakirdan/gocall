let wsProtocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
let baseHost = window.location.host;

// Connect to SFU endpoint
const sfuUrl = wsProtocol + '//' + baseHost + '/ws';

// Generate unique self ID for this client
const selfId = "user_" + Math.floor(Math.random() * 10000);

// Default room
const roomId = "main";

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
let sfuSocket = null;

let isMuted = false;
let isCameraOff = false;

startButton.onclick = start;
callButton.onclick = call;
muteButton.onclick = toggleMute;
cameraButton.onclick = toggleCamera;
hangupButton.onclick = hangUp;

async function start() {
    // Choose source of media: camera or screen
    let source = videoSourceSelect.value;

    try {
        if (source === 'camera') {
            // Get camera and microphone
            localStream = await navigator.mediaDevices.getUserMedia({ video: true, audio: true });
        } else if (source === 'screen') {
            // Get screen sharing stream (some browsers might have restrictions)
            localStream = await navigator.mediaDevices.getDisplayMedia({ video: true, audio: true });
        }
    } catch (err) {
        console.error("Failed to get media:", err);
        return;
    }

    localVideo.srcObject = localStream;

    // Connect to SFU server
    sfuSocket = new WebSocket(sfuUrl);

    sfuSocket.onopen = () => {
        console.log("SFU WebSocket connected");
        // Join room event
        let joinMsg = {
            event: "joinRoom",
            data: {
                self_id: selfId,
                room_id: roomId
            }
        };
        sfuSocket.send(JSON.stringify(joinMsg));
    };

    sfuSocket.onmessage = (event) => {
        const msg = JSON.parse(event.data);
        // Handle incoming events: offer, answer, candidate
        switch (msg.event) {
            case "offer":
                handleOffer(msg.data);
                break;
            case "answer":
                handleAnswer(msg.data);
                break;
            case "candidate":
                handleRemoteICE(msg.data);
                break;
            default:
                console.log("Unknown event:", msg.event);
        }
    };

    sfuSocket.onerror = (err) => {
        console.error("WebSocket error:", err);   
    };

    sfuSocket.onclose = () => {
        console.log("WebSocket closed");
    };

    // Create RTCPeerConnection
    pc = new RTCPeerConnection({
        iceServers: [{ urls: "stun:stun.l.google.com:19302" }]
    });

    // Add local tracks to the connection
    localStream.getTracks().forEach(track => pc.addTrack(track, localStream));

    // Handle remote tracks
    pc.ontrack = (event) => {
        console.log("Got remote track:", event.track);
        remoteVideo.srcObject = event.streams[0];
    };

    // Send ICE candidates to SFU
    pc.onicecandidate = (event) => {
        if (event.candidate) {
            console.log("Sending ICE candidate to SFU");
            let candidateMsg = {
                event: "ice-candidate",
                data: {
                    self_id: selfId,
                    room_id: roomId,
                    candidate: event.candidate.toJSON()
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

async function call() {
    console.log("Creating offer");
    const offer = await pc.createOffer();
    await pc.setLocalDescription(offer);

    console.log("Sending Offer to SFU via WebSocket");
    let offerMsg = {
        event: "offer",
        data: {
            self_id: selfId,
            room_id: roomId,
            offer: offer
        }
    };
    sfuSocket.send(JSON.stringify(offerMsg));
}

async function handleOffer(offer) {
    console.log("Received offer");
    await pc.setRemoteDescription(new RTCSessionDescription(offer));
    const answer = await pc.createAnswer();
    await pc.setLocalDescription(answer);

    console.log("Sending answer back to SFU");
    let answerMsg = {
        event: "answer",
        data: {
            self_id: selfId,
            room_id: roomId,
            answer: answer
        }
    };
    sfuSocket.send(JSON.stringify(answerMsg));
}

async function handleAnswer(answer) {
    console.log("Received answer");
    await pc.setRemoteDescription(new RTCSessionDescription(answer));
}

function handleRemoteICE(candidate) {
    console.log("Received ICE candidate");
    pc.addIceCandidate(new RTCIceCandidate(candidate)).catch(e => console.error("Error adding ICE:", e));
}

// Toggle microphone
function toggleMute() {
    if (localStream) {
        isMuted = !isMuted;
        localStream.getAudioTracks().forEach(track => track.enabled = !isMuted);
        muteButton.textContent = isMuted ? 'Enable microphone' : 'Disable microphone';
    }
}

// Toggle camera (в случае захвата экрана, это будет выключать стрим экрана)
function toggleCamera() {
    if (localStream) {
        isCameraOff = !isCameraOff;
        localStream.getVideoTracks().forEach(track => track.enabled = !isCameraOff);
        cameraButton.textContent = isCameraOff ? 'Turn on camera/screen' : 'Turn off camera/screen';
    }
}

function hangUp() {
    console.log("Ending call");

    // Close WebRTC connection
    if (pc) {
        pc.close();
        pc = null;
    }

    // Close WebSocket connection
    if (sfuSocket) {
        sfuSocket.close();
        sfuSocket = null;
    }

    // Stop local stream
    if (localStream) {
        localStream.getTracks().forEach(track => track.stop());
        localStream = null;
    }

    // Reset UI
    localVideo.srcObject = null;
    remoteVideo.srcObject = null;
    callButton.disabled = true;
    muteButton.disabled = true;
    cameraButton.disabled = true;
    hangupButton.disabled = true;
}
