const signalingUrl = "ws://localhost:8080/signal";
const webrtcUrl = "http://localhost:8080/webrtc";

let localVideo = document.getElementById('localVideo');
let remoteVideo = document.getElementById('remoteVideo');
let startButton = document.getElementById('startButton');
let callButton = document.getElementById('callButton');

let localStream = null;
let pc = null;
let signalingSocket = null;

startButton.onclick = start;
callButton.onclick = call;

async function start() {
    // ask for local mediastream (camera + microphone)
    localStream = await navigator.mediaDevices.getUserMedia({ video: true, audio: true });
    localVideo.srcObject = localStream;

    // init websocket for signaling
    signalingSocket = new WebSocket(signalingUrl);

    signalingSocket.onopen = () => {
        console.log("WebSocket connected");
        // Assume, we have roomId from URL parameters or strongly fixed:
        let roomId = "main"; 
        signalingSocket.send(JSON.stringify({ type: "join", roomId: roomId }));
    };

    signalingSocket.onmessage = (event) => {
        const msg = JSON.parse(event.data);
    
        // msg: { type: "offer"/"answer"/"ice", roomId: "xxx", payload: {...} }
    
        if (msg.type === "offer") {
            console.log("Got offer from remote:", msg.payload);
            pc.setRemoteDescription(new RTCSessionDescription(msg.payload)).then(async () => {
                const answer = await pc.createAnswer();
                await pc.setLocalDescription(answer);
                // Send answer
                signalingSocket.send(JSON.stringify({ 
                    type: "answer", 
                    roomId: msg.roomId, 
                    payload: pc.localDescription 
                }));
            });
    
        } else if (msg.type === "answer") {
            console.log("Got answer from remote:", msg.payload);
            pc.setRemoteDescription(new RTCSessionDescription(msg.payload));
    
        } else if (msg.type === "ice") {
            console.log("Got ICE from remote:", msg.payload);
            pc.addIceCandidate(new RTCIceCandidate(msg.payload));
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
            console.log("Got local ICE, sending to signaling");
            signalingSocket.send(JSON.stringify({ ice: event.candidate }));
        }
    };

    callButton.disabled = false;
}

async function call() {
    // create offer SDP
    const offer = await pc.createOffer();
    await pc.setLocalDescription(offer);

    console.log("Sending Offer to server via HTTP");
    signalingSocket.send(JSON.stringify({
        type: "offer",
        roomId: "main",
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