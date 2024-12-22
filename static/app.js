// Подключение к SFU-серверу и работа с несколькими участниками
let wsProtocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
let baseHost = window.location.host;

// Подключение к SFU
const sfuUrl = wsProtocol + '//' + baseHost + '/ws';

// Генерируем уникальный self_id для этого клиента
const selfId = "user_" + Math.floor(Math.random() * 10000);

// Комната по умолчанию
const roomId = "main";

let localVideo = document.getElementById('localVideo');
let remoteVideosContainer = document.getElementById('remoteVideos');
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

// Хранить ссылки на видео по stream.id, чтобы не создавать дубликаты
let remoteStreams = {};

startButton.onclick = start;
callButton.onclick = call;
muteButton.onclick = toggleMute;
cameraButton.onclick = toggleCamera;
hangupButton.onclick = hangUp;

async function start() {
    if (!selfId || !roomId) {
        console.error("selfId or roomId is not defined");
        return;
    }    
    // Источник: камера или экран
    let source = videoSourceSelect.value;

    try {
        if (source === 'camera') {
            localStream = await navigator.mediaDevices.getUserMedia({ video: true, audio: true });
        } else if (source === 'screen') {
            localStream = await navigator.mediaDevices.getDisplayMedia({ video: true, audio: true });
        }
    } catch (err) {
        console.error("Failed to get media:", err);
        return;
    }

    localVideo.srcObject = localStream;

    // Подключаемся к SFU
    sfuSocket = new WebSocket(sfuUrl);

    sfuSocket.onopen = () => {
        console.log("SFU WebSocket connected");
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

    // Создаём RTCPeerConnection
    pc = new RTCPeerConnection({
        iceServers: [{ urls: "stun:stun.l.google.com:19302"}]
    });

    // Добавляем локальные треки в PeerConnection
    localStream.getTracks().forEach(track => pc.addTrack(track, localStream));

    // Обработка входящих треков
    pc.ontrack = (event) => {
        console.log("Got remote track:", event.track);
        let stream = event.streams[0];
        if (!remoteStreams[stream.id]) {
            // Создаём новый video-элемент для нового потока
            let video = document.createElement('video');
            video.autoplay = true;
            video.playsinline = true;
            video.srcObject = stream;
            remoteVideosContainer.appendChild(video);
            remoteStreams[stream.id] = video;
        }
    };

    // Отправляем ICE-кандидаты в SFU
    pc.onicecandidate = (event) => {
        if (event.candidate) {
            let iceMsg = {
                event: "ice-candidate",
                data: {
                    self_id: selfId,
                    candidate: event.candidate,
                    room_id: roomId
                }
            };
            console.log("Sending ICE candidate:", iceMsg);
            sfuSocket.send(JSON.stringify(iceMsg));
        }
    };    

    pc.onnegotiationneeded = async () => {
        console.log("Negotiation needed, signaling state:", pc.signalingState);
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

let offerQueue = [];

let pendingCandidates = [];

function handleRemoteICE(candidate) {
    console.log("Received ICE candidate");
    if (pc.remoteDescription && pc.remoteDescription.type) {
        // Если удалённое описание уже установлено, добавляем кандидата
        pc.addIceCandidate(new RTCIceCandidate(candidate))
            .then(() => console.log("Added ICE candidate"))
            .catch((err) => console.error("Error adding ICE:", err));
    } else {
        // Сохраняем кандидата в очередь
        console.log("Queuing ICE candidate until remote description is set");
        pendingCandidates.push(candidate);
    }
}

async function handleOffer(offer) {
    console.log("Received offer");
    if (pc.signalingState !== "stable") {
        console.warn("Queuing offer due to signaling state:", pc.signalingState);
        offerQueue.push(offer);
        return;
    }

    try {
        await pc.setRemoteDescription(new RTCSessionDescription(offer));
        console.log("Remote description set");

        // Обработка очереди ICE-кандидатов
        while (pendingCandidates.length > 0) {
            const candidate = pendingCandidates.shift();
            try {
                await pc.addIceCandidate(new RTCIceCandidate(candidate));
                console.log("Processed pending ICE candidate");
            } catch (err) {
                console.error("Error adding pending ICE candidate:", err);
            }
        }

        const answer = await pc.createAnswer();
        await pc.setLocalDescription(answer);
        console.log("Local description set and answer created");

        const answerMsg = {
            event: "answer",
            data: {
                self_id: selfId,
                room_id: roomId,
                answer: answer
            }
        };
        sfuSocket.send(JSON.stringify(answerMsg));

        // После обработки текущего предложения, обработать очередь
        if (offerQueue.length > 0) {
            console.log("Processing queued offer");
            const nextOffer = offerQueue.shift();
            await handleOffer(nextOffer);
        }
    } catch (err) {
        console.error("Failed to handle offer:", err);
    }
}

async function handleAnswer(answer) {
    console.log("Received answer");
    if (pc.signalingState === "have-local-offer") {
        await pc.setRemoteDescription(new RTCSessionDescription(answer))
            .then(() => console.log("Remote description set"))
            .catch(err => console.error("Failed to set remote description:", err));
    } else {
        console.warn(`Skipping setRemoteDescription in handleAnswer: signaling state is ${pc.signalingState}`);
    }
}

function toggleMute() {
    if (localStream) {
        isMuted = !isMuted;
        localStream.getAudioTracks().forEach(track => track.enabled = !isMuted);
        muteButton.textContent = isMuted ? 'Enable microphone' : 'Disable microphone';
    }
}

function toggleCamera() {
    if (localStream) {
        isCameraOff = !isCameraOff;
        localStream.getVideoTracks().forEach(track => track.enabled = !isCameraOff);
        cameraButton.textContent = isCameraOff ? 'Turn on camera/screen' : 'Turn off camera/screen';
    }
}

function hangUp() {
    console.log("Ending call");

    // Закрываем PeerConnection
    if (pc) {
        pc.close();
        pc = null;
    }

    // Закрываем WebSocket
    if (sfuSocket) {
        sfuSocket.close();
        sfuSocket = null;
    }

    // Останавливаем локальный стрим
    if (localStream) {
        localStream.getTracks().forEach(track => track.stop());
        localStream = null;
    }

    // Чистим интерфейс
    localVideo.srcObject = null;
    callButton.disabled = true;
    muteButton.disabled = true;
    cameraButton.disabled = true;
    hangupButton.disabled = true;

    // Удаляем все удалённые видео
    for (let streamId in remoteStreams) {
        let video = remoteStreams[streamId];
        remoteVideosContainer.removeChild(video);
    }
    remoteStreams = {};
}
