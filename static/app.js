// Connection Type Selection
const connectionTypeSelect = document.getElementById('connectionType');
const sfuControls = document.getElementById('sfuControls');

// Show/Hide Controls Based on Connection Type
connectionTypeSelect.onchange = () => {
    if (connectionTypeSelect.value === 'sfu') {
        sfuControls.style.display = 'flex';
        // Disable P2P controls
        document.getElementById('videoSource').disabled = true;
        document.getElementById('startButton').disabled = true;
        document.getElementById('callButton').disabled = true;
        document.getElementById('muteButton').disabled = true;
        document.getElementById('cameraButton').disabled = true;
        document.getElementById('hangupButton').disabled = true;
    } else {
        sfuControls.style.display = 'none';
        // Enable P2P controls
        document.getElementById('videoSource').disabled = false;
        document.getElementById('startButton').disabled = false;
        document.getElementById('callButton').disabled = false;
        document.getElementById('muteButton').disabled = false;
        document.getElementById('cameraButton').disabled = false;
        document.getElementById('hangupButton').disabled = false;
    }
};

// Initialize variables for P2P
let p2pLocalVideo = document.getElementById('localVideo');
let p2pRemoteVideo = document.getElementById('remoteVideo');
let p2pStartButton = document.getElementById('startButton');
let p2pCallButton = document.getElementById('callButton');
let p2pMuteButton = document.getElementById('muteButton');
let p2pCameraButton = document.getElementById('cameraButton');
let p2pHangupButton = document.getElementById('hangupButton');
let p2pVideoSourceSelect = document.getElementById('videoSource');

let p2pLocalStream = null;
let p2pPc = null;
let p2pSignalingSocket = null;

let p2pIsMuted = false;
let p2pIsCameraOff = false;

p2pStartButton.onclick = startP2P;
p2pCallButton.onclick = callP2P;
p2pMuteButton.onclick = toggleMuteP2P;
p2pCameraButton.onclick = toggleCameraP2P;
p2pHangupButton.onclick = hangUpP2P;

// Initialize variables for SFU
let sfuPeerConnection = null;
let sfuSignalingSocket = null;
let sfuClientId = null;
let sfuInternalChannel = null;
let videoObserver = null;

// SFU Buttons
const btnStartH264 = document.getElementById('btnStartH264');
const btnStartVP9 = document.getElementById('btnStartVP9');
const btnViewOnly = document.getElementById('btnViewOnly');
const btnShareScreen = document.getElementById('btnShareScreen');
const btnToggleMic = document.getElementById('btnToggleMic');
const btnToggleCam = document.getElementById('btnToggleCam');
const btnStats = document.getElementById('btnStats');

btnStartH264.onclick = () => startSFU('h264', false);
btnStartVP9.onclick = () => startSFU('vp9', false);
btnViewOnly.onclick = () => startSFU(null, true);
btnShareScreen.onclick = shareScreen;
btnToggleMic.onclick = toggleMicSFU;
btnToggleCam.onclick = toggleCamSFU;
btnStats.onclick = toggleStats;

// Common Elements
let statsLocal = document.getElementById('stats-local');
let clientIdDisplay = document.getElementById('clientid');
let networkStatus = document.getElementById('network');

// ========================= P2P Functions =========================

// Start P2P Connection
async function startP2P() {
    // Select media source
    let source = p2pVideoSourceSelect.value; // "camera" or "screen"

    try {
        if (source === 'camera') {
            p2pLocalStream = await navigator.mediaDevices.getUserMedia({ video: true, audio: true });
        } else if (source === 'screen') {
            p2pLocalStream = await navigator.mediaDevices.getDisplayMedia({ video: true, audio: true });
        }
    } catch (err) {
        console.error("Failed to get media:", err);
        return;
    }

    p2pLocalVideo.srcObject = p2pLocalStream;

    // Initialize WebSocket for signaling
    let wsProtocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
    let baseHost = window.location.host;
    const signalingUrl = `${wsProtocol}//${baseHost}/signal`;
    p2pSignalingSocket = new WebSocket(signalingUrl);

    p2pSignalingSocket.onopen = () => {
        console.log("P2P WebSocket connected");
        // Join the room
        p2pSignalingSocket.send(JSON.stringify({ type: "join", roomId: "main" }));
    };

    p2pSignalingSocket.onmessage = (event) => {
        const msg = JSON.parse(event.data);
        if (msg.type === "offer") {
            handleOfferP2P(msg.payload);
        } else if (msg.type === "answer") {
            handleAnswerP2P(msg.payload);
        } else if (msg.type === "ice") {
            handleRemoteICEP2P(msg.payload);
        }
    };

    p2pSignalingSocket.onerror = (err) => {
        console.error("P2P WebSocket error:", err);   
    };

    p2pSignalingSocket.onclose = () => {
        console.log("P2P WebSocket closed");
    };

    // Create RTCPeerConnection
    p2pPc = new RTCPeerConnection({
        iceServers: [{ urls: "stun:stun.l.google.com:19302"}]
    });

    // Add tracks to RTCPeerConnection
    p2pLocalStream.getTracks().forEach(track => p2pPc.addTrack(track, p2pLocalStream));

    // Handle incoming tracks
    p2pPc.ontrack = (event) => {
        console.log("P2P Got remote track:", event.track);
        p2pRemoteVideo.srcObject = event.streams[0];
    };

    // Send ICE candidates
    p2pPc.onicecandidate = (event) => {
        if (event.candidate) {
            console.log("P2P Sending ICE candidate to remote");
            p2pSignalingSocket.send(JSON.stringify({
                type: "ice",
                roomId: "main",
                payload: event.candidate
            }));
        }
    };

    // Enable P2P controls
    p2pCallButton.disabled = false;
    p2pMuteButton.disabled = false;
    p2pCameraButton.disabled = false;
    p2pHangupButton.disabled = false;
}

// Call P2P Connection
async function callP2P() {
    console.log("P2P Creating offer");
    const offer = await p2pPc.createOffer();
    await p2pPc.setLocalDescription(offer);

    console.log("P2P Sending Offer to server via WebSocket");
    p2pSignalingSocket.send(JSON.stringify({
        type: "offer",
        roomId: "main",
        payload: offer
    }));
}

// Handle P2P Offer
async function handleOfferP2P(offer) {
    console.log("P2P Received offer");
    await p2pPc.setRemoteDescription(new RTCSessionDescription(offer));
    const answer = await p2pPc.createAnswer();
    await p2pPc.setLocalDescription(answer);

    console.log("P2P Sending answer back to initiator");
    p2pSignalingSocket.send(JSON.stringify({
        type: "answer",
        roomId: "main",
        payload: answer
    }));
}

// Handle P2P Answer
async function handleAnswerP2P(answer) {
    console.log("P2P Received answer");
    await p2pPc.setRemoteDescription(new RTCSessionDescription(answer));
}

// Handle Remote ICE Candidate for P2P
function handleRemoteICEP2P(candidate) {
    console.log("P2P Received ICE candidate");
    p2pPc.addIceCandidate(new RTCIceCandidate(candidate)).catch(e => console.error("P2P Error adding ICE:", e));
}

// Toggle Microphone for P2P
function toggleMuteP2P() {
    if (p2pLocalStream) {
        p2pIsMuted = !p2pIsMuted;
        p2pLocalStream.getAudioTracks().forEach(track => track.enabled = !p2pIsMuted);
        p2pMuteButton.textContent = p2pIsMuted ? 'Unmute Microphone' : 'Mute Microphone';
    }
}

// Toggle Camera for P2P
function toggleCameraP2P() {
    if (p2pLocalStream) {
        p2pIsCameraOff = !p2pIsCameraOff;
        p2pLocalStream.getVideoTracks().forEach(track => track.enabled = !p2pIsCameraOff);
        p2pCameraButton.textContent = p2pIsCameraOff ? 'Turn On Camera' : 'Turn Off Camera';
    }
}

// Hang Up P2P Call
function hangUpP2P() {
    console.log("P2P Ending call");

    // Close RTCPeerConnection
    if (p2pPc) {
        p2pPc.close();
        p2pPc = null;
    }

    // Close WebSocket
    if (p2pSignalingSocket) {
        p2pSignalingSocket.close();
        p2pSignalingSocket = null;
    }

    // Stop local stream
    if (p2pLocalStream) {
        p2pLocalStream.getTracks().forEach(track => track.stop());
        p2pLocalStream = null;
    }

    // Reset UI
    p2pLocalVideo.srcObject = null;
    p2pRemoteVideo.srcObject = null;
    p2pCallButton.disabled = true;
    p2pMuteButton.disabled = true;
    p2pCameraButton.disabled = true;
    p2pHangupButton.disabled = true;
}

// ========================= SFU Functions =========================

async function startSFU(codec, viewOnly) {
    await startSFUWebSocket();
    if (!viewOnly) {
        const videoConstraints = {
            width: { ideal: 1280 },
            height: { ideal: 720 },
            advanced: [
                { frameRate: { min: 30 }},
                { height: { min: 360 }},
                { width: { min: 720 }},
                { frameRate: { max: 30 }},
                { width: { max: 1280 }},
                { height: { max: 720 }},
                { aspectRatio: { exact: 1.77778 }}
            ]
        };

        const constraints = {
            audio: true,
            video: videoConstraints
        };

        const initStream = await navigator.mediaDevices.getUserMedia(constraints);
        const streamId = initStream.id.replace('{','').replace('}','');
        initStream.getTracks().forEach(track => sfuPeerConnection.addTrack(track, initStream));

        // Display local video
        let container = document.getElementById(`container-${streamId}`);
        if (!container) {
            container = document.createElement("div");
            container.className = "container";
            container.id = `container-${streamId}`;
            document.querySelector('main').appendChild(container);
        }

        let localVideo = document.getElementById(`video-${streamId}`);
        if (!localVideo) {
            localVideo = document.createElement("video");
            localVideo.id = `video-${streamId}`;
            localVideo.autoplay = true;
            localVideo.muted = true;
            container.appendChild(localVideo);
        }

        localVideo.srcObject = initStream;

        // Add audio transceiver
        const audioTcvr = sfuPeerConnection.addTransceiver(initStream.getAudioTracks()[0], {
            direction: 'sendonly',
            streams: [initStream],
            sendEncodings: [{ priority: 'high' }],
        });

        // Set codec preferences if supported
        if (audioTcvr.setCodecPreferences && RTCRtpReceiver.getCapabilities) {
            const audioCodecs = RTCRtpReceiver.getCapabilities('audio').codecs;
            let audioCodecsPref = [];

            if (codec === 'h264') {
                audioCodecsPref = audioCodecs.filter(codec => codec.mimeType === "audio/opus");
            } else if (codec === 'vp9') {
                audioCodecsPref = audioCodecs.filter(codec => codec.mimeType === "audio/opus");
            }

            audioTcvr.setCodecPreferences(audioCodecsPref);
        }

        // Set video codec preferences
        setCodecPreferences(sfuPeerConnection, initStream, codec, "L3T3_KEY");

    } else {
        // View Only Mode
        sfuPeerConnection.addTransceiver('video', { direction: 'recvonly' });
        sfuPeerConnection.addTransceiver('audio', { direction: 'recvonly' });
    }

    // Create and send offer
    const offer = await sfuPeerConnection.createOffer();
    await sfuPeerConnection.setLocalDescription(offer);
    sfuSignalingSocket.send(JSON.stringify({ type: 'offer', data: offer.sdp }));
    console.log("SFU Browser sent offer");

    sfuPeerConnection.onicecandidate = (e) => {
        if (e.candidate) {
            sfuSignalingSocket.send(JSON.stringify({ type: 'candidate', data: e.candidate }));
        }
    };

    sfuPeerConnection.onconnectionstatechange = (e) => {
        console.log("SFU Connection State:", sfuPeerConnection.connectionState);
        if (sfuPeerConnection.connectionState === "connected") {
            monitorStatsSFU();
            monitorBandwidthSFU();
        }
    };
}

// Start SFU WebSocket
async function startSFUWebSocket() {
    let wsProtocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
    let baseHost = window.location.host;
    const signalingUrl = `${wsProtocol}//${baseHost}/sfu`;
    sfuSignalingSocket = new WebSocket(signalingUrl);

    sfuSignalingSocket.onopen = () => {
        console.log("SFU WebSocket connected");
    };

    sfuSignalingSocket.onmessage = async (event) => {
        const msg = JSON.parse(event.data);
        try {
            if (msg.type === 'clientid') {
                sfuClientId = msg.data;
                clientIdDisplay.innerText = `ClientID: ${sfuClientId}`;
            } else if (msg.type === 'network_condition') {
                networkStatus.innerText = msg.data === 0 ? 'Unstable' : 'Stable';
            } else if (msg.type === 'offer') {
                console.log("SFU Client received offer:", msg.data);
                await sfuPeerConnection.setRemoteDescription(new RTCSessionDescription(msg.data));
                const answer = await sfuPeerConnection.createAnswer();
                await sfuPeerConnection.setLocalDescription(answer);
                sfuSignalingSocket.send(JSON.stringify({ type: 'answer', data: answer.sdp }));
                console.log("SFU Client sent answer:", answer);
            } else if (msg.type === 'answer') {
                await sfuPeerConnection.setRemoteDescription(new RTCSessionDescription(msg.data));
                console.log("SFU Client received answer");
            } else if (msg.type === 'candidate') {
                await sfuPeerConnection.addIceCandidate(new RTCIceCandidate(msg.data));
                console.log("SFU Client added ICE candidate");
            } else if (msg.type === 'tracks_added') {
                handleTracksAddedSFU(msg.data);
            } else if (msg.type === 'tracks_available') {
                handleTracksAvailableSFU(msg.data);
            } else if (msg.type === 'allow_renegotiation') {
                if (msg.data && negotiationNeededSFU) {
                    negotiateSFU();
                }
            } else if (msg.type === 'track_stats') {
                updateTrackStatsSFU(msg.data);
            }
        } catch (error) {
            console.error("SFU Error handling message:", error);
        }
    };

    sfuSignalingSocket.onclose = () => {
        console.log("SFU WebSocket closed");
    };

    // Create RTCPeerConnection for SFU
    sfuPeerConnection = new RTCPeerConnection({
        iceServers: [{ urls: "stun:stun.l.google.com:19302"}]
    });

    sfuPeerConnection.ondatachannel = (e) => {
        if (e.channel.label === "internal") {
            sfuInternalChannel = e.channel;
            videoObserver = new InliveVideoObserver(sfuInternalChannel, 1000);

            sfuInternalChannel.addEventListener('message', (e) => {
                const msg = JSON.parse(e.data);
                if (msg.type === 'vad_started' || msg.type === 'vad_ended') {
                    updateVoiceDetectedSFU(msg);
                }
            });
        }
    };

    sfuPeerConnection.ontrack = (e) => {
        e.streams.forEach((stream) => {
            console.log("SFU ontrack:", stream, e.track);
            let container = document.getElementById(`container-${stream.id}`);
            if (!container) {
                container = document.createElement("div");
                container.className = "container";
                container.id = `container-${stream.id}`;
                document.querySelector('main').appendChild(container);
            }

            let remoteVideo = document.getElementById(`video-${stream.id}`);
            if (!remoteVideo) {
                remoteVideo = document.createElement("video");
                remoteVideo.id = `video-${stream.id}`;
                remoteVideo.autoplay = true;
                container.appendChild(remoteVideo);
                if (videoObserver) {
                    videoObserver.observe(remoteVideo);
                }
            }

            remoteVideo.srcObject = stream;

            stream.onremovetrack = (e) => {
                console.log("SFU onremovetrack:", stream, e.track);
                remoteVideo.srcObject = null;
                remoteVideo.remove();
                container.remove();
                if (videoObserver) {
                    videoObserver.unobserve(remoteVideo);
                }
            };
        });
    };
}

// Handle Tracks Added (SFU)
function handleTracksAddedSFU(tracksAdded) {
    console.log("SFU Tracks Added:", tracksAdded);
    const trackType = {};
    Object.keys(tracksAdded).forEach(uid => {
        trackType[uid] = "media";
    });
    sfuSignalingSocket.send(JSON.stringify({ type: 'tracks_added', data: trackType }));
}

// Handle Tracks Available (SFU)
function handleTracksAvailableSFU(availableTracks) {
    console.log("SFU Tracks Available:", availableTracks);
    const subTracks = [];
    Object.keys(availableTracks).forEach(uid => {
        const track = availableTracks[uid];
        subTracks.push({
            client_id: track.client_id,
            track_id: track.track_id,
        });
    });
    sfuSignalingSocket.send(JSON.stringify({ type: 'subscribe_tracks', data: subTracks }));
}

// Set Codec Preferences (SFU)
function setCodecPreferences(peerConnection, stream, codec, scalabilityMode) {
    const isFirefox = navigator.userAgent.includes("Firefox");
    const isSimulcast = document.querySelector("#simulcast").checked;
    const isSvc = document.querySelector("#svc").checked;
    const maxBitrate = parseInt(document.getElementById("maxBitrate").value, 10);

    if (codec === 'vp9' && !isFirefox) {
        let videoTcvr = null;
        console.log("Simulcast:", isSimulcast);

        if (!isSimulcast) {
            videoTcvr = peerConnection.addTransceiver(stream.getVideoTracks()[0], {
                direction: 'sendonly',
                streams: [stream],
                sendEncodings: [
                    {
                        maxBitrate: maxBitrate,
                        scalabilityMode: isSvc ? scalabilityMode : 'L1T1'
                    },
                ]
            });
        } else {
            videoTcvr = peerConnection.addTransceiver(stream.getVideoTracks()[0], {
                direction: 'sendonly',
                streams: [stream],
                sendEncodings: [
                    {
                        rid: 'high',
                        maxBitrate: maxBitrate,
                        maxFramerate: 30,
                        scalabilityMode: isSvc ? scalabilityMode : 'L1T1'
                    },
                    {
                        rid: 'mid',
                        scaleResolutionDownBy: 2.0,
                        maxFramerate: 30,
                        maxBitrate: maxBitrate / 2,
                        scalabilityMode: isSvc ? scalabilityMode : 'L1T1'
                    },
                    {
                        rid: 'low',
                        scaleResolutionDownBy: 4.0,
                        maxBitrate: maxBitrate / 4,
                        maxFramerate: 30,
                        scalabilityMode: isSvc ? scalabilityMode : 'L1T1'
                    }
                ]
            });
        }

        const codecs = RTCRtpReceiver.getCapabilities('video').codecs;
        let vp9_codecs = codecs.filter(codec => codec.mimeType === "video/VP9");
        vp9_codecs = vp9_codecs.concat(codecs.filter(codec => codec.mimeType !== "video/VP9"));

        if (videoTcvr.setCodecPreferences) {
            videoTcvr.setCodecPreferences(vp9_codecs);
        }
    } else {
        let videoTcvr = null;
        if (!isSimulcast) {
            videoTcvr = peerConnection.addTransceiver(stream.getVideoTracks()[0], {
                direction: 'sendonly',
                streams: [stream],
                sendEncodings: [
                    { maxBitrate: 1200 * 1000 }
                ]
            });
        } else {
            videoTcvr = peerConnection.addTransceiver(stream.getVideoTracks()[0], {
                direction: 'sendonly',
                streams: [stream],
                sendEncodings: [
                    {
                        rid: 'high',
                        maxBitrate: 1200 * 1000,
                        maxFramerate: 30,
                    },
                    {
                        rid: 'mid',
                        scaleResolutionDownBy: 2.0,
                        maxFramerate: 30,
                        maxBitrate: 500 * 1000,
                    },
                    {
                        rid: 'low',
                        scaleResolutionDownBy: 4.0,
                        maxBitrate: 150 * 1000,
                        maxFramerate: 30,
                    }
                ]
            });
        }

        const codecs = RTCRtpReceiver.getCapabilities('video').codecs;
        let h264Codecs = codecs.filter(codec => codec.mimeType === "video/H264");
        h264Codecs = h264Codecs.concat(codecs.filter(codec => codec.mimeType !== "video/H264"));

        if (videoTcvr.setCodecPreferences) {
            videoTcvr.setCodecPreferences(h264Codecs);
        } else {
            console.log("setCodecPreferences not supported");
        }
    }
}

// Toggle Microphone for SFU
let sfuMutedMic = false;
function toggleMicSFU(e) {
    if (sfuPeerConnection) {
        sfuPeerConnection.getSenders().forEach(sender => {
            if (sender.track && sender.track.kind === 'audio') {
                sender.track.enabled = !sfuMutedMic;
                sfuMutedMic = !sfuMutedMic;
                btnToggleMic.innerText = sfuMutedMic ? 'Unmute Mic' : 'Mute Mic';
            }
        });
    }
}

// Toggle Camera for SFU
let sfuMutedCam = false;
function toggleCamSFU(e) {
    if (sfuPeerConnection) {
        sfuPeerConnection.getSenders().forEach(sender => {
            if (sender.track && sender.track.kind === 'video') {
                sender.track.enabled = !sfuMutedCam;
                sfuMutedCam = !sfuMutedCam;
                btnToggleCam.innerText = sfuMutedCam ? 'Unmute Cam' : 'Mute Cam';
            }
        });
    }
}

// Hang Up SFU Call
function hangUpSFU() {
    console.log("SFU Ending call");

    if (sfuPeerConnection) {
        sfuPeerConnection.close();
        sfuPeerConnection = null;
    }

    if (sfuSignalingSocket) {
        sfuSignalingSocket.close();
        sfuSignalingSocket = null;
    }

    // Stop all local streams
    // Assuming you have references to all local streams
    // This part can be expanded based on implementation

    // Reset UI if necessary
}

// Toggle Stats Display
function toggleStats() {
    const statsEls = document.querySelectorAll(".stats");
    statsEls.forEach(el => {
        el.style.display = el.style.display === "none" ? "flex" : "none";
    });
}

// Monitor Stats for SFU
async function monitorStatsSFU() {
    while (sfuPeerConnection && sfuPeerConnection.connectionState === "connected") {
        const stats = await sfuPeerConnection.getStats();

        stats.forEach(report => {
            // Process stats as needed
            // This can be customized based on your requirements
        });

        await sleep(1000);
    }
}

// Monitor Bandwidth for SFU
async function monitorBandwidthSFU() {
    // Implement bandwidth monitoring if needed
    // This can be customized based on your requirements
}

// SFU Negotiation
let negotiationNeededSFU = false;
const negotiateSFU = async () => {
    console.log("SFU Negotiating");
    const offer = await sfuPeerConnection.createOffer();
    await sfuPeerConnection.setLocalDescription(offer);
    sfuSignalingSocket.send(JSON.stringify({ type: 'offer', data: offer.sdp }));
};

// SFU Share Screen Function
let sfuScreenStream = null;
async function shareScreen() {
    if (sfuScreenStream) {
        const trackIds = sfuScreenStream.getTracks().map(track => track.id);
        sfuPeerConnection.getSenders().forEach(sender => {
            if (sender.track && trackIds.includes(sender.track.id)) {
                sender.track.stop();
                sfuPeerConnection.removeTrack(sender);
            }
        });

        document.getElementById(`container-${sfuScreenStream.id}`).remove();
        sfuScreenStream = null;
        negotiateSFU();
        return;
    }

    sfuScreenStream = await navigator.mediaDevices.getDisplayMedia({
        video: true,
        audio: true
    });

    const videoTrack = sfuScreenStream.getVideoTracks()[0];
    const audioTrack = sfuScreenStream.getAudioTracks()[0];

    let tscvAudio = null;
    let tscvVideo = null;

    if (audioTrack) {
        tscvAudio = sfuPeerConnection.addTransceiver(audioTrack, {
            direction: 'sendonly',
            streams: [sfuScreenStream],
            sendEncodings: [{ priority: 'high' }],
        });
    }

    setCodecPreferences(sfuPeerConnection, sfuScreenStream, 'vp9', "L1T3");

    const container = document.createElement("div");
    container.className = "container";
    container.id = `container-${sfuScreenStream.id}`;

    const video = document.createElement("video");
    video.id = `video-${videoTrack.id}`;
    video.autoplay = true;
    video.srcObject = sfuScreenStream;
    container.appendChild(video);

    document.querySelector('main').appendChild(container);

    videoTrack.addEventListener('ended', () => {
        console.log('SFU Screen Sharing Ended');
        document.querySelector('main').removeChild(container);
        sfuPeerConnection.removeTrack(tscvVideo.sender);
        sfuPeerConnection.removeTrack(tscvAudio.sender);
        negotiateSFU();
    });

    negotiateSFU();
}

// Update Voice Detection for SFU
function updateVoiceDetectedSFU(vad) {
    const streamId = vad.data.streamID;
    const videoEl = document.getElementById(`video-${streamId}`);
    const container = document.getElementById(`container-${streamId}`);
    if (!videoEl) {
        console.log("SFU Video element not found:", streamId);
        return;
    }

    if (vad.type === 'vad_ended') {
        videoEl.style.border = "none";
        container.style.margin = "0";
    } else {
        videoEl.style.border = "5px solid green";
        container.style.margin = "-5px";
    }

    let vadEl = document.getElementById(`vad-${streamId}`);
    if (!vadEl) {
        vadEl = document.createElement("div");
        vadEl.id = `vad-${streamId}`;
        container.appendChild(vadEl);
    }

    if (vad.data.audioLevels !== null) {
        vadEl.innerText = Math.floor(vad.data.audioLevels.reduce((sum, value) => sum + value.audioLevel, 0) / vad.data.audioLevels.length);
    } else {
        vadEl.innerText = "0";
    }
}

// Update Track Stats for SFU
function updateTrackStatsSFU(trackStats) {
    const sentStats = trackStats.sent_track_stats;
    sentStats.forEach(stat => {
        const statsEl = document.getElementById(`stats-${stat.id}`);
        if (!statsEl) return;

        let trackStatsEl = statsEl.querySelector(".track-stats");
        if (!trackStatsEl) {
            trackStatsEl = document.createElement("div");
            trackStatsEl.className = "track-stats";
            statsEl.appendChild(trackStatsEl);
        }

        const statsText = `
            <p>Packet Loss Ratio: ${Math.round(stat.fraction_lost * 100) / 100}</p>
        `;
        trackStatsEl.innerHTML = statsText;
    });

    // Handle received stats similarly
}

// ========================= Utility Functions =========================

// Utility Sleep Function
const sleep = (delay) => new Promise((resolve) => setTimeout(resolve, delay));
