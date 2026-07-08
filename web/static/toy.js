const state = {
  recorder: null,
  recognition: null,
  chunks: [],
  recording: false,
  speechText: "",
  speechSent: false,
  health: null,
  awake: false,
  spaceDown: false,
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
    els.modePill.textContent = health.mode === "openai" ? "OpenAI" : "Mock";
    if (health.mode === "openai") {
      const speed = Number(health.tts_speed || 1).toFixed(2);
      els.voiceNote.textContent = `OpenAI 语音：${health.tts_voice || "voice"} / ${speed}x`;
    } else if (browserSpeechRecognition()) {
      els.voiceNote.textContent = "按住说话，松开发送";
    } else {
      els.voiceNote.textContent = "当前浏览器不支持听写";
    }
  } catch (error) {
    els.modePill.textContent = "离线";
    els.voiceNote.textContent = "服务未连接";
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

  if (state.health?.mode !== "openai") {
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
    const mimeType = pickAudioMimeType();
    const recorder = new MediaRecorder(stream, mimeType ? { mimeType } : undefined);
    state.chunks = [];
    state.recorder = recorder;
    state.recording = true;
    setPhase("listening", "豆豆听着呢", "松开");

    recorder.addEventListener("dataavailable", (recordEvent) => {
      if (recordEvent.data.size > 0) state.chunks.push(recordEvent.data);
    });
    recorder.addEventListener("stop", () => {
      stream.getTracks().forEach((track) => track.stop());
      sendRecording(mimeType || recorder.mimeType || "audio/webm");
    });
    recorder.start();
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
  state.recording = false;
  if (state.recorder.state !== "inactive") state.recorder.stop();
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
    await speak("豆豆这里出了一点小问题。");
  }
}

async function sendRecording(mimeType) {
  if (!state.chunks.length) return;
  const blob = new Blob(state.chunks, { type: mimeType });
  const form = new FormData();
  const ext = mimeType.includes("mp4") ? "mp4" : "webm";
  form.append("audio", blob, `recording.${ext}`);

  setPhase("thinking", "豆豆想一想", "等一下");
  try {
    const response = await fetch("/api/voice", { method: "POST", body: form });
    if (!response.ok) throw new Error(await response.text());
    const payload = await response.json();
    await handleDogResponse(payload);
  } catch (error) {
    setPhase("idle", "出错了", "按住");
    await speak("豆豆这里出了一点小问题。");
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
}

function shouldUseBrowserSpeech() {
  return state.health?.mode !== "openai" && Boolean(browserSpeechRecognition());
}

function browserSpeechRecognition() {
  return window.SpeechRecognition || window.webkitSpeechRecognition || null;
}

function pickAudioMimeType() {
  const candidates = ["audio/webm;codecs=opus", "audio/webm", "audio/mp4"];
  return candidates.find((candidate) => MediaRecorder.isTypeSupported(candidate)) || "";
}

async function fetchJSON(url) {
  const response = await fetch(url);
  if (!response.ok) throw new Error(await response.text());
  return response.json();
}

async function postJSON(url, payload) {
  const response = await fetch(url, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(payload),
  });
  if (!response.ok) throw new Error(await response.text());
  return response.json();
}

function speak(text) {
  if (state.health?.mode === "openai") {
    return speakWithOpenAI(text);
  }
  return speakInBrowser(text);
}

async function speakWithOpenAI(text) {
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
