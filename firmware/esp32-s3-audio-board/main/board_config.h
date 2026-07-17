#pragma once

#include "driver/gpio.h"

constexpr int kAudioSampleRate = 24000;
constexpr int kInitialOutputVolume = 50;
constexpr int kOutputVolumeStep = 10;

constexpr char kBackendHost[] = "pupbox.983457.xyz";
constexpr char kBackendHealthURL[] =
    "https://pupbox.983457.xyz/api/health";

constexpr gpio_num_t kI2CSCLGPIO = GPIO_NUM_10;
constexpr gpio_num_t kI2CSDAGPIO = GPIO_NUM_11;

constexpr gpio_num_t kI2SMCLKGPIO = GPIO_NUM_12;
constexpr gpio_num_t kI2SBCLKGPIO = GPIO_NUM_13;
constexpr gpio_num_t kI2SWSGPIO = GPIO_NUM_14;
constexpr gpio_num_t kI2SInputGPIO = GPIO_NUM_15;
constexpr gpio_num_t kI2SOutputGPIO = GPIO_NUM_16;

constexpr uint32_t kAmplifierEnablePin = 1U << 8;
constexpr uint32_t kVolumeUpButtonPin = 1U << 9;
constexpr uint32_t kRecordButtonPin = 1U << 10;
constexpr uint32_t kVolumeDownButtonPin = 1U << 11;
constexpr uint32_t kUserButtonPins =
    kVolumeUpButtonPin | kRecordButtonPin | kVolumeDownButtonPin;
