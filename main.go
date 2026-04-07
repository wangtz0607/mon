package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"gopkg.in/natefinch/lumberjack.v2"

	"mon/bot"
	"mon/checker"
	"mon/command"
	"mon/config"
	"mon/cron"
	"mon/monitor"
	"mon/notifier"
)

type checkRequest struct {
	Service string `json:"service"`
}

type notifyRequest struct {
	Service  string `json:"service"`
	Notifier string `json:"notifier"`
	Subject  string `json:"subject"`
	Body     string `json:"body"`
}

type pauseResumeRequest struct {
	Service string `json:"service"`
}

type testRequest struct {
	Service  string `json:"service"`
	Notifier string `json:"notifier"`
}

func usage() {
	fmt.Fprintf(os.Stdout, `Usage:
  mon [options]
  mon [options] <command>

Options:
  -c <config>  Configuration file path (default: config.yaml)

Commands:
  check
  notify
  pause
  resume
  status
  test
`)
}

func main() {
	globalFlags := flag.NewFlagSet("mon", flag.ContinueOnError)
	globalFlags.SetOutput(os.Stdout)
	globalFlags.Usage = usage
	configPath := globalFlags.String("c", "config.yaml", "Configuration file path")

	if err := globalFlags.Parse(os.Args[1:]); err != nil {
		if err == flag.ErrHelp {
			os.Exit(0)
		}
		os.Exit(1)
	}

	args := globalFlags.Args()

	cfg, err := config.Load(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ error: failed to load config: %v\n", err)
		os.Exit(1)
	}

	if len(args) == 0 {
		runMonitor(cfg)
		return
	}

	switch args[0] {
	case "check":
		checkFlags := flag.NewFlagSet("check", flag.ExitOnError)
		checkFlags.SetOutput(os.Stdout)
		checkFlags.Usage = func() {
			fmt.Fprint(os.Stdout, `Usage:
  mon check [service]
`)
		}
		checkFlags.Parse(args[1:])
		positional := checkFlags.Args()
		serviceName := ""
		if len(positional) >= 1 {
			serviceName = positional[0]
		}
		runCheck(cfg, serviceName)
	case "notify":
		notifyFlags := flag.NewFlagSet("notify", flag.ExitOnError)
		notifyFlags.SetOutput(os.Stdout)
		notifyFlags.Usage = func() {
			fmt.Fprintf(os.Stdout, `Usage:
  mon notify [service] [notifier] -s <subject> [-b <body>]

Options:
  -b <body>     Body
  -s <subject>  Subject
`)
		}
		subject := notifyFlags.String("s", "", "Subject")
		body := notifyFlags.String("b", "", "Body")
		notifyFlags.Parse(args[1:])
		positional := notifyFlags.Args()
		if *subject == "" {
			fmt.Fprintf(os.Stderr, "❌ error: missing subject\n")
			notifyFlags.Usage()
			os.Exit(1)
		}
		serviceName := ""
		notifierName := ""
		if len(positional) >= 1 {
			serviceName = positional[0]
		}
		if len(positional) >= 2 {
			notifierName = positional[1]
		}
		runNotify(cfg, serviceName, notifierName, *subject, *body)
	case "pause":
		pauseFlags := flag.NewFlagSet("pause", flag.ExitOnError)
		pauseFlags.SetOutput(os.Stdout)
		pauseFlags.Usage = func() {
			fmt.Fprint(os.Stdout, `Usage:
  mon pause [service]
`)
		}
		pauseFlags.Parse(args[1:])
		positional := pauseFlags.Args()
		serviceName := ""
		if len(positional) >= 1 {
			serviceName = positional[0]
		}
		runPauseResume(cfg, serviceName, "pause")
	case "resume":
		resumeFlags := flag.NewFlagSet("resume", flag.ExitOnError)
		resumeFlags.SetOutput(os.Stdout)
		resumeFlags.Usage = func() {
			fmt.Fprint(os.Stdout, `Usage:
  mon resume [service]
`)
		}
		resumeFlags.Parse(args[1:])
		positional := resumeFlags.Args()
		serviceName := ""
		if len(positional) >= 1 {
			serviceName = positional[0]
		}
		runPauseResume(cfg, serviceName, "resume")
	case "status":
		statusFlags := flag.NewFlagSet("status", flag.ExitOnError)
		statusFlags.SetOutput(os.Stdout)
		statusFlags.Usage = func() {
			fmt.Fprint(os.Stdout, `Usage:
  mon status [service]
`)
		}
		statusFlags.Parse(args[1:])
		positional := statusFlags.Args()
		serviceName := ""
		if len(positional) >= 1 {
			serviceName = positional[0]
		}
		runStatus(cfg, serviceName)
	case "test":
		testFlags := flag.NewFlagSet("test", flag.ExitOnError)
		testFlags.SetOutput(os.Stdout)
		testFlags.Usage = func() {
			fmt.Fprint(os.Stdout, `Usage:
  mon test [service] [notifier]
`)
		}
		testFlags.Parse(args[1:])
		positional := testFlags.Args()
		serviceName := ""
		notifierName := ""
		if len(positional) >= 1 {
			serviceName = positional[0]
		}
		if len(positional) >= 2 {
			notifierName = positional[1]
		}
		runTest(cfg, serviceName, notifierName)
	default:
		fmt.Fprintf(os.Stderr, "❌ error: unknown command %s\n\n", args[0])
		usage()
		os.Exit(1)
	}
}

func buildChecker(svc config.ServiceConfig) (checker.Checker, error) {
	switch svc.Checker.Type {
	case "custom":
		return checker.NewCustomChecker(svc.Checker.Custom), nil
	case "http_get":
		c, err := checker.NewHTTPGetChecker(svc.Checker.HTTPGet)
		if err != nil {
			return nil, fmt.Errorf("http_get: %w", err)
		}
		return c, nil
	case "ping":
		return checker.NewPingChecker(svc.Checker.Ping), nil
	default:
		return nil, fmt.Errorf("unknown checker type %s", svc.Checker.Type)
	}
}

func buildNotifier(ncfg config.NotifierConfig, logger *zap.SugaredLogger) (notifier.Notifier, error) {
	switch ncfg.Type {
	case "custom":
		return notifier.NewCustomNotifier(ncfg.Name, ncfg.Custom, logger)
	case "discord":
		return notifier.NewDiscordNotifier(ncfg.Name, ncfg.Discord, logger)
	case "feishu":
		return notifier.NewFeishuNotifier(ncfg.Name, ncfg.Feishu, logger)
	case "smtp":
		return notifier.NewSMTPNotifier(ncfg.Name, ncfg.SMTP, logger)
	case "spug":
		return notifier.NewSpugNotifier(ncfg.Name, ncfg.Spug, logger)
	case "telegram":
		return notifier.NewTelegramNotifier(ncfg.Name, ncfg.Telegram, logger)
	default:
		return nil, fmt.Errorf("unknown notifier type %s", ncfg.Type)
	}
}

func buildBot(bcfg config.BotConfig, monitors []*monitor.Monitor, logger *zap.SugaredLogger) (bot.Bot, error) {
	switch bcfg.Type {
	case "telegram":
		return bot.NewTelegramBot(bcfg.Name, bcfg.Telegram, monitors, logger)
	default:
		return nil, fmt.Errorf("unknown bot type %s", bcfg.Type)
	}
}

func newLogger(w zapcore.WriteSyncer) *zap.SugaredLogger {
	enc := zap.NewProductionEncoderConfig()
	enc.EncodeTime = func(t time.Time, enc zapcore.PrimitiveArrayEncoder) {
		enc.AppendString(t.Format("2006-01-02T15:04:05.000Z07:00"))
	}
	enc.EncodeLevel = zapcore.CapitalLevelEncoder
	enc.ConsoleSeparator = " "
	core := zapcore.NewCore(zapcore.NewConsoleEncoder(enc), w, zapcore.DebugLevel)
	return zap.New(core).Sugar()
}

func openLogger(lcfg config.LogConfig) (*zap.SugaredLogger, io.Closer) {
	if lcfg.File == "" {
		return newLogger(os.Stderr), nil
	}
	lj := &lumberjack.Logger{
		Filename:   lcfg.File,
		MaxSize:    lcfg.MaxSize,
		MaxBackups: lcfg.MaxBackups,
		MaxAge:     lcfg.MaxAge,
		Compress:   lcfg.Compress,
	}
	return newLogger(zapcore.AddSync(lj)), lj
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

func runMonitor(cfg *config.Config) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	logger := newLogger(os.Stderr)

	var wg sync.WaitGroup
	var closers []io.Closer
	var monitors []*monitor.Monitor

	for _, svc := range cfg.Services {
		c, err := buildChecker(svc)
		if err != nil {
			logger.Errorf("%s: Failed to build checker: %v", svc.Name, err)
			os.Exit(1)
		}
		var notifiers []notifier.Notifier
		for _, ncfg := range svc.Notifiers {
			n, err := buildNotifier(ncfg, logger)
			if err != nil {
				logger.Errorf("%s: Failed to build notifier %s: %v", svc.Name, ncfg.Name, err)
				os.Exit(1)
			}
			notifiers = append(notifiers, n)
		}
		svcLogger, closer := openLogger(svc.Log)
		if closer != nil {
			closers = append(closers, closer)
		}

		m := &monitor.Monitor{
			Name:             svc.Name,
			Checker:          c,
			Notifiers:        notifiers,
			Interval:         svc.Interval,
			FailureThreshold: svc.FailureThreshold,
			SuccessThreshold: svc.SuccessThreshold,
			Logger:           svcLogger,
		}
		monitors = append(monitors, m)

		wg.Add(1)
		go func() {
			defer wg.Done()
			m.Run(ctx)
		}()
	}

	// Start periodic status notification scheduler
	scheduler := cron.New(cfg.Location())
	for svcIdx, svc := range cfg.Services {
		m := monitors[svcIdx]
		for notifierIdx, ncfg := range svc.Notifiers {
			if ncfg.StatusSchedule == "" {
				continue
			}
			sched, _ := cron.Parse(ncfg.StatusSchedule) // already validated
			n := m.Notifiers[notifierIdx]
			svcName := svc.Name
			nName := n.Name()
			scheduler.Add(sched, func() {
				s := m.Status()
				subject := fmt.Sprintf("📄 %s", svcName)
				body := monitor.FormatStatus(&s, time.Now())
				logger.Infof("%s: %s: Sending periodic status", svcName, nName)
				if err := n.Notify(ctx, subject, body); err != nil {
					logger.Errorf("%s: %s: Failed to send periodic status: %v", svcName, nName, err)
				}
			})
			logger.Infof("%s: %s: Scheduled periodic status %s", svc.Name, nName, ncfg.StatusSchedule)
		}
	}
	if scheduler.HasJobs() {
		wg.Add(1)
		go func() {
			defer wg.Done()
			scheduler.Start(ctx)
		}()
	}

	// Start bots if configured
	for _, bcfg := range cfg.Bots {
		b, err := buildBot(bcfg, monitors, logger)
		if err != nil {
			logger.Errorf("Failed to build bot %s: %v", bcfg.Name, err)
			os.Exit(1)
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := b.Start(ctx); err != nil {
				logger.Errorf("Failed to start bot %s: %v", bcfg.Name, err)
			}
		}()
	}

	// Start HTTP server if configured
	var httpServer *http.Server
	if cfg.Listen != "" {
		mux := http.NewServeMux()
		mux.HandleFunc("POST /check", func(w http.ResponseWriter, r *http.Request) {
			r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
			var req checkRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			results := command.Check(monitors, req.Service)
			writeJSON(w, map[string]any{"results": results})
		})
		mux.HandleFunc("POST /notify", func(w http.ResponseWriter, r *http.Request) {
			r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
			var req notifyRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			results := command.Notify(monitors, req.Service, func(m *monitor.Monitor) []monitor.NotifyResult {
				return m.Notify(r.Context(), req.Notifier, req.Subject, req.Body)
			})
			writeJSON(w, map[string]any{"results": results})
		})
		mux.HandleFunc("POST /pause", func(w http.ResponseWriter, r *http.Request) {
			r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
			var req pauseResumeRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			affected := command.PauseResume(monitors, req.Service, "pause")
			writeJSON(w, map[string]any{"services": affected})
		})
		mux.HandleFunc("POST /resume", func(w http.ResponseWriter, r *http.Request) {
			r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
			var req pauseResumeRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			affected := command.PauseResume(monitors, req.Service, "resume")
			writeJSON(w, map[string]any{"services": affected})
		})
		mux.HandleFunc("GET /status", func(w http.ResponseWriter, r *http.Request) {
			service := r.URL.Query().Get("service")
			statuses := command.Status(monitors, service)
			writeJSON(w, map[string]any{"services": statuses})
		})
		mux.HandleFunc("POST /test", func(w http.ResponseWriter, r *http.Request) {
			r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
			var req testRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			results := command.Notify(monitors, req.Service, func(m *monitor.Monitor) []monitor.NotifyResult {
				return m.TestNotify(r.Context(), req.Notifier)
			})
			writeJSON(w, map[string]any{"results": results})
		})
		httpServer = &http.Server{
			Addr:              cfg.Listen,
			Handler:           mux,
			ReadHeaderTimeout: 10 * time.Second,
			ReadTimeout:       30 * time.Second,
			WriteTimeout:      30 * time.Second,
			IdleTimeout:       60 * time.Second,
		}
		ln, err := net.Listen("tcp", cfg.Listen)
		if err != nil {
			logger.Errorf("Failed to listen on %s: %v", cfg.Listen, err)
			os.Exit(1)
		}
		go func() {
			logger.Infof("Starting HTTP server on %s", cfg.Listen)
			if err := httpServer.Serve(ln); err != http.ErrServerClosed {
				logger.Errorf("Failed to start HTTP server on %s: %v", cfg.Listen, err)
			}
		}()
	}

	sig := <-sigCh
	logger.Infof("Handling signal %s", sig)

	// Shut down HTTP server first
	if httpServer != nil {
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer shutdownCancel()
		logger.Infof("Shutting down HTTP server on %s", cfg.Listen)
		if err := httpServer.Shutdown(shutdownCtx); err != nil {
			logger.Errorf("Failed to shut down HTTP server on %s: %v", cfg.Listen, err)
		}
	}

	cancel()
	wg.Wait()

	for _, c := range closers {
		c.Close()
	}
}

func runCheck(cfg *config.Config, serviceName string) {
	if cfg.Listen == "" {
		fmt.Fprintf(os.Stderr, "❌ error: no listen address configured\n")
		os.Exit(1)
	}

	u := "http://" + cfg.Listen + "/check"

	reqBody := checkRequest{Service: serviceName}
	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ error: %v\n", err)
		os.Exit(1)
	}

	resp, err := http.Post(u, "application/json", bytes.NewReader(bodyBytes))
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ error: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		fmt.Fprintf(os.Stderr, "❌ error: server returned %d: %s\n", resp.StatusCode, body)
		os.Exit(1)
	}

	var result struct {
		Results []command.CheckResult `json:"results"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		fmt.Fprintf(os.Stderr, "❌ error: failed to decode response: %v\n", err)
		os.Exit(1)
	}

	if len(result.Results) == 0 {
		fmt.Fprintf(os.Stderr, "❌ error: service not found\n")
		os.Exit(1)
	}

	for _, r := range result.Results {
		fmt.Printf("%s: %s\n", r.ServiceName, r.Result)
	}

	// Exit with non-zero status if any check failed
	for _, r := range result.Results {
		if !r.Result.OK {
			os.Exit(1)
		}
	}
}

func runNotify(cfg *config.Config, serviceName, notifierName, subject, body string) {
	if cfg.Listen == "" {
		fmt.Fprintf(os.Stderr, "❌ error: no listen address configured\n")
		os.Exit(1)
	}

	u := "http://" + cfg.Listen + "/notify"

	reqBody := notifyRequest{
		Service:  serviceName,
		Notifier: notifierName,
		Subject:  subject,
		Body:     body,
	}
	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ error: %v\n", err)
		os.Exit(1)
	}

	resp, err := http.Post(u, "application/json", bytes.NewReader(bodyBytes))
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ error: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		fmt.Fprintf(os.Stderr, "❌ error: server returned %d: %s\n", resp.StatusCode, body)
		os.Exit(1)
	}

	var result struct {
		Results []command.NotifyResult `json:"results"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		fmt.Fprintf(os.Stderr, "❌ error: failed to decode response: %v\n", err)
		os.Exit(1)
	}

	anyFailure := false
	anyNotified := false
	for _, svc := range result.Results {
		for _, r := range svc.Results {
			anyNotified = true
			if r.Success {
				fmt.Printf("%s %s\n", svc.ServiceName, r.NotifierName)
			} else {
				fmt.Fprintf(os.Stderr, "❌ error: %s: %s: %s\n", svc.ServiceName, r.NotifierName, r.Error)
				anyFailure = true
			}
		}
	}
	if !anyNotified {
		fmt.Fprintf(os.Stderr, "❌ error: notifier not found\n")
		os.Exit(1)
	}

	if anyFailure {
		os.Exit(1)
	}
}

func runPauseResume(cfg *config.Config, serviceName, action string) {
	if cfg.Listen == "" {
		fmt.Fprintf(os.Stderr, "❌ error: no listen address configured\n")
		os.Exit(1)
	}

	u := "http://" + cfg.Listen + "/" + action

	reqBody := pauseResumeRequest{Service: serviceName}
	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ error: %v\n", err)
		os.Exit(1)
	}

	resp, err := http.Post(u, "application/json", bytes.NewReader(bodyBytes))
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ error: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		fmt.Fprintf(os.Stderr, "❌ error: server returned %d: %s\n", resp.StatusCode, body)
		os.Exit(1)
	}

	var result struct {
		Services []string `json:"services"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		fmt.Fprintf(os.Stderr, "❌ error: failed to decode response: %v\n", err)
		os.Exit(1)
	}

	if len(result.Services) == 0 {
		fmt.Fprintf(os.Stderr, "❌ error: service not found\n")
		os.Exit(1)
	}

	for _, svc := range result.Services {
		fmt.Println(svc)
	}
}

func runStatus(cfg *config.Config, serviceName string) {
	if cfg.Listen == "" {
		fmt.Fprintf(os.Stderr, "❌ error: no listen address configured\n")
		os.Exit(1)
	}

	u := "http://" + cfg.Listen + "/status"
	if serviceName != "" {
		u += "?service=" + url.QueryEscape(serviceName)
	}
	resp, err := http.Get(u)
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ error: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		fmt.Fprintf(os.Stderr, "❌ error: server returned %d: %s\n", resp.StatusCode, body)
		os.Exit(1)
	}

	var result struct {
		Services []monitor.Status `json:"services"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		fmt.Fprintf(os.Stderr, "❌ error: failed to decode response: %v\n", err)
		os.Exit(1)
	}

	if len(result.Services) == 0 {
		fmt.Fprintf(os.Stderr, "❌ error: service not found\n")
		os.Exit(1)
	}

	if serviceName != "" {
		fmt.Print(monitor.FormatStatus(&result.Services[0], time.Now()))
		return
	}

	fmt.Print(monitor.FormatStatusTable(result.Services, time.Now()))
}

func runTest(cfg *config.Config, serviceName, notifierName string) {
	if cfg.Listen == "" {
		fmt.Fprintf(os.Stderr, "❌ error: no listen address configured\n")
		os.Exit(1)
	}

	u := "http://" + cfg.Listen + "/test"

	reqBody := testRequest{
		Service:  serviceName,
		Notifier: notifierName,
	}
	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ error: %v\n", err)
		os.Exit(1)
	}

	resp, err := http.Post(u, "application/json", bytes.NewReader(bodyBytes))
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ error: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		fmt.Fprintf(os.Stderr, "❌ error: server returned %d: %s\n", resp.StatusCode, body)
		os.Exit(1)
	}

	var result struct {
		Results []command.NotifyResult `json:"results"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		fmt.Fprintf(os.Stderr, "❌ error: failed to decode response: %v\n", err)
		os.Exit(1)
	}

	anyFailure := false
	anyTested := false
	for _, svc := range result.Results {
		for _, r := range svc.Results {
			anyTested = true
			if r.Success {
				fmt.Printf("%s %s\n", svc.ServiceName, r.NotifierName)
			} else {
				fmt.Fprintf(os.Stderr, "❌ error: %s: %s: %s\n", svc.ServiceName, r.NotifierName, r.Error)
				anyFailure = true
			}
		}
	}
	if !anyTested {
		fmt.Fprintf(os.Stderr, "❌ error: notifier not found\n")
		os.Exit(1)
	}

	if anyFailure {
		os.Exit(1)
	}
}
