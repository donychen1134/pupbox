# Pupbox ESP32-S3 Audio Board

This ESP-IDF firmware targets the Waveshare ESP32-S3-AUDIO-Board.

The current bench prototype supports a complete cloud conversation:

1. Press `K1` to increase the playback volume by 10%.
2. Tap `K2` once to start a short conversation. After each listening cue,
   speak normally. Local VAD starts on speech and stops after about 1.1
   seconds of silence.
3. Hold `K2` for at least 450 ms to use push-to-talk instead; release it to
   finish recording.
4. The board uploads the recording, runs Pupbox STT/reply/TTS, and plays the
   reply through the speaker.
5. Press `K3` to decrease the playback volume by 10%.
6. After a successful reply, the board plays another cue and listens for a
   follow-up without another button press. The first turn waits eight seconds;
   follow-ups wait 30 seconds. Press `K2` while listening to end the session.
7. A session also ends after a farewell reply, a failed turn, or inactivity.
   Inactivity plays a spoken goodbye before exit.
   Every individual recording stops after at most eight seconds.

A bright cue means the microphone has started listening. A short descending
double tone means speech capture has ended and the board is processing the
reply. Nursery-rhyme activities start with a short toy melody. After inactivity,
the board says goodbye; if the network is unavailable, it
falls back to the longer, softer descending sleep cue.

Each new `K2` conversation creates a fresh backend session. Automatic follow-up
turns share that session for continuity, while a later conversation does not
inherit an old story or game after the board has said goodbye.

The microphone remains at 24 kHz for the board codec. Before upload, firmware
resamples speech to 16 kHz mono PCM to reduce request size by one third. TTS
PCM remains at 24 kHz for playback quality. The reply client buffers slow
first-time TTS completely so playback stays continuous, while cached or
faster-than-realtime audio can begin after a shorter prebuffer. Wi-Fi state is
checked before every turn so a hotspot that appears after startup can recover
without a reset. If Wi-Fi or the voice request fails, the board plays a short
low error cue instead of repeating the child's recording.

Automatic recording keeps roughly 300 ms before detected speech to avoid
clipping the first syllable and retains about 200 ms of trailing silence. Very
short sounds are ignored locally. Playback and microphone capture never run
at the same time, so the dog does not record its own reply. The initial
thresholds are intentionally conservative and should continue to be tuned
with real child speech and household noise.

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

For the temporary bench Wi-Fi test, copy `main/secrets.example.h` to
`main/secrets.h` and fill in a 2.4 GHz SSID and password. The local secrets
file is ignored by Git. Add `PUPBOX_ACCESS_TOKEN` from the VPS environment to
receive HTTP 200 from the protected backend health endpoint. Without it, HTTP
401 still verifies DNS, Internet access, CA-validated TLS, and HTTP transport.
ESP32-S3 does not support 5 GHz Wi-Fi.

After Wi-Fi connects, a background task synchronizes the clock with SNTP and
checks `https://pupbox-aws.983457.xyz/api/health`. Serial logs report DNS, secure
connection, first-byte, upload, STT, reply buffering, underrun, and total
timings without logging credentials, transcripts, replies, or response
bodies. TCP buffers are enlarged for the high-latency VPS connection, and
Wi-Fi power saving is disabled during this latency-focused prototype. Revisit
that power setting before battery testing.

The 16 MB flash uses two 4 MB OTA application slots plus a data partition.
This leaves room for the HTTPS and voice client while reserving a rollback
slot for future over-the-air firmware updates.

`idf.py build` only creates files on the Mac and never changes the board.

When someone can stay beside the connected board, flash and then monitor it:

```bash
idf.py -p "$PUPBOX_SERIAL_PORT" flash
idf.py -p "$PUPBOX_SERIAL_PORT" monitor
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
