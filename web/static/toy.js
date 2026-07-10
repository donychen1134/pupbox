const SILENT_WAV_DATA_URI = "data:audio/wav;base64,UklGRiYAAABXQVZFZm10IBAAAAABAAEAgD4AAAB9AAACABAAZGF0YQIAAAAAAA==";
const MAX_RECORDING_MS = 12000;

const state = {
  recorder: null,
  recognition: null,
  recording: false,
  speechText: "",
  speechSent: false,
  health: null,
  feedbackContext: null,
  thinkingTimer: null,
  thinkingNodes: new Set(),
  accessToken: "",
  recordingStartedAt: 0,
  recordingTimer: null,
  audioPlayer: null,
  audioObjectURL: "",
  audioUnlocked: false,
  busy: false,
  sessionID: "",
};

const els = {
  modePill: document.querySelector("#modePill"),
  recordButton: document.querySelector("#recordButton"),
  recordLabel: document.querySelector("#recordLabel"),
  toyState: document.querySelector("#toyState"),
  voiceNote: document.querySelector("#voiceNote"),
  voiceLevel: document.querySelector("#voiceLevel"),
};

init();

async function init() {
  state.accessToken = loadAccessToken();
  state.sessionID = loadSessionID("pupbox.toySessionId");
  bindEvents();
  await loadHealth();
}

function bindEvents() {
  els.recordButton.addEventListener("pointerdown", startRecording);
  els.recordButton.addEventListener("pointerup", stopRecording);
  els.recordButton.addEventListener("pointercancel", stopRecording);
  els.recordButton.addEventListener("pointerleave", () => {
    if (state.recording) stopRecording();
  });
}

async function loadHealth() {
  try {
    const health = await fetchJSON("/api/health");
    state.health = health;
    els.modePill.textContent = "在线";
    if (hasServerVoice()) {
      els.voiceNote.textContent = "豆豆准备好啦";
    } else if (browserSpeechRecognition()) {
      els.voiceNote.textContent = "按住说话，松开发送";
    } else {
      els.voiceNote.textContent = "当前浏览器不支持听写";
    }
  } catch (error) {
    if (error.status === 401) {
      els.modePill.textContent = "授权";
      els.toyState.textContent = "找爸爸妈妈";
      els.voiceNote.textContent = "需要家长授权";
    } else {
      els.modePill.textContent = "离线";
      els.voiceNote.textContent = "服务未连接";
    }
  }
}

async function startRecording(event) {
  event.preventDefault();
  if (state.busy || state.recording) return;
  unlockAudioPlayback();
  unlockFeedbackAudio();

  if (shouldUseBrowserSpeech()) {
    startBrowserSpeech(event);
    return;
  }

  if (!hasServerVoice()) {
    await speakStatus("豆豆还没连上语音。请让爸爸妈妈看一下设置。", "找爸爸妈妈");
    return;
  }

  if (!navigator.mediaDevices?.getUserMedia) {
    await speakStatus("这个浏览器不能录音。", "找爸爸妈妈");
    return;
  }

  try {
    const stream = await navigator.mediaDevices.getUserMedia({ audio: true });
    const recorder = await createWavRecorder(stream);
    state.recorder = recorder;
    state.recording = true;
    startRecordingMeter(recorder);
    setPhase("listening", "豆豆听着呢", "松开发送");
    playCue("listening");
    els.recordButton.setPointerCapture?.(event.pointerId);
  } catch (error) {
    state.recording = false;
    stopRecordingMeter();
    await speakStatus("豆豆没有听清。再试一次吧。", "没有听清");
  }
}

function stopRecording() {
  if (state.recognition) {
    state.recording = false;
    stopRecordingMeter();
    setPhase("thinking", "豆豆听到啦", "想一想");
    state.recognition.stop();
    return;
  }

  if (!state.recording || !state.recorder) return;
  const recorder = state.recorder;
  state.recorder = null;
  state.recording = false;
  stopRecordingMeter();
  setPhase("thinking", "豆豆听到啦", "想一想");
  recorder.stop().then((recording) => {
    sendRecording(recording.blob, recording.mimeType, recording.filename, recording.durationMs, recording.peakLevel);
  }).catch(() => speakStatus("豆豆没有听清。再试一次吧。", "没有听清"));
}

function startBrowserSpeech(event) {
  const SpeechRecognition = browserSpeechRecognition();
  if (!SpeechRecognition) return;

  const recognition = new SpeechRecognition();
  recognition.lang = "zh-CN";
  recognition.interimResults = true;
  recognition.continuous = true;
  recognition.maxAlternatives = 1;

  state.recognition = recognition;
  state.speechText = "";
  state.speechSent = false;
  state.recording = true;
  setPhase("listening", "豆豆听着呢", "松开发送");
  playCue("listening");
  els.recordButton.setPointerCapture?.(event.pointerId);

  recognition.addEventListener("result", (resultEvent) => {
    for (let i = resultEvent.resultIndex; i < resultEvent.results.length; i += 1) {
      const transcript = resultEvent.results[i][0]?.transcript?.trim() || "";
      if (resultEvent.results[i].isFinal) {
        state.speechText = `${state.speechText} ${transcript}`.trim();
      }
    }
  });

  recognition.addEventListener("error", async () => {
    state.speechSent = true;
    cleanupSpeech();
    await speakStatus("豆豆没有听清。再说一次吧。", "没有听清");
  });

  recognition.addEventListener("end", () => {
    const text = state.speechText.trim();
    cleanupSpeech();
    if (!state.speechSent) {
      state.speechSent = true;
      sendRecognizedText(text || "嗯嗯");
    }
  });

  try {
    recognition.start();
  } catch (error) {
    cleanupSpeech();
  }
}

function cleanupSpeech() {
  state.recording = false;
  state.recognition = null;
  stopRecordingMeter();
}

function startRecordingMeter(recorder) {
  stopRecordingMeter();
  state.recordingStartedAt = Date.now();
  updateRecordingMeter(recorder);
  state.recordingTimer = window.setInterval(() => updateRecordingMeter(recorder), 120);
}

function updateRecordingMeter(recorder) {
  const durationMS = Date.now() - state.recordingStartedAt;
  if (durationMS >= MAX_RECORDING_MS) {
    stopRecording();
    return;
  }
  const seconds = Math.max(0, durationMS / 1000);
  const level = recorder?.level?.() || 0;
  const fill = Math.max(0.04, Math.min(1, level * 8));
  document.body.style.setProperty("--voice-level", fill.toFixed(3));
  if (durationMS > 900 && level < 0.012) {
    els.toyState.textContent = `靠近一点 ${seconds.toFixed(1)}秒`;
  } else {
    els.toyState.textContent = `豆豆听着呢 ${seconds.toFixed(1)}秒`;
  }
  els.recordLabel.textContent = durationMS < 700 ? "继续说" : "松开发送";
}

function stopRecordingMeter() {
  if (state.recordingTimer) {
    window.clearInterval(state.recordingTimer);
    state.recordingTimer = null;
  }
  state.recordingStartedAt = 0;
  document.body.style.setProperty("--voice-level", "0");
}

async function sendRecognizedText(text) {
  setPhase("thinking", "豆豆想一想", "等一下");
  try {
    const response = await postJSON("/api/chat?tts=off", { text });
    await handleDogResponse(response);
  } catch (error) {
    await speakStatus(
      error.status === 401 ? "请爸爸妈妈帮豆豆设置一下。" : "豆豆这里出了一点小问题。",
      error.status === 401 ? "找爸爸妈妈" : "出错了",
    );
  }
}

async function sendRecording(blob, mimeType, filename, durationMs, peakLevel) {
  if (!blob || blob.size === 0) return;
  if (durationMs && durationMs < 260) {
    await speakStatus("豆豆没有听清。再说长一点点。", "没有听清");
    return;
  }
  const form = new FormData();
  form.append("audio", blob, filename || "recording.wav");
  if (durationMs) form.append("duration_ms", String(durationMs));
  if (peakLevel) form.append("peak_level", String(peakLevel));

  setPhase("thinking", "豆豆想一想", "等一下");
  try {
    const response = await fetch("/api/voice?tts=off", { method: "POST", headers: authHeaders(), body: form });
    if (!response.ok) throw await responseError(response);
    const payload = await response.json();
    await handleDogResponse(payload);
  } catch (error) {
    await speakStatus(
      error.status === 401 ? "请爸爸妈妈帮豆豆设置一下。" : "豆豆这里出了一点小问题。",
      error.status === 401 ? "找爸爸妈妈" : "出错了",
    );
  }
}

async function handleDogResponse(payload) {
  const reply = payload.reply || "豆豆没有想好。";
  const speakingState = payload.safety?.triggered ? "找爸爸妈妈" : actionLabel(payload);
  let played = false;
  if (payload.audio_base64 && payload.audio_mime) {
    setPhase("speaking", speakingState, "等一下");
    played = await playBase64Audio(payload.audio_base64, payload.audio_mime);
  } else if (hasServerVoice() && state.health?.tts_streaming) {
    played = await playSpeechStream(reply, () => {
      setPhase("speaking", speakingState, "等一下");
    });
  }
  if (!played) {
    setPhase("speaking", speakingState, "等一下");
    await speak(reply);
  }
  setPhase("idle", "按住小爪子说话", "按住说话");
}

async function playSpeechStream(text, onFirstAudio) {
  try {
    const response = await fetch("/api/speech-stream", {
      method: "POST",
      headers: authHeaders({ "Content-Type": "application/json" }),
      body: JSON.stringify({ text }),
    });
    if (!response.ok || !response.body) return false;

    const reader = response.body.getReader();
    const decoder = new TextDecoder();
    const completePlayback = [];
    let pcmPlayer = null;
    let buffer = "";
    let started = false;

    const processLine = (line) => {
      if (!line.trim()) return;
      const event = JSON.parse(line);
      if (event.type !== "audio" || !event.audio_base64) return;
      if (!started) {
        started = true;
        onFirstAudio?.();
      }
      if (event.audio_mime === "audio/pcm") {
        if (!pcmPlayer) pcmPlayer = createPCMPlayer(event.sample_rate || 24000);
        pcmPlayer?.enqueue(event.audio_base64);
      } else {
        completePlayback.push(playBase64Audio(event.audio_base64, event.audio_mime || "audio/mpeg"));
      }
    };

    while (true) {
      const { value, done } = await reader.read();
      buffer += decoder.decode(value || new Uint8Array(), { stream: !done });
      const lines = buffer.split("\n");
      buffer = lines.pop() || "";
      for (const line of lines) processLine(line);
      if (done) break;
    }
    if (buffer.trim()) processLine(buffer);
    if (pcmPlayer) await pcmPlayer.finish();
    if (completePlayback.length) await Promise.all(completePlayback);
    return started;
  } catch (error) {
    return false;
  }
}

function createPCMPlayer(sampleRate) {
  const context = unlockFeedbackAudio();
  if (!context) return null;
  let nextStart = context.currentTime + 0.18;
  let pending = 0;
  let finished = false;
  let leftover = null;
  let resolveFinished;
  const finishedPromise = new Promise((resolve) => {
    resolveFinished = resolve;
  });

  const finishIfReady = () => {
    if (finished && pending === 0) resolveFinished();
  };

  return {
    enqueue(base64) {
      let bytes = decodeBase64Bytes(base64);
      if (leftover !== null) {
        const joined = new Uint8Array(bytes.length + 1);
        joined[0] = leftover;
        joined.set(bytes, 1);
        bytes = joined;
        leftover = null;
      }
      if (bytes.length % 2 === 1) {
        leftover = bytes[bytes.length - 1];
        bytes = bytes.subarray(0, bytes.length - 1);
      }
      if (!bytes.length) return;

      const samples = new Float32Array(bytes.length / 2);
      const view = new DataView(bytes.buffer, bytes.byteOffset, bytes.byteLength);
      for (let i = 0; i < samples.length; i += 1) {
        samples[i] = view.getInt16(i * 2, true) / 32768;
      }
      const audioBuffer = context.createBuffer(1, samples.length, sampleRate);
      audioBuffer.copyToChannel(samples, 0);
      const source = context.createBufferSource();
      source.buffer = audioBuffer;
      source.connect(context.destination);
      const startAt = Math.max(nextStart, context.currentTime + 0.03);
      source.start(startAt);
      nextStart = startAt + audioBuffer.duration;
      pending += 1;
      source.addEventListener("ended", () => {
        pending -= 1;
        finishIfReady();
      }, { once: true });
    },
    finish() {
      finished = true;
      finishIfReady();
      return finishedPromise;
    },
  };
}

async function speakStatus(text, stateText) {
  setPhase("speaking", stateText, "等一下");
  await speak(text);
  setPhase("idle", "按住小爪子说话", "按住说话");
}

function actionLabel(payload) {
  switch (payload.activity?.action) {
    case "tail_wag":
      return "摇摇尾巴";
    case "ear_wiggle":
      return "动动耳朵";
    case "glow_red":
      return "找红色";
    case "paw_tap":
      return "拍拍爪子";
    case "slow_breathe":
      return "慢慢呼吸";
    default:
      return "豆豆说话";
  }
}

function setPhase(phase, stateText, label) {
  document.body.classList.remove("idle", "listening", "thinking", "speaking");
  document.body.classList.add(phase);
  state.busy = phase === "thinking" || phase === "speaking";
  els.recordButton.setAttribute("aria-disabled", String(state.busy));
  els.toyState.textContent = stateText;
  els.recordLabel.textContent = label;
  if (phase === "thinking") {
    startThinkingSound();
  } else {
    stopThinkingSound();
  }
}

function shouldUseBrowserSpeech() {
  return !hasServerVoice() && Boolean(browserSpeechRecognition());
}

function browserSpeechRecognition() {
  return window.SpeechRecognition || window.webkitSpeechRecognition || null;
}

async function createWavRecorder(stream) {
  const AudioContext = window.AudioContext || window.webkitAudioContext;
  if (!AudioContext) throw new Error("AudioContext is not supported");

  const context = new AudioContext();
  await context.resume();
  const source = context.createMediaStreamSource(stream);
  const processor = context.createScriptProcessor(4096, 1, 1);
  const gain = context.createGain();
  const chunks = [];
  let latestRMS = 0;
  let peakLevel = 0;

  gain.gain.value = 0;
  processor.onaudioprocess = (event) => {
    const input = event.inputBuffer.getChannelData(0);
    chunks.push(new Float32Array(input));
    let sumSquares = 0;
    let peak = 0;
    for (let i = 0; i < input.length; i += 1) {
      const abs = Math.abs(input[i]);
      if (abs > peak) peak = abs;
      sumSquares += input[i] * input[i];
    }
    latestRMS = Math.sqrt(sumSquares / input.length);
    if (peak > peakLevel) peakLevel = peak;
  };
  source.connect(processor);
  processor.connect(gain);
  gain.connect(context.destination);

  return {
    async stop() {
      processor.disconnect();
      source.disconnect();
      gain.disconnect();
      stream.getTracks().forEach((track) => track.stop());
      const sampleRate = context.sampleRate;
      await context.close();
      const durationMs = Math.round((chunks.reduce((sum, chunk) => sum + chunk.length, 0) / sampleRate) * 1000);
      return {
        blob: encodeWav(chunks, sampleRate),
        mimeType: "audio/wav",
        filename: "recording.wav",
        durationMs,
        peakLevel,
      };
    },
    level() {
      return latestRMS;
    },
  };
}

function encodeWav(chunks, sampleRate) {
  const targetSampleRate = 16000;
  const samples = resampleFloat32(mergeFloat32(chunks), sampleRate, targetSampleRate);
  const buffer = new ArrayBuffer(44 + samples.length * 2);
  const view = new DataView(buffer);

  writeString(view, 0, "RIFF");
  view.setUint32(4, 36 + samples.length * 2, true);
  writeString(view, 8, "WAVE");
  writeString(view, 12, "fmt ");
  view.setUint32(16, 16, true);
  view.setUint16(20, 1, true);
  view.setUint16(22, 1, true);
  view.setUint32(24, targetSampleRate, true);
  view.setUint32(28, targetSampleRate * 2, true);
  view.setUint16(32, 2, true);
  view.setUint16(34, 16, true);
  writeString(view, 36, "data");
  view.setUint32(40, samples.length * 2, true);
  floatTo16BitPCM(view, 44, samples);

  return new Blob([buffer], { type: "audio/wav" });
}

function resampleFloat32(input, fromRate, toRate) {
  if (!input.length || fromRate === toRate) return input;
  const ratio = fromRate / toRate;
  const length = Math.max(1, Math.floor(input.length / ratio));
  const output = new Float32Array(length);
  for (let i = 0; i < length; i += 1) {
    const position = i * ratio;
    const left = Math.floor(position);
    const right = Math.min(left + 1, input.length - 1);
    const weight = position - left;
    output[i] = input[left] * (1 - weight) + input[right] * weight;
  }
  return output;
}

function mergeFloat32(chunks) {
  const length = chunks.reduce((sum, chunk) => sum + chunk.length, 0);
  const result = new Float32Array(length);
  let offset = 0;
  for (const chunk of chunks) {
    result.set(chunk, offset);
    offset += chunk.length;
  }
  return result;
}

function floatTo16BitPCM(view, offset, input) {
  for (let i = 0; i < input.length; i += 1, offset += 2) {
    const sample = Math.max(-1, Math.min(1, input[i]));
    view.setInt16(offset, sample < 0 ? sample * 0x8000 : sample * 0x7fff, true);
  }
}

function writeString(view, offset, value) {
  for (let i = 0; i < value.length; i += 1) {
    view.setUint8(offset + i, value.charCodeAt(i));
  }
}

async function fetchJSON(url) {
  const response = await fetch(url, { headers: authHeaders() });
  if (!response.ok) throw await responseError(response);
  return response.json();
}

async function postJSON(url, payload) {
  const response = await fetch(url, {
    method: "POST",
    headers: authHeaders({ "Content-Type": "application/json" }),
    body: JSON.stringify(payload),
  });
  if (!response.ok) throw await responseError(response);
  return response.json();
}

async function responseError(response) {
  const body = await response.text();
  let message = body || `HTTP ${response.status}`;
  try {
    message = JSON.parse(body).error || message;
  } catch (error) {
    // Keep the raw response body.
  }
  const error = new Error(message);
  error.status = response.status;
  return error;
}

function loadAccessToken() {
  const url = new URL(window.location.href);
  if (url.searchParams.get("clearToken") === "1") {
    removeStoredAccessToken();
    url.searchParams.delete("clearToken");
    window.history.replaceState(null, "", url.pathname + url.search + url.hash);
    return "";
  }

  const token = (url.searchParams.get("token") || "").trim();
  if (token) {
    storeAccessToken(token);
    url.searchParams.delete("token");
    window.history.replaceState(null, "", url.pathname + url.search + url.hash);
    return token;
  }
  return storedAccessToken();
}

function authHeaders(headers = {}) {
  const result = { ...headers };
  if (state.accessToken) result.Authorization = `Bearer ${state.accessToken}`;
  if (state.sessionID) result["X-Pupbox-Session-ID"] = state.sessionID;
  return result;
}

function loadSessionID(storageKey) {
  try {
    const stored = window.localStorage.getItem(storageKey);
    if (stored) return stored;
    const id = `toy-${randomSessionID()}`;
    window.localStorage.setItem(storageKey, id);
    return id;
  } catch (error) {
    return `toy-${randomSessionID()}`;
  }
}

function randomSessionID() {
  if (window.crypto?.randomUUID) return window.crypto.randomUUID();
  const values = new Uint32Array(4);
  window.crypto?.getRandomValues?.(values);
  return Array.from(values, (value) => value.toString(16).padStart(8, "0")).join("");
}

function storedAccessToken() {
  try {
    return window.localStorage.getItem("pupbox.accessToken") || "";
  } catch (error) {
    return "";
  }
}

function storeAccessToken(token) {
  try {
    window.localStorage.setItem("pupbox.accessToken", token);
  } catch (error) {
    // Query-token fallback still works for this page load.
  }
}

function removeStoredAccessToken() {
  try {
    window.localStorage.removeItem("pupbox.accessToken");
  } catch (error) {
    // Ignore storage failures.
  }
}

function speak(text) {
  if (hasServerVoice()) {
    return speakWithServerVoice(text);
  }
  return speakInBrowser(text);
}

async function speakWithServerVoice(text) {
  try {
    const payload = await postJSON("/api/speech", { text });
    if (payload.audio_base64 && payload.audio_mime) {
      const played = await playBase64Audio(payload.audio_base64, payload.audio_mime);
      if (played) return;
    }
  } catch (error) {
    // Fall back to browser speech below.
  }
  await speakInBrowser(text);
}

function startThinkingSound() {
  if (state.thinkingTimer) return;
  const context = unlockFeedbackAudio();
  if (!context) return;
  playThinkingMotif(context);
  state.thinkingTimer = window.setInterval(() => playThinkingMotif(context), 1800);
}

function stopThinkingSound() {
  if (state.thinkingTimer) {
    window.clearInterval(state.thinkingTimer);
    state.thinkingTimer = null;
  }
  for (const oscillator of state.thinkingNodes) {
    try {
      oscillator.stop();
    } catch (error) {
      // The oscillator may already have ended.
    }
  }
  state.thinkingNodes.clear();
}

function unlockFeedbackAudio() {
  const AudioContext = window.AudioContext || window.webkitAudioContext;
  if (!AudioContext) return null;
  if (!state.feedbackContext) state.feedbackContext = new AudioContext();
  if (state.feedbackContext.state === "suspended") {
    state.feedbackContext.resume().catch(() => {});
  }
  return state.feedbackContext;
}

function playCue(kind) {
  const context = unlockFeedbackAudio();
  if (!context) return;
  const patterns = {
    listening: [[440, 0, 0.08, 0.035], [659.25, 0.09, 0.12, 0.04]],
  };
  for (const tone of patterns[kind] || []) playTone(context, ...tone, false);
}

function playThinkingMotif(context) {
  playTone(context, 523.25, 0, 0.14, 0.025, true);
  playTone(context, 659.25, 0.14, 0.14, 0.023, true);
  playTone(context, 783.99, 0.28, 0.2, 0.021, true);
}

function playTone(context, frequency, delay, duration, gainValue, thinking) {
  const oscillator = context.createOscillator();
  const gain = context.createGain();
  const start = context.currentTime + delay;
  oscillator.type = "triangle";
  oscillator.frequency.value = frequency;
  gain.gain.setValueAtTime(0.0001, start);
  gain.gain.exponentialRampToValueAtTime(gainValue, start + 0.02);
  gain.gain.exponentialRampToValueAtTime(0.0001, start + duration);
  oscillator.connect(gain);
  gain.connect(context.destination);
  oscillator.start(start);
  oscillator.stop(start + duration + 0.02);
  if (thinking) {
    state.thinkingNodes.add(oscillator);
    oscillator.addEventListener("ended", () => state.thinkingNodes.delete(oscillator), { once: true });
  }
}

function hasServerVoice() {
  return remoteVoiceProvider() !== "mock";
}

function remoteVoiceProvider() {
  return state.health?.voice_provider || (state.health?.mode === "openai" ? "openai" : "mock");
}

function speakInBrowser(text) {
  if (!("speechSynthesis" in window)) return Promise.resolve();
  window.speechSynthesis.cancel();
  return new Promise((resolve) => {
    const utterance = new SpeechSynthesisUtterance(text);
    utterance.lang = "zh-CN";
    utterance.rate = 0.88;
    utterance.pitch = 1.25;
    utterance.onend = resolve;
    utterance.onerror = resolve;
    window.speechSynthesis.speak(utterance);
  });
}

function playBase64Audio(base64, mimeType) {
  const bytes = decodeBase64Bytes(base64);
  const url = URL.createObjectURL(new Blob([bytes], { type: mimeType }));
  const audio = audioPlayer();
  if (state.audioObjectURL) URL.revokeObjectURL(state.audioObjectURL);
  state.audioObjectURL = url;
  audio.pause();
  audio.muted = false;
  audio.src = url;
  return new Promise((resolve) => {
    let settled = false;
    const finish = (played) => {
      if (settled) return;
      settled = true;
      URL.revokeObjectURL(url);
      if (state.audioObjectURL === url) state.audioObjectURL = "";
      resolve(played);
    };
    audio.addEventListener("ended", () => {
      finish(true);
    }, { once: true });
    audio.addEventListener("error", () => {
      finish(false);
    }, { once: true });
    audio.play().catch(() => finish(false));
  });
}

function decodeBase64Bytes(base64) {
  const binary = atob(base64);
  const bytes = new Uint8Array(binary.length);
  for (let i = 0; i < binary.length; i += 1) {
    bytes[i] = binary.charCodeAt(i);
  }
  return bytes;
}

function unlockAudioPlayback() {
  if (state.audioUnlocked) return;
  const audio = audioPlayer();
  audio.muted = true;
  audio.src = SILENT_WAV_DATA_URI;
  audio.play().then(() => {
    audio.pause();
    audio.currentTime = 0;
    audio.muted = false;
    state.audioUnlocked = true;
  }).catch(() => {
    audio.muted = false;
    state.audioUnlocked = false;
  });
}

function audioPlayer() {
  if (!state.audioPlayer) {
    state.audioPlayer = new Audio();
    state.audioPlayer.preload = "auto";
    state.audioPlayer.playsInline = true;
  }
  return state.audioPlayer;
}
