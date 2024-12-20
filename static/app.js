let wsProtocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
let baseHost = window.location.host;

const signalingUrl = wsProtocol + '//' + baseHost + '/signal';
const webrtcUrl = window.location.origin + '/webrtc';
const roomId = "main"; // For testing, both clients use the same roomId

let localVideo = document.getElementById('localVideo');
let remoteVideo = document.getElementById('remoteVideo');
let startButton = document.getElementById('startButton');
let callButton = document.getElementById('callButton');
let muteButton = document.getElementById('muteButton');
let cameraButton = document.getElementById('cameraButton');
let videoSourceSelect = document.getElementById('videoSource');

let localStream = null;
let pc = null;
let signalingSocket = null;

let isMuted = false;
let isCameraOff = false;

startButton.onclick = start;
callButton.onclick = call;
muteButton.onclick = toggleMute;
cameraButton.onclick = toggleCamera;

async function start() {
    // Выбираем источник видео:
    let source = videoSourceSelect.value; // "camera" или "screen"

    try {
        if (source === 'camera') {
            localStream = await navigator.mediaDevices.getUserMedia({ video: true, audio: true });
        } else if (source === 'screen') {
            // В некоторых браузерах надо будет убрать audio: true
            localStream = await navigator.mediaDevices.getDisplayMedia({ video: true, audio: true });
        }
    } catch (err) {
        console.error("Failed to get media:", err);
        return;
    }

    localVideo.srcObject = localStream;

    // init websocket for signaling
    signalingSocket = new WebSocket(signalingUrl);

    signalingSocket.onopen = () => {
        console.log("WebSocket connected");
        // Join the room
        signalingSocket.send(JSON.stringify({ type: "join", roomId: roomId }));
    };

    signalingSocket.onmessage = (event) => {
        const msg = JSON.parse(event.data);
        if (msg.type === "offer") {
            handleOffer(msg.payload);
        } else if (msg.type === "answer") {
            handleAnswer(msg.payload);
        } else if (msg.type === "ice") {
            handleRemoteICE(msg.payload);
        }
    };

    signalingSocket.onerror = (err) => {
        console.error("webSocket error:", err);   
    };

    signalingSocket.onclose = () => {
        console.log("WebSocket closed");
    };

    // create RTCPeerConnection
    pc = new RTCPeerConnection({
        iceServers: [{ urls: "stun:stun.l.google.com:19302"}]
    })

    // add all tracks from localStream to RTCPeerConnection
    localStream.getTracks().forEach(track => pc.addTrack(track, localStream));

    // Handle incoming video
    pc.ontrack = (event) => {
        console.log("Got remote track:", event.track);
        remoteVideo.srcObject = event.streams[0];
    };

    // sending ICE candidates to WebSocket
    pc.onicecandidate = (event) => {
        if (event.candidate) {
            console.log("Sending ICE candidate to remote");
            signalingSocket.send(JSON.stringify({
                type: "ice",
                roomId: roomId,
                payload: event.candidate
            }));
        }
    };

    callButton.disabled = false;
    muteButton.disabled = false;
    cameraButton.disabled = false;
}

async function call() {
    console.log("Creating offer");
    const offer = await pc.createOffer();
    await pc.setLocalDescription(offer);

    console.log("Sending Offer to server via WebSocket");
    signalingSocket.send(JSON.stringify({
        type: "offer",
        roomId: roomId,
        payload: offer
    }));

    if (!response.ok) {
        console.error("Failed to get answer from server");
        return;
    }

    let answer = await response.json();
    console.log("Got Answer from server:", answer);

    await pc.setRemoteDescription(answer);
    console.log("Remote SDP set successfully");
}

async function handleOffer(offer) {
    console.log("Received offer");
    await pc.setRemoteDescription(new RTCSessionDescription(offer));
    const answer = await pc.createAnswer();
    await pc.setLocalDescription(answer);

    console.log("Sending answer back to initiator");
    signalingSocket.send(JSON.stringify({
        type: "answer",
        roomId: roomId,
        payload: answer
    }));
}

async function handleAnswer(answer) {
    console.log("Received answer");
    await pc.setRemoteDescription(new RTCSessionDescription(answer));
}

function handleRemoteICE(candidate) {
    console.log("Received ICE candidate");
    pc.addIceCandidate(new RTCIceCandidate(candidate)).catch(e=>console.error("Error adding ICE:", e));
}

// Toggle microphone
function toggleMute() {
    if (localStream) {
        isMuted = !isMuted;
        localStream.getAudioTracks().forEach(track => track.enabled = !isMuted);
        muteButton.textContent = isMuted ? 'Включить микрофон' : 'Выключить микрофон';
    }
}

// Toggle camera (в случае захвата экрана, это будет выключать стрим экрана)
function toggleCamera() {
    if (localStream) {
        isCameraOff = !isCameraOff;
        localStream.getVideoTracks().forEach(track => track.enabled = !isCameraOff);
        cameraButton.textContent = isCameraOff ? 'Включить камеру/экран' : 'Выключить камеру/экран';
    }
}
