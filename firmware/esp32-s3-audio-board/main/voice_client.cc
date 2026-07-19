#include "voice_client.h"

#include <algorithm>
#include <cstdint>
#include <cstdio>
#include <cstring>

#include "audio_board.h"
#include "board_config.h"
#include "cJSON.h"
#include "esp_check.h"
#include "esp_crt_bundle.h"
#include "esp_heap_caps.h"
#include "esp_http_client.h"
#include "esp_log.h"
#include "esp_random.h"
#include "esp_timer.h"
#include "freertos/FreeRTOS.h"
#include "freertos/stream_buffer.h"
#include "freertos/task.h"
#include "mbedtls/base64.h"

namespace {
constexpr char kTag[] = "voice_client";
constexpr char kBoundary[] = "----pupbox-esp32-s3-audio";
char session_id[48] = "esp32-s3-audio-board-boot";
constexpr size_t kResponseCapacity = 16 * 1024;
constexpr size_t kStreamLineCapacity = 20 * 1024;
constexpr size_t kDecodedAudioCapacity = 12 * 1024;
constexpr size_t kPlaybackBufferCapacity = 3 * 1024 * 1024;
constexpr size_t kPlaybackPrebufferBytes = 72 * 1024;
constexpr size_t kPlaybackRebufferBytes = 48 * 1024;
constexpr size_t kPlaybackChunkBytes = 960;
constexpr int64_t kMinimumStreamingBytesPerSecond = 60 * 1024;
constexpr int kVoiceTimeoutMs = 60000;

enum class ResponseMode {
    kNone,
    kJSON,
    kSpeechStream,
};

struct ClientState {
    ResponseMode mode = ResponseMode::kNone;
    char* response = nullptr;
    size_t response_capacity = 0;
    size_t response_size = 0;
    char* line = nullptr;
    size_t line_capacity = 0;
    size_t line_size = 0;
    uint8_t* decoded_audio = nullptr;
    size_t decoded_audio_capacity = 0;
    AudioBoard* audio = nullptr;
    int output_volume = 0;
    bool output_started = false;
    bool stream_done = false;
    volatile bool producer_done = false;
    bool playback_task_started = false;
    StreamBufferHandle_t playback_buffer = nullptr;
    TaskHandle_t playback_waiter = nullptr;
    size_t audio_bytes = 0;
    size_t audio_chunks = 0;
    size_t audio_underruns = 0;
    int64_t audio_underrun_ms = 0;
    int64_t request_started_us = 0;
    int64_t producer_finished_us = 0;
    int64_t first_audio_us = 0;
    int64_t playback_started_us = 0;
    int64_t playback_finished_us = 0;
    int64_t last_audio_us = 0;
    int64_t max_audio_gap_ms = 0;
    int64_t write_ms = 0;
    char tts_cache[16] = {};
    esp_err_t error = ESP_OK;
};

esp_http_client_handle_t client;
ClientState state;
StaticStreamBuffer_t playback_buffer_control;

int64_t ElapsedMs(int64_t started_us, int64_t finished_us) {
    return (finished_us - started_us) / 1000;
}

void SetPlaybackError(PlaybackMetrics* metrics, esp_err_t error) {
    if (metrics == nullptr || error == ESP_OK) {
        return;
    }
    std::snprintf(metrics->playback_error,
                  sizeof(metrics->playback_error), "%s",
                  esp_err_to_name(error));
}

void WriteLE16(uint8_t* output, uint16_t value) {
    output[0] = static_cast<uint8_t>(value);
    output[1] = static_cast<uint8_t>(value >> 8);
}

void WriteLE32(uint8_t* output, uint32_t value) {
    output[0] = static_cast<uint8_t>(value);
    output[1] = static_cast<uint8_t>(value >> 8);
    output[2] = static_cast<uint8_t>(value >> 16);
    output[3] = static_cast<uint8_t>(value >> 24);
}

void WriteWAVHeader(uint8_t* output, size_t audio_bytes, uint32_t sample_rate) {
    std::memcpy(output, "RIFF", 4);
    WriteLE32(output + 4, static_cast<uint32_t>(36 + audio_bytes));
    std::memcpy(output + 8, "WAVEfmt ", 8);
    WriteLE32(output + 16, 16);
    WriteLE16(output + 20, 1);
    WriteLE16(output + 22, 1);
    WriteLE32(output + 24, sample_rate);
    WriteLE32(output + 28, sample_rate * sizeof(int16_t));
    WriteLE16(output + 32, sizeof(int16_t));
    WriteLE16(output + 34, 16);
    std::memcpy(output + 36, "data", 4);
    WriteLE32(output + 40, static_cast<uint32_t>(audio_bytes));
}

size_t ResampleVoiceForUpload(const int16_t* input, size_t input_samples,
                              uint8_t* output) {
    static_assert(kAudioSampleRate == 24000);
    static_assert(kVoiceUploadSampleRate == 16000);
    const size_t complete_groups = input_samples / 3;
    for (size_t group = 0; group < complete_groups; ++group) {
        const size_t input_offset = group * 3;
        const size_t output_offset = group * 2;
        const int16_t first = input[input_offset];
        const int16_t second = static_cast<int16_t>(
            (static_cast<int32_t>(input[input_offset + 1]) +
             static_cast<int32_t>(input[input_offset + 2])) /
            2);
        WriteLE16(output + output_offset * sizeof(int16_t),
                  static_cast<uint16_t>(first));
        WriteLE16(output + (output_offset + 1) * sizeof(int16_t),
                  static_cast<uint16_t>(second));
    }
    return complete_groups * 2;
}

void CopyJSONString(cJSON* root, const char* name, char* output,
                    size_t output_capacity) {
    output[0] = '\0';
    cJSON* value = cJSON_GetObjectItemCaseSensitive(root, name);
    if (cJSON_IsString(value) && value->valuestring != nullptr) {
        std::snprintf(output, output_capacity, "%s", value->valuestring);
    }
}

int64_t JSONInt64(cJSON* root, const char* name) {
    cJSON* value = cJSON_GetObjectItemCaseSensitive(root, name);
    return cJSON_IsNumber(value) ? static_cast<int64_t>(value->valuedouble) : 0;
}

esp_err_t ProcessSpeechLine(ClientState* current) {
    if (current->line_size == 0) {
        return ESP_OK;
    }
    cJSON* root = cJSON_ParseWithLength(current->line, current->line_size);
    current->line_size = 0;
    if (root == nullptr) {
        return ESP_ERR_INVALID_RESPONSE;
    }
    cJSON* type = cJSON_GetObjectItemCaseSensitive(root, "type");
    if (!cJSON_IsString(type) || type->valuestring == nullptr) {
        cJSON_Delete(root);
        return ESP_ERR_INVALID_RESPONSE;
    }
    cJSON* cache = cJSON_GetObjectItemCaseSensitive(root, "cache");
    if (cJSON_IsString(cache) && cache->valuestring != nullptr) {
        std::snprintf(current->tts_cache, sizeof(current->tts_cache), "%s",
                      cache->valuestring);
    }
    if (std::strcmp(type->valuestring, "done") == 0) {
        current->stream_done = true;
        cJSON_Delete(root);
        return ESP_OK;
    }
    if (std::strcmp(type->valuestring, "error") == 0) {
        cJSON_Delete(root);
        return ESP_FAIL;
    }
    if (std::strcmp(type->valuestring, "audio") != 0) {
        cJSON_Delete(root);
        return ESP_OK;
    }

    cJSON* sample_rate = cJSON_GetObjectItemCaseSensitive(root, "sample_rate");
    cJSON* encoded = cJSON_GetObjectItemCaseSensitive(root, "audio_base64");
    if (!cJSON_IsNumber(sample_rate) || sample_rate->valueint != kAudioSampleRate ||
        !cJSON_IsString(encoded) || encoded->valuestring == nullptr) {
        cJSON_Delete(root);
        return ESP_ERR_NOT_SUPPORTED;
    }
    size_t decoded_size = 0;
    const int decode_result = mbedtls_base64_decode(
        current->decoded_audio, current->decoded_audio_capacity, &decoded_size,
        reinterpret_cast<const uint8_t*>(encoded->valuestring),
        std::strlen(encoded->valuestring));
    cJSON_Delete(root);
    if (decode_result != 0 || decoded_size == 0 || decoded_size % 2 != 0) {
        return ESP_ERR_INVALID_RESPONSE;
    }
    const int64_t arrived_us = esp_timer_get_time();
    if (current->first_audio_us == 0) {
        current->first_audio_us = arrived_us;
    } else if (current->last_audio_us != 0) {
        current->max_audio_gap_ms = std::max(
            current->max_audio_gap_ms,
            ElapsedMs(current->last_audio_us, arrived_us));
    }
    current->last_audio_us = arrived_us;

    size_t sent = 0;
    while (sent < decoded_size) {
        const size_t written = xStreamBufferSend(
            current->playback_buffer, current->decoded_audio + sent,
            decoded_size - sent, pdMS_TO_TICKS(5000));
        if (written == 0) {
            return ESP_ERR_TIMEOUT;
        }
        sent += written;
    }
    current->audio_bytes += decoded_size;
    ++current->audio_chunks;
    return ESP_OK;
}

void PlaybackTask(void* argument) {
    auto* current = static_cast<ClientState*>(argument);
    uint8_t chunk[kPlaybackChunkBytes];
    while (!current->producer_done &&
           xStreamBufferBytesAvailable(current->playback_buffer) <
               kPlaybackPrebufferBytes) {
        vTaskDelay(pdMS_TO_TICKS(10));
    }

    bool buffered_complete_reply = false;
    if (!current->producer_done && current->first_audio_us > 0) {
        const int64_t buffered_us =
            esp_timer_get_time() - current->first_audio_us;
        const int64_t arrival_bytes_per_second =
            buffered_us > 0
                ? static_cast<int64_t>(
                      xStreamBufferBytesAvailable(current->playback_buffer)) *
                      1000000 / buffered_us
                : 0;
        if (arrival_bytes_per_second < kMinimumStreamingBytesPerSecond) {
            buffered_complete_reply = true;
            while (!current->producer_done) {
                vTaskDelay(pdMS_TO_TICKS(10));
            }
        }
        ESP_LOGI(kTag,
                 "playback buffering: arrival=%lld bytes/s complete=%d buffered=%u bytes",
                 arrival_bytes_per_second, buffered_complete_reply,
                 static_cast<unsigned>(xStreamBufferBytesAvailable(
                     current->playback_buffer)));
    }

    if (xStreamBufferBytesAvailable(current->playback_buffer) > 0) {
        current->error = current->audio->SetOutputEnabled(true);
        if (current->error == ESP_OK) {
            current->error =
                current->audio->SetOutputVolume(current->output_volume);
        }
        if (current->error == ESP_OK) {
            current->output_started = true;
            current->playback_started_us = esp_timer_get_time();
        }
    }

    while (current->error == ESP_OK && current->output_started) {
        const int64_t receive_started_us = esp_timer_get_time();
        const size_t received = xStreamBufferReceive(
            current->playback_buffer, chunk, sizeof(chunk),
            pdMS_TO_TICKS(100));
        if (received == 0) {
            if (current->producer_done &&
                xStreamBufferBytesAvailable(current->playback_buffer) == 0) {
                break;
            }
            ++current->audio_underruns;
            while (!current->producer_done &&
                   xStreamBufferBytesAvailable(current->playback_buffer) <
                       kPlaybackRebufferBytes) {
                vTaskDelay(pdMS_TO_TICKS(10));
            }
            current->audio_underrun_ms += ElapsedMs(
                receive_started_us, esp_timer_get_time());
            continue;
        }
        if (received % sizeof(int16_t) != 0) {
            current->error = ESP_ERR_INVALID_SIZE;
            break;
        }
        const int64_t write_started_us = esp_timer_get_time();
        current->error = current->audio->Write(
            reinterpret_cast<const int16_t*>(chunk),
            received / sizeof(int16_t));
        current->write_ms +=
            ElapsedMs(write_started_us, esp_timer_get_time());
    }
    if (current->output_started) {
        const esp_err_t close_result = current->audio->SetOutputEnabled(false);
        if (current->error == ESP_OK) {
            current->error = close_result;
        }
    }
    current->playback_finished_us = esp_timer_get_time();
    xTaskNotifyGive(current->playback_waiter);
    vTaskDelete(nullptr);
}

esp_err_t HandleHTTPEvent(esp_http_client_event_t* event) {
    auto* current = static_cast<ClientState*>(event->user_data);
    if (event->event_id != HTTP_EVENT_ON_DATA || current->error != ESP_OK) {
        return current->error;
    }
    if (current->mode == ResponseMode::kJSON) {
        if (current->response_size + event->data_len >= current->response_capacity) {
            current->error = ESP_ERR_NO_MEM;
            return current->error;
        }
        std::memcpy(current->response + current->response_size, event->data,
                    event->data_len);
        current->response_size += event->data_len;
        current->response[current->response_size] = '\0';
        return ESP_OK;
    }
    if (current->mode != ResponseMode::kSpeechStream) {
        return ESP_OK;
    }

    const auto* input = static_cast<const char*>(event->data);
    for (int index = 0; index < event->data_len; ++index) {
        if (input[index] == '\n') {
            current->error = ProcessSpeechLine(current);
            if (current->error != ESP_OK) {
                return current->error;
            }
            continue;
        }
        if (current->line_size + 1 >= current->line_capacity) {
            current->error = ESP_ERR_NO_MEM;
            return current->error;
        }
        current->line[current->line_size++] = input[index];
    }
    return ESP_OK;
}

esp_err_t EnsureClient() {
    if (client != nullptr) {
        return ESP_OK;
    }
    esp_http_client_config_t config = {};
    config.url = kBackendVoiceURL;
    config.timeout_ms = kVoiceTimeoutMs;
    config.event_handler = HandleHTTPEvent;
    config.user_data = &state;
    config.crt_bundle_attach = esp_crt_bundle_attach;
    config.keep_alive_enable = true;
    client = esp_http_client_init(&config);
    return client == nullptr ? ESP_ERR_NO_MEM : ESP_OK;
}

esp_err_t ConfigureRequest(const char* url, const char* content_type,
                           const char* access_token) {
    ESP_RETURN_ON_ERROR(EnsureClient(), kTag, "initialize voice client");
    ESP_RETURN_ON_ERROR(esp_http_client_set_url(client, url), kTag,
                        "set request URL");
    ESP_RETURN_ON_ERROR(esp_http_client_set_method(client, HTTP_METHOD_POST), kTag,
                        "set request method");
    ESP_RETURN_ON_ERROR(
        esp_http_client_set_header(client, "Content-Type", content_type), kTag,
        "set content type");
    ESP_RETURN_ON_ERROR(
        esp_http_client_set_header(client, "Accept-Encoding", "identity"), kTag,
        "disable response compression");
    ESP_RETURN_ON_ERROR(esp_http_client_set_header(
                            client, "X-Pupbox-Access-Token", access_token),
                        kTag, "set access token");
    ESP_RETURN_ON_ERROR(esp_http_client_set_header(
                            client, "X-Pupbox-Session-ID", session_id),
                        kTag, "set session ID");
    return ESP_OK;
}
}

void StartVoiceSession() {
    std::snprintf(session_id, sizeof(session_id), "esp32-s3-%08lx%08lx",
                  static_cast<unsigned long>(esp_random()),
                  static_cast<unsigned long>(esp_random()));
    ESP_LOGI(kTag, "started a fresh conversation session");
}

esp_err_t UploadVoiceRecording(const int16_t* samples, size_t sample_count,
                               const char* access_token,
                               VoiceReply* response) {
    if (samples == nullptr || sample_count == 0 || access_token == nullptr ||
        access_token[0] == '\0' || response == nullptr) {
        return ESP_ERR_INVALID_ARG;
    }
    const int64_t turn_started_us = esp_timer_get_time();
    constexpr char prefix_format[] =
        "--%s\r\nContent-Disposition: form-data; name=\"audio\"; "
        "filename=\"recording.wav\"\r\nContent-Type: audio/wav\r\n\r\n";
    constexpr char suffix_format[] = "\r\n--%s--\r\n";
    char prefix[256];
    char suffix[64];
    const int prefix_size = std::snprintf(prefix, sizeof(prefix), prefix_format,
                                          kBoundary);
    const int suffix_size = std::snprintf(suffix, sizeof(suffix), suffix_format,
                                          kBoundary);
    const size_t upload_sample_count = (sample_count / 3) * 2;
    const size_t audio_bytes = upload_sample_count * sizeof(int16_t);
    const size_t body_size = prefix_size + 44 + audio_bytes + suffix_size;
    auto* body = static_cast<uint8_t*>(heap_caps_malloc(
        body_size, MALLOC_CAP_SPIRAM | MALLOC_CAP_8BIT));
    auto* json_response = static_cast<char*>(heap_caps_malloc(
        kResponseCapacity, MALLOC_CAP_SPIRAM | MALLOC_CAP_8BIT));
    if (body == nullptr || json_response == nullptr) {
        heap_caps_free(body);
        heap_caps_free(json_response);
        return ESP_ERR_NO_MEM;
    }
    size_t offset = 0;
    std::memcpy(body + offset, prefix, prefix_size);
    offset += prefix_size;
    WriteWAVHeader(body + offset, audio_bytes, kVoiceUploadSampleRate);
    offset += 44;
    const size_t resampled_count = ResampleVoiceForUpload(
        samples, sample_count, body + offset);
    if (resampled_count != upload_sample_count) {
        heap_caps_free(body);
        heap_caps_free(json_response);
        return ESP_ERR_INVALID_SIZE;
    }
    offset += audio_bytes;
    std::memcpy(body + offset, suffix, suffix_size);

    char content_type[96];
    std::snprintf(content_type, sizeof(content_type),
                  "multipart/form-data; boundary=%s", kBoundary);
    esp_err_t result = ConfigureRequest(kBackendVoiceURL, content_type,
                                        access_token);
    if (result == ESP_OK) {
        state = {};
        state.mode = ResponseMode::kJSON;
        state.response = json_response;
        state.response_capacity = kResponseCapacity;
        state.request_started_us = esp_timer_get_time();
        result = esp_http_client_set_post_field(
            client, reinterpret_cast<const char*>(body),
            static_cast<int>(body_size));
        if (result == ESP_OK) {
            result = esp_http_client_perform(client);
        }
    }
    const int status =
        client == nullptr ? 0 : esp_http_client_get_status_code(client);
    const int64_t client_total_ms =
        ElapsedMs(state.request_started_us, esp_timer_get_time());
    heap_caps_free(body);
    if (result != ESP_OK || state.error != ESP_OK || status != HttpStatus_Ok) {
        ESP_LOGE(kTag, "voice upload failed: transport=%s state=%s HTTP=%d",
                 esp_err_to_name(result), esp_err_to_name(state.error), status);
        heap_caps_free(json_response);
        return result != ESP_OK ? result
                                : (state.error != ESP_OK ? state.error
                                                        : ESP_ERR_INVALID_RESPONSE);
    }

    cJSON* root = cJSON_ParseWithLength(json_response, state.response_size);
    if (root == nullptr) {
        heap_caps_free(json_response);
        return ESP_ERR_INVALID_RESPONSE;
    }
    *response = {};
    response->turn_started_us = turn_started_us;
    response->client_response_ms = client_total_ms;
    CopyJSONString(root, "trace_id", response->trace_id,
                   sizeof(response->trace_id));
    CopyJSONString(root, "reply", response->reply, sizeof(response->reply));
    CopyJSONString(root, "source", response->source, sizeof(response->source));
    cJSON* timings = cJSON_GetObjectItemCaseSensitive(root, "timings");
    if (cJSON_IsObject(timings)) {
        response->upload_ms = JSONInt64(timings, "upload_ms");
        response->stt_ms = JSONInt64(timings, "stt_ms");
        response->reply_ms = JSONInt64(timings, "reply_ms");
        response->server_total_ms = JSONInt64(timings, "total_ms");
    }
    cJSON_Delete(root);
    heap_caps_free(json_response);
    if (response->reply[0] == '\0') {
        return ESP_ERR_INVALID_RESPONSE;
    }
    ESP_LOGI(kTag,
             "voice reply ready: audio=%u bytes client=%lld ms server=%lld ms STT=%lld ms reply=%lld ms source=%s",
             static_cast<unsigned>(audio_bytes), client_total_ms,
             response->server_total_ms, response->stt_ms, response->reply_ms,
             response->source);
    return ESP_OK;
}

esp_err_t StreamVoiceReply(const VoiceReply& response,
                           const char* access_token, AudioBoard* audio,
                           int output_volume, PlaybackMetrics* metrics) {
    if (response.reply[0] == '\0' || access_token == nullptr ||
        access_token[0] == '\0' || audio == nullptr || metrics == nullptr) {
        return ESP_ERR_INVALID_ARG;
    }
    *metrics = {};
    cJSON* request = cJSON_CreateObject();
    if (request == nullptr ||
        !cJSON_AddStringToObject(request, "text", response.reply)) {
        cJSON_Delete(request);
        return ESP_ERR_NO_MEM;
    }
    char* request_json = cJSON_PrintUnformatted(request);
    cJSON_Delete(request);
    if (request_json == nullptr) {
        SetPlaybackError(metrics, ESP_ERR_NO_MEM);
        return ESP_ERR_NO_MEM;
    }
    auto* line = static_cast<char*>(heap_caps_malloc(
        kStreamLineCapacity, MALLOC_CAP_SPIRAM | MALLOC_CAP_8BIT));
    auto* decoded_audio = static_cast<uint8_t*>(heap_caps_malloc(
        kDecodedAudioCapacity, MALLOC_CAP_SPIRAM | MALLOC_CAP_8BIT));
    auto* playback_storage = static_cast<uint8_t*>(heap_caps_malloc(
        kPlaybackBufferCapacity, MALLOC_CAP_SPIRAM | MALLOC_CAP_8BIT));
    if (line == nullptr || decoded_audio == nullptr || playback_storage == nullptr) {
        cJSON_free(request_json);
        heap_caps_free(line);
        heap_caps_free(decoded_audio);
        heap_caps_free(playback_storage);
        SetPlaybackError(metrics, ESP_ERR_NO_MEM);
        return ESP_ERR_NO_MEM;
    }

    state = {};
    esp_err_t result = ConfigureRequest(kBackendSpeechStreamURL,
                                        "application/json", access_token);
    if (result == ESP_OK) {
        state.mode = ResponseMode::kSpeechStream;
        state.line = line;
        state.line_capacity = kStreamLineCapacity;
        state.decoded_audio = decoded_audio;
        state.decoded_audio_capacity = kDecodedAudioCapacity;
        state.audio = audio;
        state.output_volume = output_volume;
        state.request_started_us = esp_timer_get_time();
        state.playback_waiter = xTaskGetCurrentTaskHandle();
        state.playback_buffer = xStreamBufferCreateStatic(
            kPlaybackBufferCapacity, 1, playback_storage,
            &playback_buffer_control);
        if (state.playback_buffer == nullptr) {
            result = ESP_ERR_NO_MEM;
        } else if (xTaskCreate(PlaybackTask, "audio_playback", 8192, &state, 5,
                               nullptr) != pdPASS) {
            result = ESP_ERR_NO_MEM;
        } else {
            state.playback_task_started = true;
        }
    }
    if (result == ESP_OK) {
        result = esp_http_client_set_post_field(
            client, request_json, static_cast<int>(std::strlen(request_json)));
        if (result == ESP_OK) {
            result = esp_http_client_perform(client);
        }
        if (result == ESP_OK && state.error == ESP_OK && state.line_size > 0) {
            state.error = ProcessSpeechLine(&state);
        }
    }
    state.producer_finished_us = esp_timer_get_time();
    state.producer_done = true;
    if (state.playback_task_started) {
        if (ulTaskNotifyTake(pdTRUE, pdMS_TO_TICKS(120000)) == 0 &&
            state.error == ESP_OK) {
            state.error = ESP_ERR_TIMEOUT;
        }
    }
    const int status =
        client == nullptr ? 0 : esp_http_client_get_status_code(client);
    const int64_t finished_us = esp_timer_get_time();
    metrics->tts_first_audio_ms =
        state.first_audio_us == 0
            ? 0
            : ElapsedMs(state.request_started_us, state.first_audio_us);
    metrics->tts_ms =
        state.producer_finished_us == 0
            ? 0
            : ElapsedMs(state.request_started_us,
                        state.producer_finished_us);
    metrics->playback_ms =
        state.playback_started_us == 0 || state.playback_finished_us == 0
            ? 0
            : ElapsedMs(state.playback_started_us,
                        state.playback_finished_us);
    metrics->turn_total_ms =
        response.turn_started_us == 0
            ? 0
            : ElapsedMs(response.turn_started_us, finished_us);
    metrics->audio_underruns = state.audio_underruns;
    metrics->audio_underrun_ms = state.audio_underrun_ms;
    std::snprintf(metrics->tts_cache, sizeof(metrics->tts_cache), "%s",
                  state.tts_cache);
    cJSON_free(request_json);
    heap_caps_free(line);
    heap_caps_free(decoded_audio);
    if (state.playback_buffer != nullptr) {
        vStreamBufferDelete(state.playback_buffer);
    }
    heap_caps_free(playback_storage);
    if (result != ESP_OK || state.error != ESP_OK || status != HttpStatus_Ok ||
        !state.stream_done || state.audio_bytes == 0) {
        const esp_err_t playback_error =
            result != ESP_OK
                ? result
                : (state.error != ESP_OK ? state.error
                                         : ESP_ERR_INVALID_RESPONSE);
        SetPlaybackError(metrics, playback_error);
        ESP_LOGE(kTag,
                 "speech stream failed: transport=%s state=%s HTTP=%d done=%d audio=%u",
                 esp_err_to_name(result), esp_err_to_name(state.error), status,
                 state.stream_done, static_cast<unsigned>(state.audio_bytes));
        return playback_error;
    }
    ESP_LOGI(kTag,
             "speech playback finished: first_audio=%lld ms playback=%lld ms audio=%u bytes chunks=%u write=%lld ms max_gap=%lld ms underruns=%u total=%lld ms",
             ElapsedMs(state.request_started_us, state.first_audio_us),
             ElapsedMs(state.playback_started_us, state.playback_finished_us),
             static_cast<unsigned>(state.audio_bytes),
             static_cast<unsigned>(state.audio_chunks), state.write_ms,
             state.max_audio_gap_ms,
             static_cast<unsigned>(state.audio_underruns),
             ElapsedMs(state.request_started_us, finished_us));
    return ESP_OK;
}

esp_err_t ReportTurnMetrics(const VoiceReply& response,
                            const PlaybackMetrics& metrics,
                            const char* access_token) {
    if (response.trace_id[0] == '\0' || access_token == nullptr ||
        access_token[0] == '\0') {
        return ESP_ERR_INVALID_ARG;
    }
    cJSON* request = cJSON_CreateObject();
    if (request == nullptr) {
        return ESP_ERR_NO_MEM;
    }
    bool ok = cJSON_AddStringToObject(request, "trace_id", response.trace_id) &&
              cJSON_AddNumberToObject(request, "voice_response_ms",
                                     response.client_response_ms) &&
              cJSON_AddNumberToObject(request, "tts_first_audio_ms",
                                     metrics.tts_first_audio_ms) &&
              cJSON_AddNumberToObject(request, "tts_ms", metrics.tts_ms) &&
              cJSON_AddNumberToObject(request, "playback_ms",
                                     metrics.playback_ms) &&
              cJSON_AddNumberToObject(request, "turn_total_ms",
                                     metrics.turn_total_ms) &&
              cJSON_AddNumberToObject(request, "audio_underruns",
                                     metrics.audio_underruns) &&
              cJSON_AddNumberToObject(request, "audio_underrun_ms",
                                     metrics.audio_underrun_ms) &&
              cJSON_AddStringToObject(request, "tts_cache",
                                     metrics.tts_cache) &&
              cJSON_AddStringToObject(request, "playback_error",
                                     metrics.playback_error);
    char* request_json = ok ? cJSON_PrintUnformatted(request) : nullptr;
    cJSON_Delete(request);
    if (request_json == nullptr) {
        return ESP_ERR_NO_MEM;
    }

    char response_body[256] = {};
    state = {};
    esp_err_t result = ConfigureRequest(kBackendTurnMetricsURL,
                                        "application/json", access_token);
    if (result == ESP_OK) {
        state.mode = ResponseMode::kJSON;
        state.response = response_body;
        state.response_capacity = sizeof(response_body);
        result = esp_http_client_set_post_field(
            client, request_json, static_cast<int>(std::strlen(request_json)));
        if (result == ESP_OK) {
            result = esp_http_client_perform(client);
        }
    }
    const int status =
        client == nullptr ? 0 : esp_http_client_get_status_code(client);
    cJSON_free(request_json);
    if (result != ESP_OK || state.error != ESP_OK || status != HttpStatus_Ok) {
        ESP_LOGW(kTag,
                 "turn metrics failed: transport=%s state=%s HTTP=%d",
                 esp_err_to_name(result), esp_err_to_name(state.error), status);
        return result != ESP_OK ? result
                                : (state.error != ESP_OK ? state.error
                                                        : ESP_ERR_INVALID_RESPONSE);
    }
    ESP_LOGI(kTag,
             "turn metrics reported: response=%lld first_audio=%lld playback=%lld total=%lld underruns=%lld",
             response.client_response_ms, metrics.tts_first_audio_ms,
             metrics.playback_ms, metrics.turn_total_ms,
             metrics.audio_underruns);
    return ESP_OK;
}
