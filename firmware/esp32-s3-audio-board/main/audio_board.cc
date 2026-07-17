#include "audio_board.h"

#include "board_config.h"

#include "driver/i2c_master.h"
#include "driver/i2s_std.h"
#include "driver/i2s_tdm.h"
#include "esp_codec_dev.h"
#include "esp_codec_dev_defaults.h"
#include "esp_check.h"
#include "esp_io_expander_tca95xx_16bit.h"
#include "esp_log.h"

namespace {
constexpr char kTag[] = "audio_board";
}

esp_err_t AudioBoard::Init() {
    i2c_master_bus_config_t i2c_config = {
        .i2c_port = I2C_NUM_0,
        .sda_io_num = kI2CSDAGPIO,
        .scl_io_num = kI2CSCLGPIO,
        .clk_source = I2C_CLK_SRC_DEFAULT,
        .glitch_ignore_cnt = 7,
        .intr_priority = 0,
        .trans_queue_depth = 0,
        .flags = {
            .enable_internal_pullup = true,
            .allow_pd = false,
        },
    };
    auto* i2c_bus = reinterpret_cast<i2c_master_bus_handle_t*>(&i2c_bus_);
    ESP_RETURN_ON_ERROR(i2c_new_master_bus(&i2c_config, i2c_bus), kTag, "initialize I2C");

    auto* expander = reinterpret_cast<esp_io_expander_handle_t*>(&io_expander_);
    ESP_RETURN_ON_ERROR(
        esp_io_expander_new_i2c_tca95xx_16bit(
            *i2c_bus,
            ESP_IO_EXPANDER_I2C_TCA9555_ADDRESS_000,
            expander),
        kTag,
        "initialize TCA9555");
    ESP_RETURN_ON_ERROR(
        esp_io_expander_set_dir(*expander, kAmplifierEnablePin, IO_EXPANDER_OUTPUT),
        kTag,
        "configure amplifier enable");
    ESP_RETURN_ON_ERROR(
        esp_io_expander_set_level(*expander, kAmplifierEnablePin, 1),
        kTag,
        "enable amplifier");
    ESP_RETURN_ON_ERROR(
        esp_io_expander_set_dir(*expander, kUserButtonPins, IO_EXPANDER_INPUT),
        kTag,
        "configure user buttons");

    i2s_chan_config_t channel_config = I2S_CHANNEL_DEFAULT_CONFIG(I2S_NUM_0, I2S_ROLE_MASTER);
    channel_config.dma_desc_num = 6;
    channel_config.dma_frame_num = 240;
    channel_config.auto_clear_after_cb = true;
    auto* tx_handle = reinterpret_cast<i2s_chan_handle_t*>(&tx_handle_);
    auto* rx_handle = reinterpret_cast<i2s_chan_handle_t*>(&rx_handle_);
    ESP_RETURN_ON_ERROR(
        i2s_new_channel(&channel_config, tx_handle, rx_handle),
        kTag,
        "create I2S channels");

    i2s_std_config_t output_config = {
        .clk_cfg = I2S_STD_CLK_DEFAULT_CONFIG(kAudioSampleRate),
        .slot_cfg = I2S_STD_PHILIPS_SLOT_DEFAULT_CONFIG(
            I2S_DATA_BIT_WIDTH_16BIT,
            I2S_SLOT_MODE_STEREO),
        .gpio_cfg = {
            .mclk = kI2SMCLKGPIO,
            .bclk = kI2SBCLKGPIO,
            .ws = kI2SWSGPIO,
            .dout = kI2SOutputGPIO,
            .din = I2S_GPIO_UNUSED,
            .invert_flags = {},
        },
    };
    output_config.clk_cfg.mclk_multiple = I2S_MCLK_MULTIPLE_256;
    ESP_RETURN_ON_ERROR(
        i2s_channel_init_std_mode(*tx_handle, &output_config),
        kTag,
        "initialize output I2S");

    i2s_tdm_config_t input_config = {
        .clk_cfg = I2S_TDM_CLK_DEFAULT_CONFIG(kAudioSampleRate),
        .slot_cfg = I2S_TDM_PHILIPS_SLOT_DEFAULT_CONFIG(
            I2S_DATA_BIT_WIDTH_16BIT,
            I2S_SLOT_MODE_STEREO,
            static_cast<i2s_tdm_slot_mask_t>(
                I2S_TDM_SLOT0 | I2S_TDM_SLOT1 |
                I2S_TDM_SLOT2 | I2S_TDM_SLOT3)),
        .gpio_cfg = {
            .mclk = kI2SMCLKGPIO,
            .bclk = kI2SBCLKGPIO,
            .ws = kI2SWSGPIO,
            .dout = I2S_GPIO_UNUSED,
            .din = kI2SInputGPIO,
            .invert_flags = {},
        },
    };
    input_config.clk_cfg.mclk_multiple = I2S_MCLK_MULTIPLE_256;
    input_config.clk_cfg.bclk_div = 8;
    ESP_RETURN_ON_ERROR(
        i2s_channel_init_tdm_mode(*rx_handle, &input_config),
        kTag,
        "initialize input I2S");
    ESP_RETURN_ON_ERROR(i2s_channel_enable(*tx_handle), kTag, "enable output I2S");
    ESP_RETURN_ON_ERROR(i2s_channel_enable(*rx_handle), kTag, "enable input I2S");

    audio_codec_i2s_cfg_t data_config = {
        .port = I2S_NUM_0,
        .rx_handle = *rx_handle,
        .tx_handle = *tx_handle,
        .clk_src = 0,
    };
    data_if_ = const_cast<audio_codec_data_if_t*>(audio_codec_new_i2s_data(&data_config));
    if (data_if_ == nullptr) {
        return ESP_ERR_NO_MEM;
    }

    audio_codec_i2c_cfg_t output_control_config = {
        .port = I2C_NUM_0,
        .addr = ES8311_CODEC_DEFAULT_ADDR,
        .bus_handle = *i2c_bus,
    };
    output_ctrl_if_ = const_cast<audio_codec_ctrl_if_t*>(
        audio_codec_new_i2c_ctrl(&output_control_config));
    output_gpio_if_ = const_cast<audio_codec_gpio_if_t*>(audio_codec_new_gpio());
    es8311_codec_cfg_t output_codec_config = {};
    output_codec_config.ctrl_if =
        reinterpret_cast<audio_codec_ctrl_if_t*>(output_ctrl_if_);
    output_codec_config.gpio_if =
        reinterpret_cast<audio_codec_gpio_if_t*>(output_gpio_if_);
    output_codec_config.codec_mode = ESP_CODEC_DEV_WORK_MODE_DAC;
    output_codec_config.pa_pin = GPIO_NUM_NC;
    output_codec_config.use_mclk = true;
    output_codec_config.hw_gain.pa_voltage = 5.0;
    output_codec_config.hw_gain.codec_dac_voltage = 3.3;
    output_codec_if_ = const_cast<audio_codec_if_t*>(
        es8311_codec_new(&output_codec_config));

    esp_codec_dev_cfg_t output_device_config = {
        .dev_type = ESP_CODEC_DEV_TYPE_OUT,
        .codec_if = reinterpret_cast<audio_codec_if_t*>(output_codec_if_),
        .data_if = reinterpret_cast<audio_codec_data_if_t*>(data_if_),
    };
    output_dev_ = esp_codec_dev_new(&output_device_config);

    audio_codec_i2c_cfg_t input_control_config = {
        .port = I2C_NUM_0,
        .addr = ES7210_CODEC_DEFAULT_ADDR,
        .bus_handle = *i2c_bus,
    };
    input_ctrl_if_ = const_cast<audio_codec_ctrl_if_t*>(
        audio_codec_new_i2c_ctrl(&input_control_config));
    es7210_codec_cfg_t input_codec_config = {};
    input_codec_config.ctrl_if =
        reinterpret_cast<audio_codec_ctrl_if_t*>(input_ctrl_if_);
    input_codec_config.mic_selected =
        ES7210_SEL_MIC1 | ES7210_SEL_MIC2 | ES7210_SEL_MIC3 | ES7210_SEL_MIC4;
    input_codec_if_ = const_cast<audio_codec_if_t*>(
        es7210_codec_new(&input_codec_config));

    esp_codec_dev_cfg_t input_device_config = {
        .dev_type = ESP_CODEC_DEV_TYPE_IN,
        .codec_if = reinterpret_cast<audio_codec_if_t*>(input_codec_if_),
        .data_if = reinterpret_cast<audio_codec_data_if_t*>(data_if_),
    };
    input_dev_ = esp_codec_dev_new(&input_device_config);

    if (output_ctrl_if_ == nullptr || output_gpio_if_ == nullptr ||
        output_codec_if_ == nullptr || output_dev_ == nullptr ||
        input_ctrl_if_ == nullptr || input_codec_if_ == nullptr ||
        input_dev_ == nullptr) {
        return ESP_ERR_NO_MEM;
    }

    ESP_LOGI(kTag, "audio board initialized at %d Hz", kAudioSampleRate);
    return ESP_OK;
}

esp_err_t AudioBoard::SetInputEnabled(bool enabled) {
    if (enabled == input_enabled_) {
        return ESP_OK;
    }
    auto input_dev = reinterpret_cast<esp_codec_dev_handle_t>(input_dev_);
    if (!enabled) {
        ESP_RETURN_ON_ERROR(esp_codec_dev_close(input_dev), kTag, "close input");
        input_enabled_ = false;
        return ESP_OK;
    }

    esp_codec_dev_sample_info_t sample_info = {
        .bits_per_sample = 16,
        .channel = 4,
        .channel_mask = ESP_CODEC_DEV_MAKE_CHANNEL_MASK(0),
        .sample_rate = kAudioSampleRate,
        .mclk_multiple = 0,
    };
    ESP_RETURN_ON_ERROR(esp_codec_dev_open(input_dev, &sample_info), kTag, "open input");
    ESP_RETURN_ON_ERROR(
        esp_codec_dev_set_in_channel_gain(
            input_dev,
            ESP_CODEC_DEV_MAKE_CHANNEL_MASK(0),
            30.0),
        kTag,
        "set microphone gain");
    input_enabled_ = true;
    return ESP_OK;
}

esp_err_t AudioBoard::SetOutputEnabled(bool enabled) {
    if (enabled == output_enabled_) {
        return ESP_OK;
    }
    auto output_dev = reinterpret_cast<esp_codec_dev_handle_t>(output_dev_);
    if (!enabled) {
        ESP_RETURN_ON_ERROR(esp_codec_dev_close(output_dev), kTag, "close output");
        output_enabled_ = false;
        return ESP_OK;
    }

    esp_codec_dev_sample_info_t sample_info = {
        .bits_per_sample = 16,
        .channel = 1,
        .channel_mask = 0,
        .sample_rate = kAudioSampleRate,
        .mclk_multiple = 0,
    };
    ESP_RETURN_ON_ERROR(esp_codec_dev_open(output_dev, &sample_info), kTag, "open output");
    output_enabled_ = true;
    return ESP_OK;
}

esp_err_t AudioBoard::SetOutputVolume(int volume) {
    return esp_codec_dev_set_out_vol(
        reinterpret_cast<esp_codec_dev_handle_t>(output_dev_),
        volume);
}

esp_err_t AudioBoard::IsButtonPressed(uint32_t button_pin, bool* pressed) {
    if (pressed == nullptr || (button_pin & kUserButtonPins) == 0) {
        return ESP_ERR_INVALID_ARG;
    }
    uint32_t levels = 0;
    auto expander = reinterpret_cast<esp_io_expander_handle_t>(io_expander_);
    ESP_RETURN_ON_ERROR(
        esp_io_expander_get_level(expander, button_pin, &levels),
        kTag,
        "read user button");
    *pressed = (levels & button_pin) == 0;
    return ESP_OK;
}

esp_err_t AudioBoard::Read(int16_t* samples, size_t sample_count) {
    return esp_codec_dev_read(
        reinterpret_cast<esp_codec_dev_handle_t>(input_dev_),
        samples,
        sample_count * sizeof(int16_t));
}

esp_err_t AudioBoard::Write(const int16_t* samples, size_t sample_count) {
    return esp_codec_dev_write(
        reinterpret_cast<esp_codec_dev_handle_t>(output_dev_),
        const_cast<int16_t*>(samples),
        sample_count * sizeof(int16_t));
}
