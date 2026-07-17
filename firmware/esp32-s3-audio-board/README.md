# Pupbox ESP32-S3 Audio Board

This ESP-IDF firmware targets the Waveshare ESP32-S3-AUDIO-Board.

The first milestone is deliberately offline:

1. Press `K1` to increase the playback volume by 10%.
2. Hold `K2` to record from the onboard microphone.
3. Release `K2` to play the recording through the speaker.
4. Press `K3` to decrease the playback volume by 10%.
5. Recording stops automatically after eight seconds.

This verifies the ESP32-S3, PSRAM, ES7210 microphone codec, ES8311 speaker
codec, amplifier, and physical button before Wi-Fi or Pupbox APIs are added.

With the USB-C connector at the top, the five tiny switches run clockwise
along the upper-right edge: `RESET`, `BOOT`, `K3`, `K2`, and `K1`. They are
side-mounted switches, not full-size push buttons. The loopback firmware uses
the three user keys and leaves `BOOT` for recovery/download mode.

## Toolchain

Use ESP-IDF v5.4.1:

```bash
source "$HOME/.espressif/frameworks/esp-idf-v5.4.1/export.sh"
idf.py set-target esp32s3
idf.py build
```

`idf.py build` only creates files on the Mac and never changes the board.

When someone can stay beside the connected board, flash and then monitor it:

```bash
idf.py -p /dev/cu.usbmodem101 flash
idf.py -p /dev/cu.usbmodem101 monitor
```

If power or USB is interrupted during flashing, the ESP32-S3 ROM downloader
is still available. Hold `BOOT`, tap and release `RESET`, then release `BOOT`
and repeat the flash command. Holding `BOOT` while reconnecting USB is an
alternative way to enter download mode.

A full factory flash backup should be stored outside the repository before the
first flash. It can be restored with esptool by writing the 16 MB image back at
address `0x0`; never commit that image.

Do not commit `sdkconfig`, build output, Wi-Fi credentials, access tokens, or
flash backups.

## Attribution

Board pin assignments and the ES7210/ES8311 duplex initialization follow the
MIT-licensed Waveshare ESP32-S3-AUDIO-Board support in
[`78/xiaozhi-esp32`](https://github.com/78/xiaozhi-esp32/tree/main/main/boards/waveshare/esp32-s3-audio-board).
