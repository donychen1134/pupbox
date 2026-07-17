#pragma once

#include <cstddef>
#include <cstdint>

#include "esp_err.h"

class AudioBoard {
public:
    esp_err_t Init();
    esp_err_t SetInputEnabled(bool enabled);
    esp_err_t SetOutputEnabled(bool enabled);
    esp_err_t SetOutputVolume(int volume);
    esp_err_t IsButtonPressed(uint32_t button_pin, bool* pressed);
    esp_err_t Read(int16_t* samples, size_t sample_count);
    esp_err_t Write(const int16_t* samples, size_t sample_count);

private:
    void* i2c_bus_ = nullptr;
    void* io_expander_ = nullptr;
    void* tx_handle_ = nullptr;
    void* rx_handle_ = nullptr;
    void* data_if_ = nullptr;
    void* input_ctrl_if_ = nullptr;
    void* input_codec_if_ = nullptr;
    void* input_dev_ = nullptr;
    void* output_ctrl_if_ = nullptr;
    void* output_gpio_if_ = nullptr;
    void* output_codec_if_ = nullptr;
    void* output_dev_ = nullptr;
    bool input_enabled_ = false;
    bool output_enabled_ = false;
};
