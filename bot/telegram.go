package bot

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"go.uber.org/zap"

	"mon/command"
	"mon/config"
	"mon/monitor"
	"mon/proxy"
)

// TelegramBot implements Bot using Telegram Bot API long polling.
type TelegramBot struct {
	name         string
	botToken     string
	baseURL      string
	allowedChats map[string]bool
	client       *http.Client
	monitors     []*monitor.Monitor
	logger       *zap.SugaredLogger
}

func (t *TelegramBot) Type() string { return "telegram" }
func (t *TelegramBot) Name() string { return t.name }

func NewTelegramBot(name string, cfg *config.TelegramBotConfig, monitors []*monitor.Monitor, logger *zap.SugaredLogger) (*TelegramBot, error) {
	transport, err := proxy.HTTPTransport(cfg.ProxyURL)
	if err != nil {
		return nil, fmt.Errorf("proxy: %w", err)
	}
	client := &http.Client{
		Timeout: 35 * time.Second,
	}
	if transport != nil {
		client.Transport = transport
	}
	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = "https://api.telegram.org"
	}
	allowedChats := make(map[string]bool, len(cfg.AllowedChats))
	for _, id := range cfg.AllowedChats {
		allowedChats[id] = true
	}
	return &TelegramBot{
		name:         name,
		botToken:     cfg.BotToken,
		baseURL:      baseURL,
		allowedChats: allowedChats,
		client:       client,
		monitors:     monitors,
		logger:       logger,
	}, nil
}

func (t *TelegramBot) Start(ctx context.Context) error {
	t.logger.Infof("%s: Bot started", t.name)
	defer t.logger.Infof("%s: Bot stopped", t.name)
	var offset int64
	for {
		select {
		case <-ctx.Done():
			return nil
		default:
		}
		updates, err := t.getUpdates(ctx, offset)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			t.logger.Errorf("%s: getUpdates failed: %v", t.name, err)
			select {
			case <-ctx.Done():
				return nil
			case <-time.After(5 * time.Second):
			}
			continue
		}
		for _, u := range updates {
			if u.UpdateID >= offset {
				offset = u.UpdateID + 1
			}
			t.handleUpdate(ctx, u)
		}
	}
}

// Telegram API types (minimal).

type telegramUpdate struct {
	UpdateID int64            `json:"update_id"`
	Message  *telegramMessage `json:"message"`
}

type telegramMessage struct {
	Chat telegramChat `json:"chat"`
	Text string       `json:"text"`
}

type telegramChat struct {
	ID int64 `json:"id"`
}

type telegramGetUpdatesResponse struct {
	OK          bool             `json:"ok"`
	ErrorCode   int              `json:"error_code,omitempty"`
	Description string           `json:"description,omitempty"`
	Result      []telegramUpdate `json:"result"`
}

func (t *TelegramBot) getUpdates(ctx context.Context, offset int64) ([]telegramUpdate, error) {
	u := fmt.Sprintf("%s/bot%s/getUpdates?offset=%d&timeout=30&allowed_updates=%s",
		t.baseURL, t.botToken, offset, `["message"]`)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	resp, err := t.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, body)
	}
	var tgResp telegramGetUpdatesResponse
	if err := json.Unmarshal(body, &tgResp); err != nil {
		return nil, err
	}
	if !tgResp.OK {
		return nil, fmt.Errorf("telegram API error: (%d) %s", tgResp.ErrorCode, tgResp.Description)
	}
	return tgResp.Result, nil
}

func (t *TelegramBot) handleUpdate(ctx context.Context, u telegramUpdate) {
	if u.Message == nil {
		return
	}
	chatID := u.Message.Chat.ID
	if !t.allowedChats[strconv.FormatInt(chatID, 10)] {
		return
	}
	text := strings.TrimSpace(u.Message.Text)
	if text == "" {
		return
	}
	cmd, args := parseCommand(text)
	if cmd == "" {
		return
	}
	var reply string
	switch cmd {
	case "/help", "/start":
		reply = cmdHelp()
	case "/check":
		reply = t.cmdCheck(args)
	case "/notify":
		reply = t.cmdNotify(ctx, args)
	case "/pause":
		reply = t.cmdPause(args)
	case "/resume":
		reply = t.cmdResume(args)
	case "/status":
		reply = t.cmdStatus(args)
	case "/test":
		reply = t.cmdTest(ctx, args)
	default:
		reply = fmt.Sprintf("❌ error: unknown command %s; type /help for available commands", cmd)
	}
	if err := t.sendMessage(ctx, chatID, reply); err != nil {
		t.logger.Errorf("%s: sendMessage failed: %v", t.name, err)
	}
}

// parseCommand extracts the command name (stripping @botname suffix) and
// tokenizes the remaining arguments. Returns empty cmd for non-command text.
func parseCommand(text string) (cmd string, args []string) {
	if !strings.HasPrefix(text, "/") {
		return "", nil
	}
	fields := strings.Fields(text)
	cmd = fields[0]
	if i := strings.Index(cmd, "@"); i != -1 {
		cmd = cmd[:i]
	}
	return cmd, fields[1:]
}

// newFlagSet creates a flag.FlagSet that captures errors and usage into the
// returned buffer, suitable for returning as a bot reply.
func newFlagSet(name, usage string) (*flag.FlagSet, *bytes.Buffer) {
	var buf bytes.Buffer
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(&buf)
	fs.Usage = func() { fmt.Fprint(&buf, usage) }
	return fs, &buf
}

func cmdHelp() string {
	return `/help
/check [service]
/notify [service] [notifier] -s <subject> [-b <body>]
/pause [service]
/resume [service]
/status [service]
/test [service] [notifier]`
}

func (t *TelegramBot) cmdCheck(args []string) string {
	fs, buf := newFlagSet("check", "Usage: /check [service]")
	if err := fs.Parse(args); err != nil {
		return buf.String()
	}
	positional := fs.Args()
	serviceName := ""
	if len(positional) >= 1 {
		serviceName = positional[0]
	}
	results := command.Check(t.monitors, serviceName)
	if len(results) == 0 {
		return "❌ error: service not found"
	}
	var sb strings.Builder
	for _, r := range results {
		fmt.Fprintf(&sb, "%s: %s\n", r.ServiceName, r.Result)
	}
	return sb.String()
}

func (t *TelegramBot) cmdNotify(ctx context.Context, args []string) string {
	fs, buf := newFlagSet("notify", "Usage: /notify [service] [notifier] -s <subject> [-b <body>]")
	subject := fs.String("s", "", "")
	body := fs.String("b", "", "")
	if err := fs.Parse(args); err != nil {
		return buf.String()
	}
	positional := fs.Args()
	if *subject == "" {
		return "❌ error: missing subject"
	}
	serviceName := ""
	notifierName := ""
	if len(positional) >= 1 {
		serviceName = positional[0]
	}
	if len(positional) >= 2 {
		notifierName = positional[1]
	}
	results := command.Notify(t.monitors, serviceName, func(m *monitor.Monitor) []monitor.NotifyResult {
		return m.Notify(ctx, notifierName, *subject, *body)
	})
	var sb strings.Builder
	anyNotified := false
	for _, svc := range results {
		for _, r := range svc.Results {
			anyNotified = true
			if r.Success {
				fmt.Fprintf(&sb, "%s %s\n", svc.ServiceName, r.NotifierName)
			} else {
				fmt.Fprintf(&sb, "❌ error: %s: %s: %s\n", svc.ServiceName, r.NotifierName, r.Error)
			}
		}
	}
	if !anyNotified {
		return "❌ error: notifier not found"
	}
	return sb.String()
}

func (t *TelegramBot) cmdPause(args []string) string {
	return t.cmdPauseResume(args, "pause")
}

func (t *TelegramBot) cmdResume(args []string) string {
	return t.cmdPauseResume(args, "resume")
}

func (t *TelegramBot) cmdPauseResume(args []string, action string) string {
	fs, buf := newFlagSet(action, fmt.Sprintf("Usage: /%s [service]", action))
	if err := fs.Parse(args); err != nil {
		return buf.String()
	}
	positional := fs.Args()
	serviceName := ""
	if len(positional) >= 1 {
		serviceName = positional[0]
	}
	affected := command.PauseResume(t.monitors, serviceName, action)
	if len(affected) == 0 {
		return "❌ error: service not found"
	}
	var sb strings.Builder
	for _, svc := range affected {
		fmt.Fprintf(&sb, "%s\n", svc)
	}
	return sb.String()
}

func (t *TelegramBot) cmdStatus(args []string) string {
	fs, buf := newFlagSet("status", "Usage: /status [service]")
	if err := fs.Parse(args); err != nil {
		return buf.String()
	}
	positional := fs.Args()
	serviceName := ""
	if len(positional) >= 1 {
		serviceName = positional[0]
	}
	statuses := command.Status(t.monitors, serviceName)
	if len(statuses) == 0 {
		return "❌ error: service not found"
	}
	if serviceName != "" {
		return monitor.FormatStatus(&statuses[0], time.Now())
	}
	return monitor.FormatStatusTable(statuses, time.Now())
}

func (t *TelegramBot) cmdTest(ctx context.Context, args []string) string {
	fs, buf := newFlagSet("test", "Usage: /test [service] [notifier]")
	if err := fs.Parse(args); err != nil {
		return buf.String()
	}
	positional := fs.Args()
	serviceName := ""
	notifierName := ""
	if len(positional) >= 1 {
		serviceName = positional[0]
	}
	if len(positional) >= 2 {
		notifierName = positional[1]
	}
	results := command.Notify(t.monitors, serviceName, func(m *monitor.Monitor) []monitor.NotifyResult {
		return m.TestNotify(ctx, notifierName)
	})
	var sb strings.Builder
	anyTested := false
	for _, svc := range results {
		for _, r := range svc.Results {
			anyTested = true
			if r.Success {
				fmt.Fprintf(&sb, "%s %s\n", svc.ServiceName, r.NotifierName)
			} else {
				fmt.Fprintf(&sb, "❌ error: %s: %s: %s\n", svc.ServiceName, r.NotifierName, r.Error)
			}
		}
	}
	if !anyTested {
		return "❌ error: notifier not found"
	}
	return sb.String()
}

func (t *TelegramBot) sendMessage(ctx context.Context, chatID int64, text string) error {
	u := fmt.Sprintf("%s/bot%s/sendMessage", t.baseURL, t.botToken)
	payload := map[string]any{
		"chat_id": chatID,
		"text":    text,
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := t.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, body)
	}
	io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
	return nil
}
