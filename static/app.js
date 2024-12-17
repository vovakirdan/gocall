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
    };

    signalingSocket.onmessage = (event) => {
        const message = JSON.parse(event.data);

        if (message.ice) {
            console.log("Got ICE from signaling:", message.ice);
            pc.addIceCandidate(message.ice).catch(e=>console.error(e));
        }

        // will handle offer/answer
        // todo
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
    let response = await fetch(webrtcUrl, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(offer)
    });

    if (!response.ok) {
        console.error("Failed to get answer from server");
        return;
    }

    let answer = await response.json();
    console.log("Got Answer from server:", answer);

    await pc.setRemoteDescription(answer);
    console.log("Remote SDP set successfully");
}