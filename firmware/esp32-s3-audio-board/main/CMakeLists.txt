idf_component_register(
    SRCS
        "audio_board.cc"
        "main.cc"
        "wifi_station.cc"
    INCLUDE_DIRS "."
    REQUIRES esp_driver_gpio esp_driver_i2c esp_driver_i2s esp_event esp_netif esp_wifi nvs_flash
)
