#pragma once

#include "esp_err.h"

esp_err_t ConnectWifi(const char* ssid, const char* password);
bool WifiConnected();
