#include "backend_health.h"

#include <cstdint>
#include <cstring>

#include "esp_check.h"
#include "esp_crt_bundle.h"
#include "esp_http_client.h"
#include "esp_log.h"
#include "esp_netif_sntp.h"
#include "esp_timer.h"
#include "freertos/FreeRTOS.h"
#include "lwip/netdb.h"

namespace {
constexpr char kTag[] = "backend_health";
constexpr int kRequestTimeoutMs = 10000;

struct HttpTimings {
    int64_t started_us;
    int64_t connected_us;
    int64_t first_byte_us;
};

int64_t ElapsedMs(int64_t started_us, int64_t finished_us) {
    if (started_us == 0 || finished_us == 0) {
        return -1;
    }
    return (finished_us - started_us) / 1000;
}

esp_err_t HandleHttpEvent(esp_http_client_event_t* event) {
    auto* timings = static_cast<HttpTimings*>(event->user_data);
    if (event->event_id == HTTP_EVENT_ON_CONNECTED &&
        timings->connected_us == 0) {
        timings->connected_us = esp_timer_get_time();
    }
    if (event->event_id == HTTP_EVENT_ON_DATA &&
        timings->first_byte_us == 0) {
        timings->first_byte_us = esp_timer_get_time();
    }
    return ESP_OK;
}

esp_err_t ResolveHost(const char* host) {
    const int64_t started_us = esp_timer_get_time();
    addrinfo hints = {};
    hints.ai_family = AF_UNSPEC;
    hints.ai_socktype = SOCK_STREAM;
    addrinfo* result = nullptr;
    const int lookup_result = getaddrinfo(host, nullptr, &hints, &result);
    const int64_t elapsed_ms = ElapsedMs(started_us, esp_timer_get_time());
    if (lookup_result != 0 || result == nullptr) {
        ESP_LOGE(kTag, "DNS lookup failed after %lld ms: code=%d",
                 elapsed_ms, lookup_result);
        if (result != nullptr) {
            freeaddrinfo(result);
        }
        return ESP_FAIL;
    }
    freeaddrinfo(result);
    ESP_LOGI(kTag, "DNS lookup succeeded in %lld ms", elapsed_ms);
    return ESP_OK;
}
}

esp_err_t SyncClock() {
    ESP_LOGI(kTag, "synchronizing clock with SNTP");
    const int64_t started_us = esp_timer_get_time();
    esp_sntp_config_t config = ESP_NETIF_SNTP_DEFAULT_CONFIG_MULTIPLE(
        3, ESP_SNTP_SERVER_LIST("time.apple.com", "ntp.aliyun.com",
                                "pool.ntp.org"));
    esp_err_t result = esp_netif_sntp_init(&config);
    if (result == ESP_OK) {
        result = esp_netif_sntp_sync_wait(pdMS_TO_TICKS(15000));
    }
    esp_netif_sntp_deinit();
    if (result != ESP_OK) {
        ESP_LOGE(kTag, "SNTP sync failed after %lld ms: %s",
                 ElapsedMs(started_us, esp_timer_get_time()),
                 esp_err_to_name(result));
        return result;
    }
    ESP_LOGI(kTag, "clock synchronized in %lld ms",
             ElapsedMs(started_us, esp_timer_get_time()));
    return ESP_OK;
}

esp_err_t CheckBackendHealth(const char* host, const char* url,
                             const char* access_token) {
    ESP_RETURN_ON_ERROR(ResolveHost(host), kTag, "resolve backend host");

    HttpTimings timings = {
        .started_us = esp_timer_get_time(),
        .connected_us = 0,
        .first_byte_us = 0,
    };
    esp_http_client_config_t config = {};
    config.url = url;
    config.method = HTTP_METHOD_GET;
    config.timeout_ms = kRequestTimeoutMs;
    config.max_authorization_retries = -1;
    config.event_handler = HandleHttpEvent;
    config.user_data = &timings;
    config.crt_bundle_attach = esp_crt_bundle_attach;
    esp_http_client_handle_t client = esp_http_client_init(&config);
    if (client == nullptr) {
        ESP_LOGE(kTag, "failed to initialize HTTPS client");
        return ESP_ERR_NO_MEM;
    }
    if (access_token != nullptr && std::strlen(access_token) > 0) {
        esp_http_client_set_header(client, "X-Pupbox-Access-Token",
                                   access_token);
    }

    const esp_err_t result = esp_http_client_perform(client);
    const int64_t finished_us = esp_timer_get_time();
    const int status = esp_http_client_get_status_code(client);
    esp_http_client_cleanup(client);
    if (result != ESP_OK) {
        ESP_LOGE(kTag, "HTTPS request failed after %lld ms: %s",
                 ElapsedMs(timings.started_us, finished_us),
                 esp_err_to_name(result));
        return result;
    }

    ESP_LOGI(kTag,
             "HTTPS status=%d secure_connect=%lld ms first_byte=%lld ms total=%lld ms",
             status, ElapsedMs(timings.started_us, timings.connected_us),
             ElapsedMs(timings.started_us, timings.first_byte_us),
             ElapsedMs(timings.started_us, finished_us));
    if (status == HttpStatus_Ok) {
        ESP_LOGI(kTag, "backend health check passed");
        return ESP_OK;
    }
    if (status == HttpStatus_Unauthorized) {
        ESP_LOGW(kTag,
                 "HTTPS transport passed, but backend authorization is missing or invalid");
    } else {
        ESP_LOGW(kTag, "backend health check returned HTTP %d", status);
    }
    return ESP_ERR_INVALID_RESPONSE;
}
