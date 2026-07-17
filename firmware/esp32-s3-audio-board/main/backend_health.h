#pragma once

#include "esp_err.h"

esp_err_t SyncClock();
esp_err_t CheckBackendHealth(const char* host, const char* url,
                             const char* access_token);
