const SILENT_WAV_DATA_URI = "data:audio/wav;base64,UklGRiYAAABXQVZFZm10IBAAAAABAAEAgD4AAAB9AAACABAAZGF0YQIAAAAAAA==";

const state = {
  recorder: null,
  recognition: null,
  recording: false,
  speechText: "",
  speechSent: false,
  health: null,
  activities: [],
  events: [],
  accessToken: "",
  recordingStartedAt: 0,
  recordingTimer: null,
  audioPlayer: null,
  audioObjectURL: "",
  audioUnlocked: false,
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
  refreshEventsButton: document.querySelector("#refreshEventsButton"),
  eventList: document.querySelector("#eventList"),
  recordingMeter: document.querySelector("#recordingMeter"),
  recordingLevel: document.querySelector("#recordingLevel"),
  recordingTime: document.querySelector("#recordingTime"),
};

init();

async function init() {
  state.accessToken = loadAccessToken();
  bindEvents();
  await loadHealth();
  await loadActivities();
  await loadEvents();
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

  els.refreshEventsButton.addEventListener("click", () => {
    loadEvents().catch(showError);
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

async function loadEvents() {
  try {
    const payload = await fetchJSON("/api/events?limit=50");
    state.events = payload.events || [];
    renderEvents();
    renderConversationHistory();
  } catch (error) {
    if (error.status === 401) {
      state.events = [];
      renderEvents("需要访问 token 才能读取诊断记录");
      renderConversationHistory();
      return;
    }
    state.events = [];
    renderEvents("诊断记录加载失败");
    renderConversationHistory();
  }
}

function renderConversationHistory() {
  els.logList.replaceChildren();
  if (!state.events.length) {
    appendLog("pup", "豆豆", "汪，豆豆在这里。");
    return;
  }
  const events = [...state.events].reverse();
  for (const event of events) {
    if (event.transcript) appendLog("child", "小朋友", event.transcript);
    if (event.reply) {
      appendLog(event.safety_triggered ? "warn" : "pup", event.safety_triggered ? "安全提醒" : "豆豆", event.reply);
    }
    const errors = eventErrors(event.errors);
    if (errors) appendLog("warn", "错误", errors);
  }
}

function renderEvents(message = "") {
  els.eventList.replaceChildren();
  if (message) {
    els.eventList.append(emptyEvents(message));
    return;
  }
  if (!state.events.length) {
    els.eventList.append(emptyEvents("还没有持久诊断记录"));
    return;
  }
  for (const event of state.events) {
    els.eventList.append(renderEvent(event));
  }
}

function renderEvent(event) {
  const item = document.createElement("article");
  item.className = `event-item${event.safety_triggered ? " safety" : ""}`;

  const meta = document.createElement("div");
  meta.className = "event-meta";
  meta.append(
    eventBadge(formatEventTime(event.time), ""),
    eventBadge(event.endpoint || "-", ""),
    eventBadge(event.mode || "-", ""),
    eventBadge(event.source || "-", sourceClass(event.source)),
    eventBadge(formatTimings(event.timings || {}), ""),
  );

  const body = document.createElement("div");
  body.className = "event-body";
  body.append(
    eventLine("听到", event.transcript || "-"),
    eventLine("回复", event.reply || "-"),
  );
  if (event.activity_label || event.safety_category) {
    body.append(eventLine("路由", event.activity_label || event.safety_category));
  }
  if (event.has_recording && event.trace_id) {
    body.append(recordingPlayback(event));
  }

  item.append(meta, body);
  const errors = eventErrors(event.errors);
  if (errors) {
    const errorEl = document.createElement("div");
    errorEl.className = "event-errors";
    errorEl.textContent = errors;
    item.append(errorEl);
  }
  return item;
}

function recordingPlayback(event) {
  const row = document.createElement("div");
  row.className = "recording-playback";
  const button = document.createElement("button");
  button.type = "button";
  button.className = "ghost-button compact";
  button.textContent = "加载录音";
  const holder = document.createElement("div");
  holder.className = "recording-audio";
  button.addEventListener("click", async () => {
    button.disabled = true;
    button.textContent = "加载中";
    try {
      const response = await fetch(`/api/recordings/${encodeURIComponent(event.trace_id)}`, { headers: authHeaders() });
      if (!response.ok) throw await responseError(response);
      const blob = await response.blob();
      const url = URL.createObjectURL(blob);
      holder.replaceChildren();
      const audio = document.createElement("audio");
      audio.controls = true;
      audio.preload = "none";
      audio.src = url;
      audio.addEventListener("emptied", () => URL.revokeObjectURL(url), { once: true });
      holder.append(audio);
      button.textContent = "已加载";
    } catch (error) {
      button.disabled = false;
      button.textContent = "重试录音";
      holder.textContent = error?.message || "录音加载失败";
    }
  });
  row.append(button, holder);
  return row;
}

function eventBadge(text, className) {
  const badge = document.createElement("span");
  badge.className = `event-badge${className ? ` ${className}` : ""}`;
  badge.textContent = text;
  return badge;
}

function eventLine(label, content) {
  const row = document.createElement("div");
  row.className = "event-line";
  const labelEl = document.createElement("span");
  labelEl.textContent = label;
  const contentEl = document.createElement("strong");
  contentEl.textContent = content;
  row.append(labelEl, contentEl);
  return row;
}

function emptyEvents(text) {
  const empty = document.createElement("div");
  empty.className = "empty-events";
  empty.textContent = text;
  return empty;
}

function sourceClass(source = "") {
  if (source === "safety") return "source-safety";
  if (source.startsWith("activity:")) return "source-activity";
  if (source === "dashscope" || source === "openai") return "source-model";
  return "";
}

function formatEventTime(value) {
  if (!value) return "-";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  return date.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit", second: "2-digit" });
}

function eventErrors(errors) {
  if (!errors) return "";
  const parts = [];
  if (errors.stt) parts.push(`STT: ${errors.stt}`);
  if (errors.chat) parts.push(`Chat: ${errors.chat}`);
  if (errors.tts) parts.push(`TTS: ${errors.tts}`);
  if (errors.recording) parts.push(`Recording: ${errors.recording}`);
  return parts.join(" / ");
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
    els.modePill.textContent = providerLabel(health.mode);
    els.modeText.textContent = health.mode;
    els.sourceText.textContent = "-";
    if (hasServerVoice()) {
      const speed = Number(health.tts_speed || 1).toFixed(2);
      els.voiceNote.textContent = `语音：${providerLabel(remoteVoiceProvider())} STT + TTS / ${health.tts_voice || "voice"} / ${speed}x`;
    } else if (browserSTT) {
      els.voiceNote.textContent = "语音：浏览器听写 + 本地 mock 回复";
    } else {
      els.voiceNote.textContent = "语音：当前浏览器不支持听写；可用文字或配置语音 provider";
    }
  } catch (error) {
    if (error.status === 401) {
      els.modePill.textContent = "未授权";
      els.modeText.textContent = "401";
      els.voiceNote.textContent = "访问 token 缺失或错误";
      appendLog("warn", "授权", "请用带 token 的链接打开，或清除后重新设置。");
    } else {
      els.modePill.textContent = "离线";
      els.modeText.textContent = "error";
      els.voiceNote.textContent = "语音：服务未连接";
    }
  }
}

async function startRecording(event) {
  event.preventDefault();
  unlockAudioPlayback();
  if (state.recording) return;

  if (shouldUseBrowserSpeech()) {
    startBrowserSpeech(event);
    return;
  }

  if (!hasServerVoice()) {
    showError(new Error("当前 mock 模式没有服务端语音识别。请用 Chrome 浏览器听写，或配置语音 provider。"));
    return;
  }

  if (!navigator.mediaDevices?.getUserMedia) {
    showError(new Error("这个浏览器不能录音"));
    return;
  }

  try {
    const stream = await navigator.mediaDevices.getUserMedia({ audio: true });
    const recorder = await createWavRecorder(stream);
    state.recorder = recorder;
    state.recording = true;
    document.body.classList.add("recording");
    els.recordLabel.textContent = "松开发送";
    startRecordingMeter(recorder);
    els.recordButton.setPointerCapture?.(event.pointerId);
  } catch (error) {
    state.recording = false;
    document.body.classList.remove("recording");
    els.recordLabel.textContent = "按住说话";
    stopRecordingMeter();
    showError(error);
  }
}

function stopRecording() {
  if (state.recognition) {
    state.recording = false;
    document.body.classList.remove("recording");
    els.recordLabel.textContent = "按住说话";
    stopRecordingMeter();
    state.recognition.stop();
    return;
  }

  if (!state.recording || !state.recorder) return;
  const recorder = state.recorder;
  state.recorder = null;
  state.recording = false;
  document.body.classList.remove("recording");
  els.recordLabel.textContent = "按住说话";
  stopRecordingMeter();
  recorder.stop().then((recording) => {
    sendRecording(recording.blob, recording.mimeType, recording.filename, recording.durationMs, recording.peakLevel);
  }).catch(showError);
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
  stopRecordingMeter();
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

async function sendRecording(blob, mimeType, filename, durationMs, peakLevel) {
  if (!blob || blob.size === 0) return;
  if (durationMs && durationMs < 260) {
    showError(new Error("录音太短，请按住说长一点点。"));
    return;
  }
  const form = new FormData();
  form.append("audio", blob, filename || "recording.wav");
  if (durationMs) form.append("duration_ms", String(durationMs));
  if (peakLevel) form.append("peak_level", String(peakLevel));

  setBusy("豆豆听一听");
  try {
    const response = await fetch("/api/voice", { method: "POST", headers: authHeaders(), body: form });
    if (!response.ok) throw await responseError(response);
    const payload = await response.json();
    if (payload.transcript) appendLog("child", "小朋友", payload.transcript);
    handleDogResponse(payload);
  } catch (error) {
    showError(error);
  }
}

async function handleDogResponse(payload) {
  const reply = payload.reply || "豆豆没有想好。";
  const className = payload.safety?.triggered ? "warn" : "pup";
  els.replyText.textContent = reply;
  els.modeText.textContent = payload.mode || "-";
  els.sourceText.textContent = payload.source || "-";
  appendLog(className, payload.safety?.triggered ? "安全提醒" : "豆豆", reply);

  if (payload.ai_error) appendLog("warn", "API", payload.ai_error);
  if (payload.tts_error) appendLog("warn", "TTS", payload.tts_error);
  if (payload.timings) appendLog("pup", "耗时", formatTimings(payload.timings));

  if (payload.audio_base64 && payload.audio_mime) {
    const played = await playBase64Audio(payload.audio_base64, payload.audio_mime);
    if (!played) speakInBrowser(reply);
  } else {
    speakInBrowser(reply);
  }
  loadEvents().catch(() => {});
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

function startRecordingMeter(recorder) {
  stopRecordingMeter();
  state.recordingStartedAt = Date.now();
  updateRecordingMeter(recorder);
  state.recordingTimer = window.setInterval(() => updateRecordingMeter(recorder), 120);
}

function updateRecordingMeter(recorder) {
  const durationMS = Date.now() - state.recordingStartedAt;
  const seconds = Math.max(0, durationMS / 1000);
  const level = recorder?.level?.() || 0;
  const fill = Math.max(0.04, Math.min(1, level * 8));
  document.body.style.setProperty("--voice-level", fill.toFixed(3));
  els.recordingTime.textContent = `${seconds.toFixed(1)}s`;
  els.recordLabel.textContent = durationMS < 700 ? "继续说" : "松开发送";
  els.replyText.textContent = durationMS > 900 && level < 0.012
    ? `豆豆听一听：${seconds.toFixed(1)}s，声音偏小`
    : `豆豆听一听：${seconds.toFixed(1)}s`;
}

function stopRecordingMeter() {
  if (state.recordingTimer) {
    window.clearInterval(state.recordingTimer);
    state.recordingTimer = null;
  }
  state.recordingStartedAt = 0;
  document.body.style.setProperty("--voice-level", "0");
  if (els.recordingTime) els.recordingTime.textContent = "0.0s";
}

function showError(error) {
  const message = error?.message || String(error);
  els.replyText.textContent = "豆豆这里出了一点小问题。";
  appendLog("warn", error?.status === 401 ? "授权" : "错误", message);
}

function formatTimings(timings) {
  const parts = [`总计 ${timings.total_ms || 0}ms`];
  if (timings.stt_ms) parts.push(`听写 ${timings.stt_ms}ms`);
  if (timings.reply_ms) parts.push(`回复 ${timings.reply_ms}ms`);
  if (timings.tts_ms) parts.push(`合成 ${timings.tts_ms}ms`);
  if (timings.audio_duration_ms) parts.push(`录音 ${(timings.audio_duration_ms / 1000).toFixed(1)}s`);
  if (timings.audio_peak) parts.push(`音量 ${formatAudioLevel(timings.audio_peak)}`);
  if (timings.audio_bytes) parts.push(`音频 ${Math.round(timings.audio_bytes / 1024)}KB`);
  return parts.join(" / ");
}

function formatAudioLevel(value) {
  return `${Math.round(Math.max(0, Math.min(1, value)) * 100)}%`;
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

function shouldUseBrowserSpeech() {
  return !hasServerVoice() && Boolean(browserSpeechRecognition());
}

function browserSpeechRecognition() {
  return window.SpeechRecognition || window.webkitSpeechRecognition || null;
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

function playBase64Audio(base64, mimeType) {
  const binary = atob(base64);
  const bytes = new Uint8Array(binary.length);
  for (let i = 0; i < binary.length; i += 1) {
    bytes[i] = binary.charCodeAt(i);
  }
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
    audio.addEventListener("ended", () => finish(true), { once: true });
    audio.addEventListener("error", () => finish(false), { once: true });
    audio.play().catch(() => finish(false));
  });
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

function speakInBrowser(text) {
  if (!("speechSynthesis" in window)) return;
  window.speechSynthesis.cancel();
  const utterance = new SpeechSynthesisUtterance(text);
  utterance.lang = "zh-CN";
  utterance.rate = 0.88;
  utterance.pitch = 1.25;
  window.speechSynthesis.speak(utterance);
}
