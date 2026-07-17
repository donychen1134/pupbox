#include <algorithm>
#include <cstddef>
#include <cstdint>
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
constexpr size_t kAutoWaitSamples = kAudioSampleRate * 8;
constexpr size_t kVADPreRollSamples = kAudioSampleRate * 300 / 1000;
constexpr size_t kVADStartFrames = 5;
constexpr size_t kVADEndSilenceFrames = 110;
constexpr size_t kVADTrailingSilenceFrames = 20;
constexpr size_t kVADMinimumActiveFrames = 20;
constexpr int32_t kVADMinimumLevel = 180;

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

esp_err_t PlayLocalRecording(AudioBoard& audio, const int16_t* recording,
                             size_t recorded_samples, int output_volume) {
    ESP_RETURN_ON_ERROR(audio.SetOutputEnabled(true), kTag,
                        "enable local playback");
    ESP_RETURN_ON_ERROR(audio.SetOutputVolume(output_volume), kTag,
                        "set local playback volume");
    for (size_t offset = 0; offset < recorded_samples;) {
        const size_t count =
            std::min(kChunkSamples, recorded_samples - offset);
        ESP_RETURN_ON_ERROR(audio.Write(recording + offset, count), kTag,
                            "write local playback");
        offset += count;
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

esp_err_t CaptureAutomaticUtterance(AudioBoard& audio, int16_t* recording,
                                    size_t* recorded_samples) {
    if (recording == nullptr || recorded_samples == nullptr) {
        return ESP_ERR_INVALID_ARG;
    }
    *recorded_samples = 0;
    size_t listened_samples = 0;
    size_t consecutive_active_frames = 0;
    size_t active_frames = 0;
    size_t quiet_frames = 0;
    int32_t noise_level = 60;
    int32_t speech_threshold = kVADMinimumLevel;
    bool speech_started = false;

    ESP_RETURN_ON_ERROR(audio.SetInputEnabled(true), kTag,
                        "enable automatic recording");
    while (*recorded_samples + kChunkSamples <= kMaxSamples) {
        int16_t* frame = recording + *recorded_samples;
        const esp_err_t read_result = audio.Read(frame, kChunkSamples);
        if (read_result != ESP_OK) {
            audio.SetInputEnabled(false);
            return read_result;
        }
        *recorded_samples += kChunkSamples;
        listened_samples += kChunkSamples;

        const int32_t frame_level = MeanAbsoluteLevel(frame, kChunkSamples);
        const bool active = frame_level >= speech_threshold;
        if (!speech_started) {
            if (active) {
                ++consecutive_active_frames;
            } else {
                consecutive_active_frames = 0;
                noise_level = (noise_level * 31 + frame_level) / 32;
                speech_threshold =
                    std::max(kVADMinimumLevel, noise_level * 3 + 80);
            }
            if (consecutive_active_frames >= kVADStartFrames) {
                const size_t keep_from =
                    *recorded_samples > kVADPreRollSamples
                        ? *recorded_samples - kVADPreRollSamples
                        : 0;
                if (keep_from > 0) {
                    std::memmove(recording, recording + keep_from,
                                 (*recorded_samples - keep_from) *
                                     sizeof(int16_t));
                    *recorded_samples -= keep_from;
                }
                speech_started = true;
                active_frames = consecutive_active_frames;
                quiet_frames = 0;
                ESP_LOGI(kTag,
                         "automatic speech started: noise=%ld threshold=%ld",
                         static_cast<long>(noise_level),
                         static_cast<long>(speech_threshold));
            } else if (listened_samples >= kAutoWaitSamples) {
                *recorded_samples = 0;
                ESP_RETURN_ON_ERROR(audio.SetInputEnabled(false), kTag,
                                    "disable timed-out recording");
                ESP_LOGI(kTag, "automatic listening timed out without speech");
                return ESP_OK;
            }
            continue;
        }

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
    }
    ESP_LOGI(kTag, "automatic recording reached the %.0f second limit",
             static_cast<double>(kMaxRecordingSeconds));
    return ESP_OK;
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

        const bool automatic_mode = recorded_samples < kTapThresholdSamples;
        if (automatic_mode) {
            ESP_LOGI(kTag, "K2 tap detected; automatic listening starts after cue");
            ESP_ERROR_CHECK(PlayListeningCue(audio, output_volume));
            ESP_ERROR_CHECK(CaptureAutomaticUtterance(
                audio, recording, &recorded_samples));
            if (recorded_samples == 0) {
                continue;
            }
        } else {
            ESP_LOGI(kTag, "K2 hold detected; push-to-talk recording ended");
        }

        if (recorded_samples < kAudioSampleRate / 5) {
            ESP_LOGW(kTag, "recording too short: %u samples",
                     static_cast<unsigned>(recorded_samples));
            WaitForButtonRelease(audio, kRecordButtonPin);
            continue;
        }

        if (wifi_result == ESP_OK) {
            ESP_LOGI(kTag, "sending %.2f seconds to Pupbox",
                     static_cast<double>(recorded_samples) / kAudioSampleRate);
            ESP_LOGI(kTag, "main stack free before request: %u bytes",
                     static_cast<unsigned>(
                         uxTaskGetStackHighWaterMark(nullptr)));
            VoiceReply reply = {};
            const esp_err_t upload_result = UploadVoiceRecording(
                recording, recorded_samples, PUPBOX_ACCESS_TOKEN, &reply);
            ESP_LOGI(kTag, "main stack free after request: %u bytes",
                     static_cast<unsigned>(
                         uxTaskGetStackHighWaterMark(nullptr)));
            if (upload_result == ESP_OK) {
                const esp_err_t playback_result = StreamVoiceReply(
                    reply, PUPBOX_ACCESS_TOKEN, &audio, output_volume);
                if (playback_result != ESP_OK) {
                    ESP_LOGE(kTag, "remote reply playback failed: %s",
                             esp_err_to_name(playback_result));
                }
            } else {
                ESP_LOGE(kTag, "voice request failed: %s",
                         esp_err_to_name(upload_result));
                ESP_LOGW(kTag, "playing local diagnostic fallback");
                ESP_ERROR_CHECK(PlayLocalRecording(
                    audio, recording, recorded_samples, output_volume));
            }
        } else {
            ESP_LOGW(kTag, "offline; playing local recording");
            ESP_ERROR_CHECK(PlayLocalRecording(
                audio, recording, recorded_samples, output_volume));
        }

        WaitForButtonRelease(audio, kRecordButtonPin);
        vTaskDelay(pdMS_TO_TICKS(150));
    }
}
