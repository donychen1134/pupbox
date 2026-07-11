# Pupbox Hardware Roadmap

The first hardware milestone is a supervised, voice-only prototype. Motion, screens, and autonomous wake words are intentionally deferred until a child repeatedly chooses to use the voice experience.

## Findings

- FoloToy validated a Huohuotu conversion with an ESP32, the toy's analog microphone, server-side speech processing, MQTT control, audio upload, and HTTPS reply download. Their published target for short replies was 3-5 seconds.
- FoloToy provides Huohuotu firmware and a self-hosted server stack, but its MQTT/UDP/HTTPS device protocol is not directly compatible with Pupbox.
- XiaoZhi documents a common DIY stack: ESP32-S3 N16R8, INMP441 microphone, MAX98357A amplifier, and a 2-3 W speaker. This is inexpensive but requires wiring, power work, and firmware adaptation.
- Integrated ESP32-S3 audio boards reduce first-time hardware risk by combining the controller, microphone, codec/amplifier, and speaker connection.

Sources:

- [FoloToy Huohuotu engineering notes](https://folotoy.com/zh/documents/hello-folotoy/)
- [FoloToy Huohuotu firmware releases](https://github.com/FoloToy/folotoy-bin/releases/)
- [FoloToy self-hosted server](https://github.com/FoloToy/folotoy-server-self-hosting)
- [XiaoZhi ESP32-S3 hardware guide](https://xiaozhi.dev/en/docs/usage/hardware-guide/)
- [Waveshare ESP32-S3 audio board](https://www.waveshare.net/shop/ESP32-S3-AUDIO-Board-EN.htm)
- [FoloToy Magicbox](https://folotoy.com/products/magicbox/)

## Recommended Sequence

### H0: Finish The Phone Acceptance Test

Exit criteria:

- Ten-minute child sessions do not collapse because of latency, broken context, or repetitive replies.
- A parent can inspect each turn and understand failures without retrieving raw recordings.
- Safety replies and volume are acceptable under direct adult supervision.

### H1: Bench Prototype Without A Toy Shell

Use one integrated ESP32-S3 audio board, one physical talk button, its microphone, and a small speaker. Power it by USB from a certified power bank during supervised tests. Do not add a loose lithium cell yet.

The device firmware only needs to:

1. Connect to Wi-Fi and store a device token.
2. Record while the button is held.
3. Upload audio to a Pupbox device endpoint.
4. Download and play the returned Opus file.
5. Emit simple listening, waiting, speaking, and error tones.

This keeps STT, conversation state, safety policy, Qwen, TTS, and diagnostics in the existing Go service.

### H2: Put The Bench Prototype In A Plush Dog

Choose a plush shell with a removable electronics pouch. Keep the microphone opening and speaker grille unobstructed. Use a recessed or fabric-covered talk button that cannot detach.

The first shell prototype remains USB powered or uses a sealed commercial power bank. Test surface temperature, maximum volume, cable strain, drop resistance, and whether any part can become a choking hazard.

### H3: Hands-Free And Motion

Only after H2 is stable:

- Add local VAD and a wake phrase.
- Add echo handling so the toy does not hear its own speaker.
- Add one low-risk motion such as a slow tail movement.
- Design a protected battery, charging board, enclosure, and service access.

## Buy Versus Build

### Fastest Physical Benchmark

Buy a FoloToy Magicbox or a known-compatible modified Huohuotu. This quickly tests whether a physical object increases engagement, but it does not reuse Pupbox directly without a protocol adapter and may introduce a separate subscription or firmware dependency.

### Recommended Pupbox Prototype

Buy an integrated ESP32-S3 audio board and keep the existing Pupbox backend. This requires firmware work but preserves the child-specific conversation behavior, diagnostics, safety rules, provider choices, and deployment already built here.

### Not Recommended As The First Step

Do not start with a bare ESP32-S3, separate microphone, amplifier, battery charger, and custom plush wiring. FoloToy's own notes show that charging interference and microphone selection can consume substantial debugging time. An integrated audio board is a better first hardware learning step.
