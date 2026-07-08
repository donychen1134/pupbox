const state = {
  recorder: null,
  recognition: null,
  recording: false,
  speechText: "",
  speechSent: false,
  health: null,
  awake: false,
  spaceDown: false,
  thinkingSound: null,
  accessToken: "",
};

const els = {
  modePill: document.querySelector("#modePill"),
  recordButton: document.querySelector("#recordButton"),
  recordLabel: document.querySelector("#recordLabel"),
  toyState: document.querySelector("#toyState"),
  voiceNote: document.querySelector("#voiceNote"),
};

init();

async function init() {
  state.accessToken = loadAccessToken();
  bindEvents();
  await loadHealth();
}

function bindEvents() {
  els.recordButton.addEventListener("pointerdown", startPress);
  els.recordButton.addEventListener("pointerup", stopPress);
  els.recordButton.addEventListener("pointercancel", stopPress);
  els.recordButton.addEventListener("pointerleave", () => {
    if (state.recording) stopPress();
  });

  window.addEventListener("keydown", (event) => {
    if (event.code !== "Space" || state.spaceDown) return;
    event.preventDefault();
    state.spaceDown = true;
    startPress(event);
  });
  window.addEventListener("keyup", (event) => {
    if (event.code !== "Space") return;
    event.preventDefault();
    state.spaceDown = false;
    stopPress();
  });
}

async function loadHealth() {
  try {
    const health = await fetchJSON("/api/health");
    state.health = health;
    els.modePill.textContent = providerLabel(health.mode);
    if (hasServerVoice()) {
      const speed = Number(health.tts_speed || 1).toFixed(2);
      els.voiceNote.textContent = `${providerLabel(remoteVoiceProvider())} 语音：${health.tts_voice || "voice"} / ${speed}x`;
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

async function startPress(event) {
  event?.preventDefault?.();
  if (state.recording) return;
  if (!state.awake) {
    state.awake = true;
    setPhase("speaking", "豆豆醒啦", "按住");
    await speak("汪，豆豆醒啦。按住小爪子，跟豆豆说话。");
    setPhase("idle", "豆豆在听", "按住");
    return;
  }

  if (shouldUseBrowserSpeech()) {
    startBrowserSpeech(event);
    return;
  }

  if (!hasServerVoice()) {
    setPhase("idle", "还没连上语音", "按住");
    await speak("豆豆还没连上语音。请让爸爸妈妈看一下设置。");
    return;
  }

  if (!navigator.mediaDevices?.getUserMedia) {
    setPhase("idle", "不能录音", "按住");
    await speak("这个浏览器不能录音。");
    return;
  }

  try {
    const stream = await navigator.mediaDevices.getUserMedia({ audio: true });
    const recorder = await createWavRecorder(stream);
    state.recorder = recorder;
    state.recording = true;
    setPhase("listening", "豆豆听着呢", "松开");
  } catch (error) {
    setPhase("idle", "没有听清", "按住");
    await speak("豆豆没有听清。再试一次吧。");
  }
}

function stopPress() {
  if (state.recognition) {
    state.recording = false;
    state.recognition.stop();
    return;
  }

  if (!state.recording || !state.recorder) return;
  const recorder = state.recorder;
  state.recorder = null;
  state.recording = false;
  recorder.stop().then((recording) => {
    sendRecording(recording.blob, recording.mimeType, recording.filename, recording.durationMs);
  }).catch(async () => {
    setPhase("idle", "没有听清", "按住");
    await speak("豆豆没有听清。再试一次吧。");
  });
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
  setPhase("listening", "豆豆听着呢", "松开");
  els.recordButton.setPointerCapture?.(event?.pointerId);

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
    setPhase("idle", "没有听清", "按住");
    await speak("豆豆没有听清。再说一次吧。");
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
}

async function sendRecognizedText(text) {
  setPhase("thinking", "豆豆想一想", "等一下");
  try {
    const response = await postJSON("/api/chat", { text });
    await handleDogResponse(response);
  } catch (error) {
    setPhase("idle", "出错了", "按住");
    await speak(error.status === 401 ? "请爸爸妈妈帮豆豆设置一下。" : "豆豆这里出了一点小问题。");
  }
}

async function sendRecording(blob, mimeType, filename, durationMs) {
  if (!blob || blob.size === 0) return;
  if (durationMs && durationMs < 260) {
    setPhase("idle", "没有听清", "按住");
    await speak("豆豆没有听清。再说长一点点。");
    return;
  }
  const form = new FormData();
  form.append("audio", blob, filename || "recording.wav");

  setPhase("thinking", "豆豆想一想", "等一下");
  try {
    const response = await fetch("/api/voice", { method: "POST", headers: authHeaders(), body: form });
    if (!response.ok) throw await responseError(response);
    const payload = await response.json();
    await handleDogResponse(payload);
  } catch (error) {
    setPhase("idle", "出错了", "按住");
    await speak(error.status === 401 ? "请爸爸妈妈帮豆豆设置一下。" : "豆豆这里出了一点小问题。");
  }
}

async function handleDogResponse(payload) {
  const reply = payload.reply || "豆豆没有想好。";
  setPhase("speaking", payload.safety?.triggered ? "找爸爸妈妈" : actionLabel(payload), "按住");
  if (payload.audio_base64 && payload.audio_mime) {
    await playBase64Audio(payload.audio_base64, payload.audio_mime);
  } else {
    await speak(reply);
  }
  setPhase("idle", "豆豆在这里", "按住");
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

  gain.gain.value = 0;
  processor.onaudioprocess = (event) => {
    chunks.push(new Float32Array(event.inputBuffer.getChannelData(0)));
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
      };
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
  return result;
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
      await playBase64Audio(payload.audio_base64, payload.audio_mime);
      return;
    }
  } catch (error) {
    // Fall back to browser speech below.
  }
  await speakInBrowser(text);
}

function startThinkingSound() {
  if (state.thinkingSound) return;
  const AudioContext = window.AudioContext || window.webkitAudioContext;
  if (!AudioContext) return;
  const context = new AudioContext();
  const play = () => {
    if (state.thinkingSound?.context === context) playTonePair(context);
  };
  state.thinkingSound = {
    context,
    timer: window.setInterval(play, 1400),
  };
  context.resume().then(play).catch(() => {});
}

function stopThinkingSound() {
  if (!state.thinkingSound) return;
  window.clearInterval(state.thinkingSound.timer);
  state.thinkingSound.context.close().catch(() => {});
  state.thinkingSound = null;
}

function playTonePair(context) {
  playTone(context, 523.25, 0, 0.08, 0.035);
  playTone(context, 659.25, 0.1, 0.09, 0.03);
}

function playTone(context, frequency, delay, duration, gainValue) {
  const oscillator = context.createOscillator();
  const gain = context.createGain();
  const start = context.currentTime + delay;
  oscillator.type = "sine";
  oscillator.frequency.value = frequency;
  gain.gain.setValueAtTime(0.0001, start);
  gain.gain.exponentialRampToValueAtTime(gainValue, start + 0.02);
  gain.gain.exponentialRampToValueAtTime(0.0001, start + duration);
  oscillator.connect(gain);
  gain.connect(context.destination);
  oscillator.start(start);
  oscillator.stop(start + duration + 0.02);
}

function hasServerVoice() {
  return remoteVoiceProvider() !== "mock";
}

function remoteVoiceProvider() {
  return state.health?.voice_provider || (state.health?.mode === "openai" ? "openai" : "mock");
}

function providerLabel(provider) {
  switch (provider) {
    case "dashscope":
      return "阿里云";
    case "openai":
      return "OpenAI";
    case "openai-chat":
      return "OpenAI Chat";
    default:
      return "Mock";
  }
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
  const binary = atob(base64);
  const bytes = new Uint8Array(binary.length);
  for (let i = 0; i < binary.length; i += 1) {
    bytes[i] = binary.charCodeAt(i);
  }
  const url = URL.createObjectURL(new Blob([bytes], { type: mimeType }));
  const audio = new Audio(url);
  return new Promise((resolve) => {
    audio.addEventListener("ended", () => {
      URL.revokeObjectURL(url);
      resolve();
    }, { once: true });
    audio.addEventListener("error", () => {
      URL.revokeObjectURL(url);
      resolve();
    }, { once: true });
    audio.play().catch(resolve);
  });
}
