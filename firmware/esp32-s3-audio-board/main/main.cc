#include <algorithm>
#include <cstddef>
#include <cstdint>

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
    ESP_LOGI(kTag, "ready: K1 volume up, hold K2 to record, K3 volume down");
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

        ESP_LOGI(kTag, "recording started");
        ESP_ERROR_CHECK(audio.SetInputEnabled(true));
        size_t recorded_samples = 0;
        while (ButtonPressed(audio, kRecordButtonPin) && recorded_samples < kMaxSamples) {
            const size_t count =
                std::min(kChunkSamples, kMaxSamples - recorded_samples);
            if (audio.Read(recording + recorded_samples, count) != ESP_OK) {
                ESP_LOGE(kTag, "audio read failed");
                break;
            }
            recorded_samples += count;
        }
        ESP_ERROR_CHECK(audio.SetInputEnabled(false));

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
