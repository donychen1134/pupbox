#include "wifi_station.h"

#include <cstring>

#include "esp_event.h"
#include "esp_check.h"
#include "esp_log.h"
#include "esp_netif.h"
#include "esp_wifi.h"
#include "freertos/FreeRTOS.h"
#include "freertos/event_groups.h"
#include "nvs_flash.h"

namespace {
constexpr char kTag[] = "wifi_station";
constexpr EventBits_t kConnectedBit = BIT0;
constexpr TickType_t kInitialConnectTimeout = pdMS_TO_TICKS(20000);

EventGroupHandle_t connection_events;

void HandleWifiEvent(void*, esp_event_base_t event_base, int32_t event_id,
                     void* event_data) {
    if (event_base == WIFI_EVENT && event_id == WIFI_EVENT_STA_START) {
        esp_wifi_connect();
        return;
    }
    if (event_base == WIFI_EVENT && event_id == WIFI_EVENT_STA_DISCONNECTED) {
        const auto* event = static_cast<wifi_event_sta_disconnected_t*>(event_data);
        ESP_LOGW(kTag, "disconnected (reason=%d), retrying", event->reason);
        xEventGroupClearBits(connection_events, kConnectedBit);
        esp_wifi_connect();
        return;
    }
    if (event_base == IP_EVENT && event_id == IP_EVENT_STA_GOT_IP) {
        const auto* event = static_cast<ip_event_got_ip_t*>(event_data);
        ESP_LOGI(kTag, "connected, ip=" IPSTR, IP2STR(&event->ip_info.ip));
        xEventGroupSetBits(connection_events, kConnectedBit);
    }
}
}

esp_err_t ConnectWifi(const char* ssid, const char* password) {
    esp_err_t nvs_result = nvs_flash_init();
    if (nvs_result == ESP_ERR_NVS_NO_FREE_PAGES ||
        nvs_result == ESP_ERR_NVS_NEW_VERSION_FOUND) {
        ESP_ERROR_CHECK(nvs_flash_erase());
        nvs_result = nvs_flash_init();
    }
    ESP_RETURN_ON_ERROR(nvs_result, kTag, "initialize NVS");
    ESP_RETURN_ON_ERROR(esp_netif_init(), kTag, "initialize network stack");
    ESP_RETURN_ON_ERROR(
        esp_event_loop_create_default(), kTag, "create event loop");

    connection_events = xEventGroupCreate();
    if (connection_events == nullptr) {
        return ESP_ERR_NO_MEM;
    }
    if (esp_netif_create_default_wifi_sta() == nullptr) {
        return ESP_ERR_NO_MEM;
    }

    wifi_init_config_t init_config = WIFI_INIT_CONFIG_DEFAULT();
    ESP_RETURN_ON_ERROR(esp_wifi_init(&init_config), kTag, "initialize Wi-Fi");
    ESP_RETURN_ON_ERROR(
        esp_event_handler_register(WIFI_EVENT, ESP_EVENT_ANY_ID,
                                   &HandleWifiEvent, nullptr),
        kTag,
        "register Wi-Fi events");
    ESP_RETURN_ON_ERROR(
        esp_event_handler_register(IP_EVENT, IP_EVENT_STA_GOT_IP,
                                   &HandleWifiEvent, nullptr),
        kTag,
        "register IP events");

    wifi_config_t wifi_config = {};
    std::strncpy(reinterpret_cast<char*>(wifi_config.sta.ssid), ssid,
                 sizeof(wifi_config.sta.ssid) - 1);
    std::strncpy(reinterpret_cast<char*>(wifi_config.sta.password), password,
                 sizeof(wifi_config.sta.password) - 1);
    wifi_config.sta.scan_method = WIFI_ALL_CHANNEL_SCAN;
    wifi_config.sta.sort_method = WIFI_CONNECT_AP_BY_SIGNAL;

    ESP_RETURN_ON_ERROR(esp_wifi_set_mode(WIFI_MODE_STA), kTag, "set station mode");
    ESP_RETURN_ON_ERROR(
        esp_wifi_set_config(WIFI_IF_STA, &wifi_config), kTag, "set Wi-Fi config");
    ESP_LOGI(kTag, "connecting to configured 2.4 GHz network");
    ESP_RETURN_ON_ERROR(esp_wifi_start(), kTag, "start Wi-Fi");

    EventBits_t bits = xEventGroupWaitBits(
        connection_events, kConnectedBit, pdFALSE, pdFALSE, kInitialConnectTimeout);
    if ((bits & kConnectedBit) == 0) {
        ESP_LOGW(kTag, "initial connection timed out; retries continue in background");
        return ESP_ERR_TIMEOUT;
    }
    return ESP_OK;
}
