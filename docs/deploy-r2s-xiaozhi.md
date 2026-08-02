# R2S 与小智固件接入

这条路线复用小智固件已经成熟的配网、ESP-SR 音频前处理、Opus 传输和播放状态机，同时保留 Pupbox 的儿童人设、安全规则、活动路由、会话上下文和诊断日志。

```text
ESP32-S3-AUDIO-Board
  -> 16 kHz mono Opus / WebSocket
  -> R2S 上的 pupbox-server
  -> DashScope 实时 ASR
  -> Pupbox 活动路由或 Qwen
  -> CosyVoice 流式 Opus
  -> ESP32 扬声器
```

API Key 只保存在 R2S 的 `/etc/pupbox.env`，不能写入固件、Git 仓库或浏览器地址。固件只保存 R2S 地址，并从 OTA 接口获取 WebSocket 连接配置。当前原型使用一个长期访问令牌；进入外网或多设备阶段前要改成每设备令牌和定期轮换。

## 当前 OpenWrt 原型

OpenWrt 只作为读卡器到货前的临时运行环境。ARM64 静态 Go 二进制、静态页面和运行数据分别放在：

```text
/opt/pupbox/
/etc/pupbox.env
/var/lib/pupbox/
```

安装文件：

```bash
install -m 0755 pupbox-server /opt/pupbox/pupbox-server
install -m 0755 deploy/openwrt/run.sh /opt/pupbox/run.sh
install -m 0755 deploy/openwrt/pupbox.init /etc/init.d/pupbox
install -m 0600 deploy/openwrt/pupbox.env.example /etc/pupbox.env
```

编辑 `/etc/pupbox.env`，填写设备 MAC、R2S LAN 地址、`CHAT_ARCHIVE_QWEN_API_KEY`、随机生成的 `PUPBOX_ACCESS_TOKEN`，以及只保存在 R2S 上的儿童资料变量 `PUPBOX_CHILD_NAME`、`PUPBOX_CHILD_ALIASES`、`PUPBOX_CHILD_BIRTHDAY`、`PUPBOX_CHILD_KINDERGARTEN_START`，然后启动：

旧版 OpenWrt 没有可用 IPv6 默认路由时，建议设置 `PUPBOX_DASHSCOPE_FORCE_IPV4=true`，让 DashScope 的 HTTP 和 WebSocket 都明确走 IPv4；该选项只影响 Pupbox 进程，不修改系统网络。

如果旧内核或网卡驱动下出现 DashScope TLS/HTTP2 长时间无响应，设置 `GODEBUG=tlsmlkem=0` 和 `PUPBOX_DASHSCOPE_TCP_MAX_SEGMENT=1200`。这两个选项分别关闭 Go 1.25 默认的 ML-KEM 兼容扩展，并只为 DashScope socket 限制 TCP 分段；HTTP/2、证书校验和 TLS 加密仍然保留。

```bash
/etc/init.d/pupbox enable
/etc/init.d/pupbox restart
logread -e pupbox
```

不要把 `/etc/pupbox.env` 复制回电脑或提交到仓库。

## 小智固件改造

基于官方 `xiaozhi-esp32` 的固定版本构建，不把整个上游仓库复制进 Pupbox。定制项只有：

1. 为微雪板增加独立构建变体。
2. 将 `CONFIG_OTA_URL` 指向 R2S 的 `/xiaozhi/ota/`。
3. 启用 `CONFIG_FORCE_DEFAULT_OTA_URL=y`，避免配网 NVS 里的旧服务地址覆盖自建地址。
4. 启用产品模式的安全音量、空闲休眠和按键恢复。
5. 使用 MultiNet7 在板端识别“豆豆你好”和“小狗豆豆”。
6. 接受后端的语音待机指令，并通过 MCP 上报 ESP32-S3 芯片温度。

仓库中的补丁需要按文件名顺序应用：`0001-pupbox-product-mode.patch` 保存基础产品模式，`0002-pupbox-standby-temperature.patch` 增加语音待机和芯片温度采集。`firmware/xiaozhi/config.pupbox.example.json` 保存不含真实地址和密钥的板型模板。将模板复制到小智板型目录、改成 R2S 的固定 LAN 地址后再构建。

```bash
git apply /path/to/pupbox/firmware/xiaozhi/patches/0001-pupbox-product-mode.patch
git apply /path/to/pupbox/firmware/xiaozhi/patches/0002-pupbox-standby-temperature.patch
```

示例板型配置：

```json
{
  "manufacturer": "waveshare",
  "target": "esp32s3",
  "builds": [
    {
      "name": "esp32-s3-audio-board-pupbox",
      "sdkconfig_append": [
        "CONFIG_OTA_URL=\"http://192.168.1.2:8791/xiaozhi/ota/\"",
        "CONFIG_FORCE_DEFAULT_OTA_URL=y",
        "CONFIG_PUPBOX_PRODUCT_MODE=y",
        "CONFIG_USE_CUSTOM_WAKE_WORD=y",
        "CONFIG_CUSTOM_WAKE_WORD=\"dou dou ni hao\"",
        "CONFIG_CUSTOM_WAKE_WORD_DISPLAY=\"豆豆你好\"",
        "CONFIG_CUSTOM_WAKE_WORD_2=\"xiao gou dou dou\"",
        "CONFIG_CUSTOM_WAKE_WORD_DISPLAY_2=\"小狗豆豆\"",
        "CONFIG_CUSTOM_WAKE_WORD_THRESHOLD=20",
        "CONFIG_SR_MN_CN_MULTINET7_QUANT=y",
        "CONFIG_USE_WECHAT_MESSAGE_STYLE=y"
      ]
    }
  ]
}
```

`FORCE_DEFAULT_OTA_URL` 是 Pupbox 对上游源码增加的布尔配置。`Ota::GetCheckVersionUrl()` 在该选项启用时直接返回 `CONFIG_OTA_URL`；关闭时保持小智原有的配网覆盖行为。两个唤醒词使用不带声调、以空格分隔的拼音写入 MultiNet7；显示文本则作为唤醒后发给服务端的第一句话。

产品模式将扬声器音量硬限制为 75。无活动时先关闭屏幕和音频；短按 `BOOT` 可恢复。

微雪 ESP32-S3-AUDIO-Board 的 `BAT_ADC` 检测通路出厂默认未连接。未改硬件时 ADC 会悬空，软件会得到从 100% 缓慢跌到低电量的假读数，因此 Pupbox 默认不报告电量，也不执行低电量休眠。微雪原理图说明，只有焊接板上的 `BAT_ADC` 0Ω 跳线后才能启用检测，而且 GPIO1 摄像头输入会同时不可用。完成该硬件改造并实测校准后，才在板型配置中加入：

```text
CONFIG_PUPBOX_BATTERY_MONITOR=y
```

该模式使用电压趋势估算充电状态，不等同于独立充电管理芯片的硬件状态脚。正式装入毛绒外壳前，更稳妥的方案是增加带电量计和保护板的电源模块。

构建与烧录：

```bash
source ~/.espressif/frameworks/esp-idf-v6.0.2/export.sh
python3 scripts/release.py waveshare/esp32-s3-audio-board \
  -c config.pupbox.json \
  --name esp32-s3-audio-board-pupbox

idf.py -p /dev/cu.usbmodem2101 flash
```

自动复位失败时，让板子进入下载模式：

1. 按住 `BOOT`。
2. 短按并松开 `RES`。
3. 松开 `BOOT`。
4. 再次执行烧录命令。

烧录不需要执行 `erase-flash`，因此通常可以保留现有 Wi-Fi。每次刷定制包前都应保存同版本官方 `merged-binary.bin`；固件损坏或接错服务端时，可以用官方包从 `0x0` 重新烧录恢复。

定制小智固件运行时，短按一次板载 `BOOT` 键开始或结束对话；不要同时按 `RES`。`K2` 按住录音只适用于 Pupbox 早期自研固件，不适用于这条小智固件路线。

## Armbian 迁移

读卡器到货后，将 R2S 迁移到 Armbian Debian 12 Minimal。迁移目标是获得正常的磁盘扩容、systemd、标准日志轮转和后续容器能力，不是增加模型算力。STT、LLM 和 TTS 仍由 DashScope 执行。

迁移顺序：

1. 校验 Armbian 镜像 SHA-256，并用 Balena Etcher 或 Raspberry Pi Imager 写入 MicroSD。
2. R2S 的 LAN 口接家庭路由器 LAN 口，从路由器 DHCP 列表获取地址。
3. 首次 SSH 登录后创建普通管理用户、安装 CA 证书和时钟同步。
4. 安装 ARM64 Pupbox Release 到 `/opt/pupbox`。
5. 以 systemd 运行，配置日志轮转和 TTS 缓存上限。
6. 为 R2S 固定 DHCP 租约，再重新构建一次固件中的 OTA 地址。
7. 完成端到端对话验证后，才下线 OpenWrt 卡。

迁移会替换当前 OpenWrt，开始写卡前必须确认现有网络不依赖 R2S 的路由或 DHCP。Pupbox 原型当前只是家庭 LAN 内的一台服务端，不应成为家庭网络的默认网关。

Release 包含 `deploy/systemd/pupbox.service` 和不含密钥的环境变量模板。安装到 Armbian 后：

```bash
install -d -o pupbox -g pupbox /opt/pupbox /var/lib/pupbox /var/lib/pupbox/tts-cache
install -d -m 0750 /etc/pupbox
install -m 0644 deploy/systemd/pupbox.service /etc/systemd/system/pupbox.service
install -m 0640 deploy/systemd/pupbox.env.example /etc/pupbox/pupbox.env
chown root:pupbox /etc/pupbox/pupbox.env
```

在 R2S 本机填写真实设备 ID、局域网地址、DashScope Key 和随机访问令牌，然后启动：

```bash
systemctl daemon-reload
systemctl enable --now pupbox
systemctl status pupbox --no-pager
journalctl -u pupbox -n 100 --no-pager
```

产品模式分为两段：连续 30 秒没有有效儿童表达时回到唤醒词待机，但保持网络连接；连续 120 秒仍无有效互动时播放告别语，随后发送延迟休眠指令。环境音频包、幼儿无意义音节和疑似成人长语音不会刷新活跃时间。可分别调整：

```bash
PUPBOX_XIAOZHI_ACTIVE_SECONDS=30
PUPBOX_XIAOZHI_IDLE_SECONDS=120
```

固件在告别后保留 15 秒唤醒词窗口，再进入只能由产品按钮唤醒的深度睡眠。需要调整这段窗口时，在 `/etc/pupbox/pupbox.env` 中设置：

```bash
PUPBOX_XIAOZHI_SLEEP_GRACE_SECONDS=15
```

这个值只控制告别语之后的延迟，不改变 `PUPBOX_XIAOZHI_IDLE_SECONDS` 的会话空闲时间。网络断开不会触发深度睡眠，设备会继续自动重连。

定制固件还通过 `self.get_device_status` 返回 ESP32-S3 芯片内部温度。服务端每 30 秒采样，默认保存到：

```bash
PUPBOX_DEVICE_LOG_PATH=/var/lib/pupbox/device-telemetry.jsonl
PUPBOX_DEVICE_LOG_LIMIT=2880
```

家长诊断页显示当前、近期最低和最高芯片温度。该值不能代表盒内电池温度；装入毛绒外壳后的首轮测试仍需使用绝缘固定的外部接触探头测量电池表面和盒内温度，并且不要在毛绒内部无人看管充电。

## 验证

先检查 R2S：

```bash
curl -H "Authorization: Bearer $PUPBOX_ACCESS_TOKEN" \
  http://192.168.1.2:8791/api/health
```

再在串口和 R2S 日志中确认：

1. 固件连接了预期 Wi-Fi。
2. OTA 响应下发了 `/xiaozhi/v1/`。
3. WebSocket 完成 `hello`。
4. 一轮语音依次出现 STT、reply、TTS first audio 和 turn total timing。
5. 设备连续播放、没有爆音或断续。
6. 说“晚安”后先听到告别语，约 15 秒后灯光完全熄灭，按板载 BOOT 键能够重新开机。
7. 完成一轮对话后等待 30 秒，确认设备回到弱呼吸灯和唤醒词待机；再次说“豆豆你好”应恢复对话。
8. 打开家长诊断页，等待至少 60 秒，确认设备温度出现两个以上样本并持续更新。

真实儿童测试前，至少验证“停”“再见”、危险话题、网络中断重连，以及 2 分钟空闲告别和按键恢复。
