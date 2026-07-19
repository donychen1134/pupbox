#include <algorithm>
#include <cstddef>
#include <cstdint>
#include <cstdio>
#include <cstring>

#include "audio_board.h"
#include "backend_health.h"
#include "board_config.h"
#include "secrets.h"
#include "voice_client.h"
#include "wifi_station.h"

#include "esp_heap_caps.h"
#include "esp_check.h"
#include "esp_log.h"
#include "freertos/FreeRTOS.h"
#include "freertos/task.h"

namespace {
constexpr char kTag[] = "pupbox_loopback";
constexpr size_t kMaxRecordingSeconds = 8;
constexpr size_t kMaxSamples = kAudioSampleRate * kMaxRecordingSeconds;
constexpr size_t kChunkSamples = 240;
constexpr size_t kTapThresholdSamples = kAudioSampleRate * 450 / 1000;
constexpr size_t kInitialWaitSeconds = 8;
constexpr size_t kFollowupWaitSeconds = 30;
constexpr size_t kMaxConversationTurns = 20;
constexpr size_t kVADPreRollSamples = kAudioSampleRate * 300 / 1000;
constexpr size_t kVADStartFrames = 5;
constexpr size_t kVADEndSilenceFrames = 110;
constexpr size_t kVADTrailingSilenceFrames = 20;
constexpr size_t kVADMinimumActiveFrames = 20;
constexpr int32_t kVADMinimumLevel = 180;

enum class AutoCaptureResult {
    kSpeech,
    kTimeout,
    kCanceled,
};

#ifndef PUPBOX_ACCESS_TOKEN
#define PUPBOX_ACCESS_TOKEN ""
#endif

void BackendHealthTask(void*) {
    const esp_err_t time_result = SyncClock();
    if (time_result != ESP_OK) {
        ESP_LOGW(kTag, "clock sync failed; attempting HTTPS diagnostics anyway");
    }
    CheckBackendHealth(kBackendHost, kBackendHealthURL,
                       PUPBOX_ACCESS_TOKEN);
    vTaskDelete(nullptr);
}

bool ButtonPressed(AudioBoard& audio, uint32_t button_pin) {
    bool pressed = false;
    ESP_ERROR_CHECK(audio.IsButtonPressed(button_pin, &pressed));
    return pressed;
}

void WaitForButtonRelease(AudioBoard& audio, uint32_t button_pin) {
    while (ButtonPressed(audio, button_pin)) {
        vTaskDelay(pdMS_TO_TICKS(20));
    }
}

esp_err_t PlayErrorCue(AudioBoard& audio, int output_volume) {
    constexpr size_t kCueFrames = 10;
    constexpr int32_t kCueAmplitude = 1800;
    int16_t samples[kChunkSamples];

    ESP_RETURN_ON_ERROR(audio.SetOutputEnabled(true), kTag,
                        "enable error cue");
    ESP_RETURN_ON_ERROR(audio.SetOutputVolume(output_volume), kTag,
                        "set error cue volume");
    for (size_t frame = 0; frame < kCueFrames; ++frame) {
        const int32_t period = frame < kCueFrames / 2 ? 64 : 96;
        for (size_t index = 0; index < kChunkSamples; ++index) {
            const int32_t phase = static_cast<int32_t>(
                (frame * kChunkSamples + index) % period);
            samples[index] = phase < period / 2 ? kCueAmplitude
                                                : -kCueAmplitude;
        }
        ESP_RETURN_ON_ERROR(audio.Write(samples, kChunkSamples), kTag,
                            "write error cue");
    }
    return audio.SetOutputEnabled(false);
}

int32_t MeanAbsoluteLevel(const int16_t* samples, size_t sample_count) {
    int64_t total = 0;
    for (size_t index = 0; index < sample_count; ++index) {
        const int32_t sample = samples[index];
        total += sample < 0 ? -sample : sample;
    }
    return sample_count == 0 ? 0
                             : static_cast<int32_t>(total / sample_count);
}

esp_err_t PlayListeningCue(AudioBoard& audio, int output_volume) {
    constexpr size_t kCueFrames = 8;
    constexpr int32_t kCueAmplitude = 2400;
    constexpr int32_t kCuePeriodSamples = 32;
    int16_t samples[kChunkSamples];

    ESP_RETURN_ON_ERROR(audio.SetOutputEnabled(true), kTag,
                        "enable listening cue");
    ESP_RETURN_ON_ERROR(audio.SetOutputVolume(output_volume), kTag,
                        "set listening cue volume");
    for (size_t frame = 0; frame < kCueFrames; ++frame) {
        for (size_t index = 0; index < kChunkSamples; ++index) {
            const int32_t phase = static_cast<int32_t>(
                (frame * kChunkSamples + index) % kCuePeriodSamples);
            const int32_t rising = phase < kCuePeriodSamples / 2
                                       ? phase
                                       : kCuePeriodSamples - phase;
            samples[index] = static_cast<int16_t>(
                -kCueAmplitude +
                rising * (kCueAmplitude * 4 / kCuePeriodSamples));
        }
        ESP_RETURN_ON_ERROR(audio.Write(samples, kChunkSamples), kTag,
                            "write listening cue");
    }
    return audio.SetOutputEnabled(false);
}

esp_err_t PlayCaptureCompleteCue(AudioBoard& audio, int output_volume) {
    constexpr size_t kCueFrames = 6;
    constexpr int32_t kCueAmplitude = 1700;
    int16_t samples[kChunkSamples];

    ESP_RETURN_ON_ERROR(audio.SetOutputEnabled(true), kTag,
                        "enable capture complete cue");
    ESP_RETURN_ON_ERROR(audio.SetOutputVolume(output_volume), kTag,
                        "set capture complete cue volume");
    for (size_t frame = 0; frame < kCueFrames; ++frame) {
        const int32_t period = frame < kCueFrames / 2 ? 40 : 56;
        for (size_t index = 0; index < kChunkSamples; ++index) {
            const int32_t phase = static_cast<int32_t>(
                (frame * kChunkSamples + index) % period);
            samples[index] = phase < period / 2 ? kCueAmplitude
                                                : -kCueAmplitude;
        }
        ESP_RETURN_ON_ERROR(audio.Write(samples, kChunkSamples), kTag,
                            "write capture complete cue");
    }
    return audio.SetOutputEnabled(false);
}

esp_err_t PlayNurseryMelody(AudioBoard& audio, int output_volume) {
    constexpr int32_t kNotePeriods[] = {46, 41, 36, 46, 46, 41, 36, 31};
    constexpr size_t kFramesPerNote = 12;
    constexpr size_t kSoundFramesPerNote = 10;
    constexpr int32_t kAmplitude = 1350;
    int16_t samples[kChunkSamples];

    ESP_RETURN_ON_ERROR(audio.SetOutputEnabled(true), kTag,
                        "enable nursery melody");
    ESP_RETURN_ON_ERROR(audio.SetOutputVolume(output_volume), kTag,
                        "set nursery melody volume");
    for (int32_t period : kNotePeriods) {
        for (size_t frame = 0; frame < kFramesPerNote; ++frame) {
            if (frame >= kSoundFramesPerNote) {
                std::memset(samples, 0, sizeof(samples));
            } else {
                for (size_t index = 0; index < kChunkSamples; ++index) {
                    const int32_t phase = static_cast<int32_t>(
                        (frame * kChunkSamples + index) % period);
                    const int32_t triangle = phase < period / 2
                                                 ? phase
                                                 : period - phase;
                    samples[index] = static_cast<int16_t>(
                        -kAmplitude + triangle * (kAmplitude * 4 / period));
                }
            }
            ESP_RETURN_ON_ERROR(audio.Write(samples, kChunkSamples), kTag,
                                "write nursery melody");
        }
    }
    return audio.SetOutputEnabled(false);
}

esp_err_t PlaySessionEndCue(AudioBoard& audio, int output_volume) {
    constexpr size_t kToneFrames = 6;
    constexpr size_t kSilenceFrames = 3;
    constexpr int32_t kCueAmplitude = 1400;
    int16_t samples[kChunkSamples];

    ESP_RETURN_ON_ERROR(audio.SetOutputEnabled(true), kTag,
                        "enable session end cue");
    ESP_RETURN_ON_ERROR(audio.SetOutputVolume(output_volume), kTag,
                        "set session end cue volume");
    for (size_t frame = 0; frame < kToneFrames * 2 + kSilenceFrames; ++frame) {
        if (frame >= kToneFrames &&
            frame < kToneFrames + kSilenceFrames) {
            std::memset(samples, 0, sizeof(samples));
        } else {
            const int32_t period = frame < kToneFrames ? 48 : 72;
            for (size_t index = 0; index < kChunkSamples; ++index) {
                const int32_t phase = static_cast<int32_t>(
                    (frame * kChunkSamples + index) % period);
                samples[index] = phase < period / 2 ? kCueAmplitude
                                                    : -kCueAmplitude;
            }
        }
        ESP_RETURN_ON_ERROR(audio.Write(samples, kChunkSamples), kTag,
                            "write session end cue");
    }
    return audio.SetOutputEnabled(false);
}

esp_err_t PlaySessionFarewell(AudioBoard& audio, int output_volume) {
    if (!WifiConnected()) {
        return PlaySessionEndCue(audio, output_volume);
    }
    VoiceReply farewell = {};
    std::snprintf(farewell.reply, sizeof(farewell.reply), "%s",
                  "拜拜，豆豆会等你下次再来。");
    PlaybackMetrics metrics = {};
    const esp_err_t result = StreamVoiceReply(
        farewell, PUPBOX_ACCESS_TOKEN, &audio, output_volume, &metrics);
    if (result == ESP_OK) {
        return ESP_OK;
    }
    ESP_LOGW(kTag, "spoken farewell failed; using local cue: %s",
             esp_err_to_name(result));
    return PlaySessionEndCue(audio, output_volume);
}

esp_err_t CaptureAutomaticUtterance(AudioBoard& audio, int16_t* recording,
                                    size_t wait_seconds,
                                    size_t* recorded_samples,
                                    AutoCaptureResult* capture_result) {
    if (recording == nullptr || recorded_samples == nullptr ||
        capture_result == nullptr || wait_seconds == 0) {
        return ESP_ERR_INVALID_ARG;
    }
    *recorded_samples = 0;
    *capture_result = AutoCaptureResult::kTimeout;
    const size_t wait_limit_samples = kAudioSampleRate * wait_seconds;
    size_t listened_samples = 0;
    size_t pre_roll_samples = 0;
    size_t pre_roll_write = 0;
    size_t consecutive_active_frames = 0;
    size_t active_frames = 0;
    size_t quiet_frames = 0;
    int32_t noise_level = 60;
    int32_t speech_threshold = kVADMinimumLevel;
    bool speech_started = false;

    ESP_RETURN_ON_ERROR(audio.SetInputEnabled(true), kTag,
                        "enable automatic recording");
    int16_t frame[kChunkSamples];
    while (true) {
        if (ButtonPressed(audio, kRecordButtonPin)) {
            ESP_RETURN_ON_ERROR(audio.SetInputEnabled(false), kTag,
                                "disable canceled automatic recording");
            WaitForButtonRelease(audio, kRecordButtonPin);
            *recorded_samples = 0;
            *capture_result = AutoCaptureResult::kCanceled;
            ESP_LOGI(kTag, "automatic listening canceled by K2");
            return ESP_OK;
        }
        const esp_err_t read_result = audio.Read(frame, kChunkSamples);
        if (read_result != ESP_OK) {
            audio.SetInputEnabled(false);
            return read_result;
        }
        listened_samples += kChunkSamples;

        const int32_t frame_level = MeanAbsoluteLevel(frame, kChunkSamples);
        const bool active = frame_level >= speech_threshold;
        if (!speech_started) {
            for (size_t index = 0; index < kChunkSamples; ++index) {
                recording[pre_roll_write] = frame[index];
                pre_roll_write = (pre_roll_write + 1) % kVADPreRollSamples;
                pre_roll_samples =
                    std::min(kVADPreRollSamples, pre_roll_samples + 1);
            }
            if (active) {
                ++consecutive_active_frames;
            } else {
                consecutive_active_frames = 0;
                noise_level = (noise_level * 31 + frame_level) / 32;
                speech_threshold =
                    std::max(kVADMinimumLevel, noise_level * 3 + 80);
            }
            if (consecutive_active_frames >= kVADStartFrames) {
                if (pre_roll_samples == kVADPreRollSamples &&
                    pre_roll_write != 0) {
                    std::rotate(recording, recording + pre_roll_write,
                                recording + kVADPreRollSamples);
                }
                *recorded_samples = pre_roll_samples;
                speech_started = true;
                active_frames = consecutive_active_frames;
                quiet_frames = 0;
                ESP_LOGI(kTag,
                         "automatic speech started: noise=%ld threshold=%ld",
                         static_cast<long>(noise_level),
                         static_cast<long>(speech_threshold));
            } else if (listened_samples >= wait_limit_samples) {
                *recorded_samples = 0;
                ESP_RETURN_ON_ERROR(audio.SetInputEnabled(false), kTag,
                                    "disable timed-out recording");
                *capture_result = AutoCaptureResult::kTimeout;
                ESP_LOGI(kTag,
                         "automatic listening timed out after %u seconds",
                         static_cast<unsigned>(wait_seconds));
                return ESP_OK;
            }
            continue;
        }

        if (*recorded_samples + kChunkSamples > kMaxSamples) {
            break;
        }
        std::memcpy(recording + *recorded_samples, frame,
                    sizeof(frame));
        *recorded_samples += kChunkSamples;

        if (active) {
            ++active_frames;
            quiet_frames = 0;
        } else {
            ++quiet_frames;
        }
        if (quiet_frames < kVADEndSilenceFrames) {
            continue;
        }
        if (active_frames < kVADMinimumActiveFrames) {
            ESP_LOGI(kTag, "ignored short automatic sound: active=%u frames",
                     static_cast<unsigned>(active_frames));
            *recorded_samples = 0;
            speech_started = false;
            pre_roll_samples = 0;
            pre_roll_write = 0;
            consecutive_active_frames = 0;
            active_frames = 0;
            quiet_frames = 0;
            continue;
        }

        const size_t removable_frames =
            quiet_frames - kVADTrailingSilenceFrames;
        const size_t removable_samples = removable_frames * kChunkSamples;
        *recorded_samples -=
            std::min(*recorded_samples, removable_samples);
        ESP_RETURN_ON_ERROR(audio.SetInputEnabled(false), kTag,
                            "disable completed automatic recording");
        *capture_result = AutoCaptureResult::kSpeech;
        ESP_LOGI(kTag,
                 "automatic speech ended: active=%u frames audio=%.2f seconds",
                 static_cast<unsigned>(active_frames),
                 static_cast<double>(*recorded_samples) / kAudioSampleRate);
        return ESP_OK;
    }

    ESP_RETURN_ON_ERROR(audio.SetInputEnabled(false), kTag,
                        "disable full automatic recording");
    if (!speech_started || active_frames < kVADMinimumActiveFrames) {
        *recorded_samples = 0;
        *capture_result = AutoCaptureResult::kTimeout;
    } else {
        *capture_result = AutoCaptureResult::kSpeech;
    }
    ESP_LOGI(kTag, "automatic recording reached the %.0f second limit",
             static_cast<double>(kMaxRecordingSeconds));
    return ESP_OK;
}

bool EndsConversation(const VoiceReply& reply) {
    return std::strcmp(reply.source, "activity:farewell") == 0;
}

bool RunVoiceTurn(AudioBoard& audio, const int16_t* recording,
                  size_t recorded_samples, int output_volume,
                  VoiceReply* reply) {
    if (recorded_samples < kAudioSampleRate / 5 || reply == nullptr) {
        ESP_LOGW(kTag, "recording too short: %u samples",
                 static_cast<unsigned>(recorded_samples));
        return false;
    }
    if (!WifiConnected()) {
        ESP_LOGW(kTag, "offline; skipping voice request");
        ESP_ERROR_CHECK(PlayErrorCue(audio, output_volume));
        return false;
    }

    ESP_LOGI(kTag, "sending %.2f seconds to Pupbox",
             static_cast<double>(recorded_samples) / kAudioSampleRate);
    ESP_LOGI(kTag, "main stack free before request: %u bytes",
             static_cast<unsigned>(uxTaskGetStackHighWaterMark(nullptr)));
    const esp_err_t upload_result = UploadVoiceRecording(
        recording, recorded_samples, PUPBOX_ACCESS_TOKEN, reply);
    ESP_LOGI(kTag, "main stack free after request: %u bytes",
             static_cast<unsigned>(uxTaskGetStackHighWaterMark(nullptr)));
    if (upload_result != ESP_OK) {
        ESP_LOGE(kTag, "voice request failed: %s",
                 esp_err_to_name(upload_result));
        ESP_ERROR_CHECK(PlayErrorCue(audio, output_volume));
        return false;
    }

    if (std::strcmp(reply->source, "activity:nursery_rhyme") == 0) {
        const esp_err_t melody_result =
            PlayNurseryMelody(audio, output_volume);
        if (melody_result != ESP_OK) {
            ESP_LOGW(kTag, "nursery melody failed: %s",
                     esp_err_to_name(melody_result));
        }
    }

    PlaybackMetrics metrics = {};
    const esp_err_t playback_result = StreamVoiceReply(
        *reply, PUPBOX_ACCESS_TOKEN, &audio, output_volume, &metrics);
    const esp_err_t metrics_result = ReportTurnMetrics(
        *reply, metrics, PUPBOX_ACCESS_TOKEN);
    if (metrics_result != ESP_OK) {
        ESP_LOGW(kTag, "turn metrics were not persisted: %s",
                 esp_err_to_name(metrics_result));
    }
    if (playback_result != ESP_OK) {
        ESP_LOGE(kTag, "remote reply playback failed: %s",
                 esp_err_to_name(playback_result));
        return false;
    }
    return true;
}
}

extern "C" void app_main() {
    const esp_err_t wifi_result =
        ConnectWifi(PUPBOX_WIFI_SSID, PUPBOX_WIFI_PASSWORD);
    if (wifi_result != ESP_OK) {
        ESP_LOGW(kTag, "Wi-Fi is not ready; local audio remains available");
    }

    AudioBoard audio;
    ESP_ERROR_CHECK(audio.Init());

    auto* recording = static_cast<int16_t*>(heap_caps_malloc(
        kMaxSamples * sizeof(int16_t),
        MALLOC_CAP_SPIRAM | MALLOC_CAP_8BIT));
    if (recording == nullptr) {
        ESP_LOGE(kTag, "failed to allocate recording buffer");
        return;
    }

    if (wifi_result == ESP_OK &&
        xTaskCreate(BackendHealthTask, "backend_health", 8192, nullptr, 4,
                    nullptr) != pdPASS) {
        ESP_LOGW(kTag, "failed to start backend health task");
    }

    int output_volume = kInitialOutputVolume;
    ESP_LOGI(kTag,
             "ready: K1 volume up, tap K2 for automatic recording, hold K2 for push-to-talk, K3 volume down");
    ESP_LOGI(kTag, "output volume: %d", output_volume);
    while (true) {
        if (ButtonPressed(audio, kVolumeUpButtonPin)) {
            output_volume = std::min(100, output_volume + kOutputVolumeStep);
            ESP_LOGI(kTag, "output volume: %d", output_volume);
            WaitForButtonRelease(audio, kVolumeUpButtonPin);
            continue;
        }
        if (ButtonPressed(audio, kVolumeDownButtonPin)) {
            output_volume = std::max(0, output_volume - kOutputVolumeStep);
            ESP_LOGI(kTag, "output volume: %d", output_volume);
            WaitForButtonRelease(audio, kVolumeDownButtonPin);
            continue;
        }
        if (!ButtonPressed(audio, kRecordButtonPin)) {
            vTaskDelay(pdMS_TO_TICKS(20));
            continue;
        }

        ESP_LOGI(kTag, "K2 recording started");
        ESP_ERROR_CHECK(audio.SetInputEnabled(true));
        size_t recorded_samples = 0;
        while (ButtonPressed(audio, kRecordButtonPin) &&
               recorded_samples < kMaxSamples) {
            const size_t count =
                std::min(kChunkSamples, kMaxSamples - recorded_samples);
            if (audio.Read(recording + recorded_samples, count) != ESP_OK) {
                ESP_LOGE(kTag, "audio read failed");
                break;
            }
            recorded_samples += count;
        }
        ESP_ERROR_CHECK(audio.SetInputEnabled(false));

        const bool conversation_mode =
            recorded_samples < kTapThresholdSamples;
        if (conversation_mode) {
            ESP_LOGI(kTag,
                     "K2 tap detected; conversation starts after cue");
            ESP_ERROR_CHECK(PlayListeningCue(audio, output_volume));
            AutoCaptureResult capture_result = AutoCaptureResult::kTimeout;
            ESP_ERROR_CHECK(CaptureAutomaticUtterance(
                audio, recording, kInitialWaitSeconds, &recorded_samples,
                &capture_result));
            if (capture_result != AutoCaptureResult::kSpeech) {
                ESP_LOGI(kTag, "conversation ended before the first turn");
                continue;
            }
        } else {
            ESP_LOGI(kTag, "K2 hold detected; push-to-talk recording ended");
        }

        ESP_ERROR_CHECK(PlayCaptureCompleteCue(audio, output_volume));
        VoiceReply reply = {};
        if (!RunVoiceTurn(audio, recording, recorded_samples,
                          output_volume, &reply)) {
            continue;
        }
        if (!conversation_mode) {
            WaitForButtonRelease(audio, kRecordButtonPin);
            vTaskDelay(pdMS_TO_TICKS(150));
            continue;
        }

        size_t completed_turns = 1;
        while (completed_turns < kMaxConversationTurns &&
               !EndsConversation(reply)) {
            ESP_LOGI(kTag,
                     "conversation waiting for follow-up: turn=%u timeout=%u seconds",
                     static_cast<unsigned>(completed_turns + 1),
                     static_cast<unsigned>(kFollowupWaitSeconds));
            ESP_ERROR_CHECK(PlayListeningCue(audio, output_volume));
            AutoCaptureResult capture_result = AutoCaptureResult::kTimeout;
            ESP_ERROR_CHECK(CaptureAutomaticUtterance(
                audio, recording, kFollowupWaitSeconds, &recorded_samples,
                &capture_result));
            if (capture_result == AutoCaptureResult::kCanceled) {
                ESP_LOGI(kTag, "conversation canceled by K2");
                break;
            }
            if (capture_result == AutoCaptureResult::kTimeout) {
                ESP_LOGI(kTag, "conversation ended after inactivity");
                ESP_ERROR_CHECK(PlaySessionFarewell(audio, output_volume));
                break;
            }
            reply = {};
            if (!RunVoiceTurn(audio, recording, recorded_samples,
                              output_volume, &reply)) {
                ESP_LOGW(kTag, "conversation ended after a failed turn");
                break;
            }
            ++completed_turns;
        }
        if (EndsConversation(reply)) {
            ESP_LOGI(kTag, "conversation ended by farewell intent");
        } else if (completed_turns >= kMaxConversationTurns) {
            ESP_LOGI(kTag, "conversation reached the %u turn limit",
                     static_cast<unsigned>(kMaxConversationTurns));
            ESP_ERROR_CHECK(PlaySessionFarewell(audio, output_volume));
        }
        vTaskDelay(pdMS_TO_TICKS(150));
    }
}
