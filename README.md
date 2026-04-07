# mon

Service monitoring tool that periodically checks health and sends alerts after consecutive failures.

## Build

```bash
go build -o mon .
```

## Usage

```bash
# Start daemon
mon -c config.yaml

# Query status for all services
mon -c config.yaml status

# Query status for a single service
mon -c config.yaml status service1

# Send test notification for all services
mon -c config.yaml test

# Send test notification for a single service
mon -c config.yaml test service1

# Send test notification for a single notifier of a single service
mon -c config.yaml test service1 telegram1
```

## Configuration

See `config.example.yaml` for a complete example.

```yaml
listen: 127.0.0.1:9876

services:
  - name: service1
    interval: 30s
    failure_threshold: 3
    success_threshold: 1
    checker:
      type: http_get
      http_get:
        url: https://api.example.com/health
        timeout: 10s
    notifiers:
      - name: telegram1
        type: telegram
        telegram:
          bot_token: 'YOUR_BOT_TOKEN'
          chat_id: 'YOUR_CHAT_ID'
          timeout: 10s
```

## Supported Checkers

- `custom`
- `http_get`
- `ping`

## Supported Notifiers

- `custom`
- `discord`
- `feishu`
- `smtp`
- `spug`
- `telegram`

### Discord Notifier Setup

1. Open your Discord server, go to **Server Settings > Integrations > Webhooks > New Webhook**
2. Select the target channel and copy the **Webhook URL** (format: `https://discord.com/api/webhooks/...`)

### Feishu Notifier Setup

1. Open a Feishu group chat, go to **Settings (设置) > Bots (群机器人) > Add Bot (添加机器人) > Custom Bot (自定义机器人)**
2. Save the **Webhook URL (webhook 地址)** (format: `https://open.feishu.cn/open-apis/bot/v2/hook/xxx`)
3. Optionally enable **Sign Verification (签名校验)** and save the **Secret (密钥)**

### Spug Notifier Setup

1. Register at [push.spug.cc](https://push.spug.cc) and log in
2. Go to **Personal Center (个人中心) > Basic Settings (基本设置)** and copy your **User ID (用户 ID)**
3. Configure push channels in **Channel Settings (通道设置)**

`channel` is optional — if omitted, the account's default channel is used. Multiple channels can be specified with `|` as separator (e.g. `voice|sms|mail`). Supported values: `voice`, `sms`, `mail`, `wx_mp`, `wx`, `dd`, `fs`.

### Telegram Notifier Setup

**1. Create a Bot**

1. Open Telegram and search for [@BotFather](https://t.me/BotFather)
2. Send `/newbot` and follow the prompts
3. Save the **Bot Token** (format: `123456:ABC-DEF...`)

**2. Get Your Chat ID**

**Private chat:**

1. Send any message to your bot
2. Open `https://api.telegram.org/bot<YOUR_BOT_TOKEN>/getUpdates`
3. Find the `"chat": {"id": ...}` value in the response

**Group chat:**

1. Add your bot to the group and send a message
2. Same as above — group Chat IDs are negative numbers (e.g. `-1001234567890`)

## Supported Bots

- `telegram`

### Telegram Bot Setup

Use the same bot created in [Telegram Notifier Setup](#telegram-notifier-setup).

## License

mon is licensed under the [MIT license](https://opensource.org/licenses/MIT).
