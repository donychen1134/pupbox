#pragma once

#include <cstddef>
#include <cstdint>

#include "esp_err.h"

class AudioBoard;

struct VoiceReply {
    char trace_id[128];
    char reply[1024];
    char source[64];
    int64_t upload_ms;
    int64_t stt_ms;
    int64_t reply_ms;
    int64_t server_total_ms;
    int64_t client_response_ms;
    int64_t turn_started_us;
};

struct PlaybackMetrics {
    int64_t tts_first_audio_ms;
    int64_t tts_ms;
    int64_t playback_ms;
    int64_t turn_total_ms;
    int64_t audio_underruns;
    int64_t audio_underrun_ms;
    char tts_cache[16];
    char playback_error[32];
};

void StartVoiceSession();
esp_err_t UploadVoiceRecording(const int16_t* samples, size_t sample_count,
                               const char* access_token,
                               VoiceReply* response);
esp_err_t StreamVoiceReply(const VoiceReply& response,
                           const char* access_token, AudioBoard* audio,
                           int output_volume, PlaybackMetrics* metrics);
esp_err_t ReportTurnMetrics(const VoiceReply& response,
                            const PlaybackMetrics& metrics,
                            const char* access_token);
