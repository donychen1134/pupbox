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

编辑 `/etc/pupbox.env`，填写设备 MAC、R2S LAN 地址、`CHAT_ARCHIVE_QWEN_API_KEY` 和随机生成的 `PUPBOX_ACCESS_TOKEN`，然后启动：

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

仓库中的 `firmware/xiaozhi/patches/0001-force-default-ota-url.patch` 保存上游源码改动，`firmware/xiaozhi/config.pupbox.example.json` 保存不含真实地址和密钥的板型模板。将模板复制到小智板型目录、改成 R2S 的固定 LAN 地址后再构建。

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
        "CONFIG_USE_WECHAT_MESSAGE_STYLE=y"
      ]
    }
  ]
}
```

`FORCE_DEFAULT_OTA_URL` 是 Pupbox 对上游源码增加的布尔配置。`Ota::GetCheckVersionUrl()` 在该选项启用时直接返回 `CONFIG_OTA_URL`；关闭时保持小智原有的配网覆盖行为。

构建与烧录：

```bash
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

真实儿童测试前，至少验证“停”“再见”、危险话题、网络中断重连和 10 分钟空闲恢复。
