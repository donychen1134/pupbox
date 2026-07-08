const state = {
  recorder: null,
  recognition: null,
  chunks: [],
  recording: false,
  speechText: "",
  speechSent: false,
  health: null,
  activities: [],
};

const els = {
  modePill: document.querySelector("#modePill"),
  modeText: document.querySelector("#modeText"),
  sourceText: document.querySelector("#sourceText"),
  replyText: document.querySelector("#replyText"),
  recordButton: document.querySelector("#recordButton"),
  recordLabel: document.querySelector("#recordLabel"),
  voiceNote: document.querySelector("#voiceNote"),
  activityTray: document.querySelector("#activityTray"),
  textForm: document.querySelector("#textForm"),
  textInput: document.querySelector("#textInput"),
  clearButton: document.querySelector("#clearButton"),
  logList: document.querySelector("#logList"),
};

init();

async function init() {
  bindEvents();
  await loadHealth();
  await loadActivities();
  appendLog("pup", "豆豆", "汪，豆豆在这里。");
}

function bindEvents() {
  els.textForm.addEventListener("submit", async (event) => {
    event.preventDefault();
    const text = els.textInput.value.trim();
    if (!text) return;
    els.textInput.value = "";
    appendLog("child", "小朋友", text);
    setBusy("豆豆想一想");
    try {
      const response = await postJSON("/api/chat", { text });
      handleDogResponse(response);
    } catch (error) {
      showError(error);
    }
  });

  els.recordButton.addEventListener("pointerdown", startRecording);
  els.recordButton.addEventListener("pointerup", stopRecording);
  els.recordButton.addEventListener("pointercancel", stopRecording);
  els.recordButton.addEventListener("pointerleave", () => {
    if (state.recording) stopRecording();
  });

  els.clearButton.addEventListener("click", () => {
    els.logList.replaceChildren();
  });
}

async function loadActivities() {
  try {
    const payload = await fetchJSON("/api/activities");
    state.activities = payload.activities || [];
    renderActivities();
  } catch (error) {
    appendLog("warn", "活动", "活动列表加载失败");
  }
}

function renderActivities() {
  els.activityTray.replaceChildren();
  for (const activity of state.activities) {
    const button = document.createElement("button");
    button.type = "button";
    button.className = `activity-button activity-${activity.category}`;
    button.textContent = activity.label;
    button.addEventListener("click", () => sendActivity(activity));
    els.activityTray.append(button);
  }
}

async function sendActivity(activity) {
  appendLog("child", "活动", activity.label);
  setBusy("豆豆想一想");
  try {
    const response = await postJSON("/api/chat", { text: activity.prompt });
    handleDogResponse(response);
  } catch (error) {
    showError(error);
  }
}

async function loadHealth() {
  try {
    const health = await fetchJSON("/api/health");
    state.health = health;
    const browserSTT = browserSpeechRecognition();
    els.modePill.textContent = health.mode === "openai" ? "OpenAI" : "Mock";
    els.modeText.textContent = health.mode;
    els.sourceText.textContent = "-";
    if (health.mode === "openai") {
      els.voiceNote.textContent = "语音：OpenAI STT + TTS";
    } else if (browserSTT) {
      els.voiceNote.textContent = "语音：浏览器听写 + 本地 mock 回复";
    } else {
      els.voiceNote.textContent = "语音：当前浏览器不支持听写；可用文字或配置 OPENAI_API_KEY";
    }
  } catch (error) {
    els.modePill.textContent = "离线";
    els.modeText.textContent = "error";
    els.voiceNote.textContent = "语音：服务未连接";
  }
}

async function startRecording(event) {
  event.preventDefault();
  if (state.recording) return;

  if (shouldUseBrowserSpeech()) {
    startBrowserSpeech(event);
    return;
  }

  if (state.health?.mode !== "openai") {
    showError(new Error("当前 mock 模式没有服务端语音识别。请用 Chrome 浏览器听写，或设置 OPENAI_API_KEY。"));
    return;
  }

  if (!navigator.mediaDevices?.getUserMedia) {
    showError(new Error("这个浏览器不能录音"));
    return;
  }

  try {
    const stream = await navigator.mediaDevices.getUserMedia({ audio: true });
    const mimeType = pickAudioMimeType();
    const recorder = new MediaRecorder(stream, mimeType ? { mimeType } : undefined);
    state.chunks = [];
    state.recorder = recorder;
    state.recording = true;
    document.body.classList.add("recording");
    els.recordLabel.textContent = "松开发送";
    els.recordButton.setPointerCapture?.(event.pointerId);

    recorder.addEventListener("dataavailable", (recordEvent) => {
      if (recordEvent.data.size > 0) state.chunks.push(recordEvent.data);
    });
    recorder.addEventListener("stop", () => {
      stream.getTracks().forEach((track) => track.stop());
      sendRecording(mimeType || recorder.mimeType || "audio/webm");
    });
    recorder.start();
  } catch (error) {
    state.recording = false;
    document.body.classList.remove("recording");
    els.recordLabel.textContent = "按住说话";
    showError(error);
  }
}

function stopRecording() {
  if (state.recognition) {
    state.recording = false;
    document.body.classList.remove("recording");
    els.recordLabel.textContent = "按住说话";
    state.recognition.stop();
    return;
  }

  if (!state.recording || !state.recorder) return;
  state.recording = false;
  document.body.classList.remove("recording");
  els.recordLabel.textContent = "按住说话";
  if (state.recorder.state !== "inactive") state.recorder.stop();
}

function startBrowserSpeech(event) {
  const SpeechRecognition = browserSpeechRecognition();
  if (!SpeechRecognition) {
    showError(new Error("这个浏览器不支持中文听写。建议用 Chrome，或配置 OPENAI_API_KEY。"));
    return;
  }

  const recognition = new SpeechRecognition();
  recognition.lang = "zh-CN";
  recognition.interimResults = true;
  recognition.continuous = true;
  recognition.maxAlternatives = 1;

  state.recognition = recognition;
  state.speechText = "";
  state.speechSent = false;
  state.recording = true;
  document.body.classList.add("recording");
  els.recordLabel.textContent = "松开发送";
  els.recordButton.setPointerCapture?.(event.pointerId);
  setBusy("豆豆听一听");

  recognition.addEventListener("result", (resultEvent) => {
    let interim = "";
    for (let i = resultEvent.resultIndex; i < resultEvent.results.length; i += 1) {
      const transcript = resultEvent.results[i][0]?.transcript?.trim() || "";
      if (resultEvent.results[i].isFinal) {
        state.speechText = `${state.speechText} ${transcript}`.trim();
      } else {
        interim = transcript;
      }
    }
    if (interim) els.replyText.textContent = `豆豆听到：${interim}`;
  });

  recognition.addEventListener("error", (errorEvent) => {
    state.speechSent = true;
    cleanupBrowserSpeech();
    showError(new Error(`浏览器听写失败：${errorEvent.error}`));
  });

  recognition.addEventListener("end", () => {
    const text = state.speechText.trim();
    cleanupBrowserSpeech();
    if (!state.speechSent) {
      state.speechSent = true;
      sendRecognizedText(text);
    }
  });

  try {
    recognition.start();
  } catch (error) {
    cleanupBrowserSpeech();
    showError(error);
  }
}

function cleanupBrowserSpeech() {
  state.recording = false;
  state.recognition = null;
  document.body.classList.remove("recording");
  els.recordLabel.textContent = "按住说话";
}

async function sendRecognizedText(text) {
  try {
    if (!text) {
      const fallback = "嗯嗯";
      appendLog("child", "小朋友", fallback);
      setBusy("豆豆想一想");
      const response = await postJSON("/api/chat", { text: fallback });
      handleDogResponse({ ...response, source: "browser_stt_empty" });
      return;
    }

    appendLog("child", "小朋友", text);
    setBusy("豆豆想一想");
    const response = await postJSON("/api/chat", { text });
    handleDogResponse({ ...response, source: `browser_stt/${response.source || "chat"}` });
  } catch (error) {
    showError(error);
  }
}

async function sendRecording(mimeType) {
  if (!state.chunks.length) return;
  const blob = new Blob(state.chunks, { type: mimeType });
  const form = new FormData();
  const ext = mimeType.includes("mp4") ? "mp4" : "webm";
  form.append("audio", blob, `recording.${ext}`);

  setBusy("豆豆听一听");
  try {
    const response = await fetch("/api/voice", { method: "POST", body: form });
    if (!response.ok) throw new Error(await response.text());
    const payload = await response.json();
    if (payload.transcript) appendLog("child", "小朋友", payload.transcript);
    handleDogResponse(payload);
  } catch (error) {
    showError(error);
  }
}

function handleDogResponse(payload) {
  const reply = payload.reply || "豆豆没有想好。";
  const className = payload.safety?.triggered ? "warn" : "pup";
  els.replyText.textContent = reply;
  els.modeText.textContent = payload.mode || "-";
  els.sourceText.textContent = payload.source || "-";
  appendLog(className, payload.safety?.triggered ? "安全提醒" : "豆豆", reply);

  if (payload.ai_error) appendLog("warn", "API", payload.ai_error);
  if (payload.tts_error) appendLog("warn", "TTS", payload.tts_error);

  if (payload.audio_base64 && payload.audio_mime) {
    playBase64Audio(payload.audio_base64, payload.audio_mime);
  } else {
    speakInBrowser(reply);
  }
}

function appendLog(kind, speaker, content) {
  const item = document.createElement("article");
  item.className = `log-item ${kind}`;
  const speakerEl = document.createElement("div");
  speakerEl.className = "speaker";
  speakerEl.textContent = speaker;
  const contentEl = document.createElement("div");
  contentEl.className = "content";
  contentEl.textContent = content;
  item.append(speakerEl, contentEl);
  els.logList.append(item);
  els.logList.scrollTop = els.logList.scrollHeight;
}

function setBusy(text) {
  els.replyText.textContent = text;
  els.sourceText.textContent = "pending";
}

function showError(error) {
  const message = error?.message || String(error);
  els.replyText.textContent = "豆豆这里出了一点小问题。";
  appendLog("warn", "错误", message);
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

function pickAudioMimeType() {
  const candidates = [
    "audio/webm;codecs=opus",
    "audio/webm",
    "audio/mp4",
  ];
  return candidates.find((candidate) => MediaRecorder.isTypeSupported(candidate)) || "";
}

function shouldUseBrowserSpeech() {
  return state.health?.mode !== "openai" && Boolean(browserSpeechRecognition());
}

function browserSpeechRecognition() {
  return window.SpeechRecognition || window.webkitSpeechRecognition || null;
}

function playBase64Audio(base64, mimeType) {
  const binary = atob(base64);
  const bytes = new Uint8Array(binary.length);
  for (let i = 0; i < binary.length; i += 1) {
    bytes[i] = binary.charCodeAt(i);
  }
  const url = URL.createObjectURL(new Blob([bytes], { type: mimeType }));
  const audio = new Audio(url);
  audio.addEventListener("ended", () => URL.revokeObjectURL(url), { once: true });
  audio.play().catch(() => speakInBrowser(els.replyText.textContent));
}

function speakInBrowser(text) {
  if (!("speechSynthesis" in window)) return;
  window.speechSynthesis.cancel();
  const utterance = new SpeechSynthesisUtterance(text);
  utterance.lang = "zh-CN";
  utterance.rate = 0.88;
  utterance.pitch = 1.25;
  window.speechSynthesis.speak(utterance);
}
