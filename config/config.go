package config

import (
	"fmt"
	"net/url"
	"os"
	"time"

	"gopkg.in/yaml.v3"

	"mon/cron"
	"mon/duration"
)

type CustomCheckerConfig struct {
	Command string            `yaml:"command"`
	Shell   string            `yaml:"shell,omitempty"`
	Timeout duration.Duration `yaml:"timeout"`
}

type HTTPGetCheckerConfig struct {
	URL             string            `yaml:"url"`
	Timeout         duration.Duration `yaml:"timeout"`
	FollowRedirects bool              `yaml:"follow_redirects,omitempty"`
	ProxyURL        string            `yaml:"proxy_url,omitempty"`
}

type PingCheckerConfig struct {
	Host    string            `yaml:"host"`
	Timeout duration.Duration `yaml:"timeout"`
}

type CheckerConfig struct {
	Type    string                `yaml:"type"`
	Custom  *CustomCheckerConfig  `yaml:"custom,omitempty"`
	HTTPGet *HTTPGetCheckerConfig `yaml:"http_get,omitempty"`
	Ping    *PingCheckerConfig    `yaml:"ping,omitempty"`
}

type CustomNotifierConfig struct {
	Command           string             `yaml:"command"`
	Shell             string             `yaml:"shell,omitempty"`
	Timeout           duration.Duration  `yaml:"timeout"`
	MaxAttempts       int                `yaml:"max_attempts,omitempty"`
	RetryInitialDelay *duration.Duration `yaml:"retry_initial_delay,omitempty"`
	RetryMaxDelay     *duration.Duration `yaml:"retry_max_delay,omitempty"`
}

type DiscordNotifierConfig struct {
	WebhookURL        string             `yaml:"webhook_url"`
	Timeout           duration.Duration  `yaml:"timeout"`
	MaxAttempts       int                `yaml:"max_attempts,omitempty"`
	RetryInitialDelay *duration.Duration `yaml:"retry_initial_delay,omitempty"`
	RetryMaxDelay     *duration.Duration `yaml:"retry_max_delay,omitempty"`
	ProxyURL          string             `yaml:"proxy_url,omitempty"`
}

type FeishuNotifierConfig struct {
	WebhookURL        string             `yaml:"webhook_url"`
	Secret            string             `yaml:"secret,omitempty"`
	Timeout           duration.Duration  `yaml:"timeout"`
	MaxAttempts       int                `yaml:"max_attempts,omitempty"`
	RetryInitialDelay *duration.Duration `yaml:"retry_initial_delay,omitempty"`
	RetryMaxDelay     *duration.Duration `yaml:"retry_max_delay,omitempty"`
	ProxyURL          string             `yaml:"proxy_url,omitempty"`
}

type SMTPNotifierConfig struct {
	Host              string             `yaml:"host"`
	Port              int                `yaml:"port"`
	Username          string             `yaml:"username,omitempty"`
	Password          string             `yaml:"password,omitempty"`
	From              string             `yaml:"from"`
	To                []string           `yaml:"to"`
	TLSMode           string             `yaml:"tls_mode,omitempty"` // "none" | "starttls" | "tls" (default "none")
	TLSSkipVerify     bool               `yaml:"tls_skip_verify,omitempty"`
	Timeout           duration.Duration  `yaml:"timeout"`
	MaxAttempts       int                `yaml:"max_attempts,omitempty"`
	RetryInitialDelay *duration.Duration `yaml:"retry_initial_delay,omitempty"`
	RetryMaxDelay     *duration.Duration `yaml:"retry_max_delay,omitempty"`
	ProxyURL          string             `yaml:"proxy_url,omitempty"`
}

type SpugNotifierConfig struct {
	UserID            string             `yaml:"user_id"`
	Channel           string             `yaml:"channel,omitempty"`
	Timeout           duration.Duration  `yaml:"timeout"`
	MaxAttempts       int                `yaml:"max_attempts,omitempty"`
	RetryInitialDelay *duration.Duration `yaml:"retry_initial_delay,omitempty"`
	RetryMaxDelay     *duration.Duration `yaml:"retry_max_delay,omitempty"`
	ProxyURL          string             `yaml:"proxy_url,omitempty"`
}

type TelegramNotifierConfig struct {
	BotToken          string             `yaml:"bot_token"`
	ChatID            string             `yaml:"chat_id"`
	Timeout           duration.Duration  `yaml:"timeout"`
	MaxAttempts       int                `yaml:"max_attempts,omitempty"`
	RetryInitialDelay *duration.Duration `yaml:"retry_initial_delay,omitempty"`
	RetryMaxDelay     *duration.Duration `yaml:"retry_max_delay,omitempty"`
	BaseURL           string             `yaml:"base_url,omitempty"`
	ProxyURL          string             `yaml:"proxy_url,omitempty"`
}

type NotifierConfig struct {
	Name           string                  `yaml:"name"`
	Type           string                  `yaml:"type"`
	StatusSchedule string                  `yaml:"status_schedule,omitempty"`
	Custom         *CustomNotifierConfig   `yaml:"custom,omitempty"`
	Discord        *DiscordNotifierConfig  `yaml:"discord,omitempty"`
	Feishu         *FeishuNotifierConfig   `yaml:"feishu,omitempty"`
	SMTP           *SMTPNotifierConfig     `yaml:"smtp,omitempty"`
	Spug           *SpugNotifierConfig     `yaml:"spug,omitempty"`
	Telegram       *TelegramNotifierConfig `yaml:"telegram,omitempty"`
}

type LogConfig struct {
	File       string `yaml:"file"`
	MaxSize    int    `yaml:"max_size,omitempty"`
	MaxBackups int    `yaml:"max_backups,omitempty"`
	MaxAge     int    `yaml:"max_age,omitempty"`
	Compress   bool   `yaml:"compress,omitempty"`
}

type ServiceConfig struct {
	Name             string            `yaml:"name"`
	Interval         duration.Duration `yaml:"interval"`
	FailureThreshold int               `yaml:"failure_threshold,omitempty"`
	SuccessThreshold int               `yaml:"success_threshold,omitempty"`
	Log              LogConfig         `yaml:"log,omitempty"`
	Checker          CheckerConfig     `yaml:"checker"`
	Notifiers        []NotifierConfig  `yaml:"notifiers,omitempty"`
}

type TelegramBotConfig struct {
	BotToken     string   `yaml:"bot_token"`
	AllowedChats []string `yaml:"allowed_chats"`
	BaseURL      string   `yaml:"base_url,omitempty"`
	ProxyURL     string   `yaml:"proxy_url,omitempty"`
}

type BotConfig struct {
	Name     string             `yaml:"name"`
	Type     string             `yaml:"type"`
	Telegram *TelegramBotConfig `yaml:"telegram,omitempty"`
}

type Config struct {
	Listen   string          `yaml:"listen,omitempty"`
	Timezone string          `yaml:"timezone,omitempty"`
	Services []ServiceConfig `yaml:"services"`
	Bots     []BotConfig     `yaml:"bots,omitempty"`
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func validateProxyURL(rawURL string, allowedSchemes []string) error {
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid proxy URL %s: %w", rawURL, err)
	}
	for _, s := range allowedSchemes {
		if u.Scheme == s {
			if u.Host == "" {
				return fmt.Errorf("proxy URL %s: host is required", rawURL)
			}
			return nil
		}
	}
	return fmt.Errorf("proxy URL %s: scheme must be one of %v, got %s", rawURL, allowedSchemes, u.Scheme)
}

// Location returns the *time.Location for the configured Timezone.
// Returns time.Local if Timezone is empty.
func (c *Config) Location() *time.Location {
	if c.Timezone == "" {
		return time.Local
	}
	loc, err := time.LoadLocation(c.Timezone)
	if err != nil {
		return time.Local
	}
	return loc
}

func (c *Config) Validate() error {
	if c.Timezone != "" {
		if _, err := time.LoadLocation(c.Timezone); err != nil {
			return fmt.Errorf("invalid timezone %s: %w", c.Timezone, err)
		}
	}
	if len(c.Services) == 0 {
		return fmt.Errorf("no services defined")
	}
	serviceNames := make(map[string]bool)
	for i, svc := range c.Services {
		if svc.Name == "" {
			return fmt.Errorf("service[%d]: name is required", i)
		}
		if serviceNames[svc.Name] {
			return fmt.Errorf("service[%d]: duplicate name %s", i, svc.Name)
		}
		serviceNames[svc.Name] = true

		if svc.Interval <= 0 {
			return fmt.Errorf("service %s: interval must be positive", svc.Name)
		}
		if svc.FailureThreshold == 0 {
			c.Services[i].FailureThreshold = 3
		} else if svc.FailureThreshold < 0 {
			return fmt.Errorf("service %s: failure_threshold must be positive", svc.Name)
		}
		if svc.SuccessThreshold == 0 {
			c.Services[i].SuccessThreshold = 1
		} else if svc.SuccessThreshold < 0 {
			return fmt.Errorf("service %s: success_threshold must be positive", svc.Name)
		}

		if svc.Log.File != "" {
			if svc.Log.MaxSize == 0 {
				c.Services[i].Log.MaxSize = 100
			} else if svc.Log.MaxSize < 0 {
				return fmt.Errorf("service %s: log.max_size must be positive", svc.Name)
			}
			if svc.Log.MaxBackups == 0 {
				c.Services[i].Log.MaxBackups = 5
			} else if svc.Log.MaxBackups < 0 {
				return fmt.Errorf("service %s: log.max_backups must be positive", svc.Name)
			}
			if svc.Log.MaxAge == 0 {
				c.Services[i].Log.MaxAge = 30
			} else if svc.Log.MaxAge < 0 {
				return fmt.Errorf("service %s: log.max_age must be positive", svc.Name)
			}
		}

		// Validate checker
		switch svc.Checker.Type {
		case "custom":
			if svc.Checker.Custom == nil {
				return fmt.Errorf("service %s: custom config is required for type %s", svc.Name, svc.Checker.Type)
			}
			if svc.Checker.Custom.Command == "" {
				return fmt.Errorf("service %s: custom.command is required", svc.Name)
			}
			if svc.Checker.Custom.Shell == "" {
				c.Services[i].Checker.Custom.Shell = "bash"
			}
			if svc.Checker.Custom.Timeout <= 0 {
				return fmt.Errorf("service %s: custom.timeout must be positive", svc.Name)
			}
		case "http_get":
			if svc.Checker.HTTPGet == nil {
				return fmt.Errorf("service %s: http_get config is required for type %s", svc.Name, svc.Checker.Type)
			}
			if svc.Checker.HTTPGet.URL == "" {
				return fmt.Errorf("service %s: http_get.url is required", svc.Name)
			}
			if svc.Checker.HTTPGet.Timeout <= 0 {
				return fmt.Errorf("service %s: http_get.timeout must be positive", svc.Name)
			}
			if svc.Checker.HTTPGet.ProxyURL != "" {
				if err := validateProxyURL(svc.Checker.HTTPGet.ProxyURL, []string{"http", "https", "socks5", "socks5h"}); err != nil {
					return fmt.Errorf("service %s: http_get.%w", svc.Name, err)
				}
			}
		case "ping":
			if svc.Checker.Ping == nil {
				return fmt.Errorf("service %s: ping config is required for type %s", svc.Name, svc.Checker.Type)
			}
			if svc.Checker.Ping.Host == "" {
				return fmt.Errorf("service %s: ping.host is required", svc.Name)
			}
			if svc.Checker.Ping.Timeout <= 0 {
				return fmt.Errorf("service %s: ping.timeout must be positive", svc.Name)
			}
		default:
			return fmt.Errorf("service %s: unknown checker type %s", svc.Name, svc.Checker.Type)
		}

		// Validate notifiers
		notifierNames := make(map[string]bool)
		for j, n := range svc.Notifiers {
			if n.Name == "" {
				return fmt.Errorf("service %s: notifiers[%d]: name is required", svc.Name, j)
			}
			if notifierNames[n.Name] {
				return fmt.Errorf("service %s: notifiers[%d]: duplicate name", svc.Name, j)
			}
			notifierNames[n.Name] = true
			switch n.Type {
			case "custom":
				if n.Custom == nil {
					return fmt.Errorf("service %s: notifier %s: custom config is required for type %s", svc.Name, n.Name, n.Type)
				}
				if n.Custom.Command == "" {
					return fmt.Errorf("service %s: notifier %s: custom.command is required", svc.Name, n.Name)
				}
				if n.Custom.Shell == "" {
					c.Services[i].Notifiers[j].Custom.Shell = "bash"
				}
				if n.Custom.Timeout <= 0 {
					return fmt.Errorf("service %s: notifier %s: custom.timeout must be positive", svc.Name, n.Name)
				}
				if n.Custom.MaxAttempts == 0 {
					c.Services[i].Notifiers[j].Custom.MaxAttempts = 3
				} else if n.Custom.MaxAttempts < 0 {
					return fmt.Errorf("service %s: notifier %s: custom.max_attempts must be positive", svc.Name, n.Name)
				}
				if n.Custom.RetryInitialDelay == nil {
					d := 1 * duration.Second
					c.Services[i].Notifiers[j].Custom.RetryInitialDelay = &d
				} else if *n.Custom.RetryInitialDelay <= 0 {
					return fmt.Errorf("service %s: notifier %s: custom.retry_initial_delay must be positive", svc.Name, n.Name)
				}
				if n.Custom.RetryMaxDelay == nil {
					d := 30 * duration.Second
					c.Services[i].Notifiers[j].Custom.RetryMaxDelay = &d
				} else if *n.Custom.RetryMaxDelay <= 0 {
					return fmt.Errorf("service %s: notifier %s: custom.retry_max_delay must be positive", svc.Name, n.Name)
				}
			case "discord":
				if n.Discord == nil {
					return fmt.Errorf("service %s: notifier %s: discord config is required for type %s", svc.Name, n.Name, n.Type)
				}
				if n.Discord.WebhookURL == "" {
					return fmt.Errorf("service %s: notifier %s: discord.webhook_url is required", svc.Name, n.Name)
				}
				if n.Discord.Timeout <= 0 {
					return fmt.Errorf("service %s: notifier %s: discord.timeout must be positive", svc.Name, n.Name)
				}
				if n.Discord.MaxAttempts == 0 {
					c.Services[i].Notifiers[j].Discord.MaxAttempts = 3
				} else if n.Discord.MaxAttempts < 0 {
					return fmt.Errorf("service %s: notifier %s: discord.max_attempts must be positive", svc.Name, n.Name)
				}
				if n.Discord.RetryInitialDelay == nil {
					d := 1 * duration.Second
					c.Services[i].Notifiers[j].Discord.RetryInitialDelay = &d
				} else if *n.Discord.RetryInitialDelay <= 0 {
					return fmt.Errorf("service %s: notifier %s: discord.retry_initial_delay must be positive", svc.Name, n.Name)
				}
				if n.Discord.RetryMaxDelay == nil {
					d := 30 * duration.Second
					c.Services[i].Notifiers[j].Discord.RetryMaxDelay = &d
				} else if *n.Discord.RetryMaxDelay <= 0 {
					return fmt.Errorf("service %s: notifier %s: discord.retry_max_delay must be positive", svc.Name, n.Name)
				}
				if n.Discord.ProxyURL != "" {
					if err := validateProxyURL(n.Discord.ProxyURL, []string{"http", "https", "socks5", "socks5h"}); err != nil {
						return fmt.Errorf("service %s: notifier %s: discord.%w", svc.Name, n.Name, err)
					}
				}
			case "feishu":
				if n.Feishu == nil {
					return fmt.Errorf("service %s: notifier %s: feishu config is required for type %s", svc.Name, n.Name, n.Type)
				}
				if n.Feishu.WebhookURL == "" {
					return fmt.Errorf("service %s: notifier %s: feishu.webhook_url is required", svc.Name, n.Name)
				}
				if n.Feishu.Timeout <= 0 {
					return fmt.Errorf("service %s: notifier %s: feishu.timeout must be positive", svc.Name, n.Name)
				}
				if n.Feishu.MaxAttempts == 0 {
					c.Services[i].Notifiers[j].Feishu.MaxAttempts = 3
				} else if n.Feishu.MaxAttempts < 0 {
					return fmt.Errorf("service %s: notifier %s: feishu.max_attempts must be positive", svc.Name, n.Name)
				}
				if n.Feishu.RetryInitialDelay == nil {
					d := 1 * duration.Second
					c.Services[i].Notifiers[j].Feishu.RetryInitialDelay = &d
				} else if *n.Feishu.RetryInitialDelay <= 0 {
					return fmt.Errorf("service %s: notifier %s: feishu.retry_initial_delay must be positive", svc.Name, n.Name)
				}
				if n.Feishu.RetryMaxDelay == nil {
					d := 30 * duration.Second
					c.Services[i].Notifiers[j].Feishu.RetryMaxDelay = &d
				} else if *n.Feishu.RetryMaxDelay <= 0 {
					return fmt.Errorf("service %s: notifier %s: feishu.retry_max_delay must be positive", svc.Name, n.Name)
				}
				if n.Feishu.ProxyURL != "" {
					if err := validateProxyURL(n.Feishu.ProxyURL, []string{"http", "https", "socks5", "socks5h"}); err != nil {
						return fmt.Errorf("service %s: notifier %s: feishu.%w", svc.Name, n.Name, err)
					}
				}
			case "smtp":
				if n.SMTP == nil {
					return fmt.Errorf("service %s: notifier %s: smtp config is required for type %s", svc.Name, n.Name, n.Type)
				}
				if n.SMTP.Host == "" {
					return fmt.Errorf("service %s: notifier %s: smtp.host is required", svc.Name, n.Name)
				}
				if n.SMTP.Port <= 0 || n.SMTP.Port > 65535 {
					return fmt.Errorf("service %s: notifier %s: smtp.port must be between 1 and 65535", svc.Name, n.Name)
				}
				if n.SMTP.From == "" {
					return fmt.Errorf("service %s: notifier %s: smtp.from is required", svc.Name, n.Name)
				}
				if len(n.SMTP.To) == 0 {
					return fmt.Errorf("service %s: notifier %s: smtp.to must not be empty", svc.Name, n.Name)
				}
				switch n.SMTP.TLSMode {
				case "", "none", "starttls", "tls":
				default:
					return fmt.Errorf("service %s: notifier %s: smtp.tls_mode must be none, starttls, or tls", svc.Name, n.Name)
				}
				if n.SMTP.Timeout <= 0 {
					return fmt.Errorf("service %s: notifier %s: smtp.timeout must be positive", svc.Name, n.Name)
				}
				if n.SMTP.MaxAttempts == 0 {
					c.Services[i].Notifiers[j].SMTP.MaxAttempts = 3
				} else if n.SMTP.MaxAttempts < 0 {
					return fmt.Errorf("service %s: notifier %s: smtp.max_attempts must be positive", svc.Name, n.Name)
				}
				if n.SMTP.RetryInitialDelay == nil {
					d := 1 * duration.Second
					c.Services[i].Notifiers[j].SMTP.RetryInitialDelay = &d
				} else if *n.SMTP.RetryInitialDelay <= 0 {
					return fmt.Errorf("service %s: notifier %s: smtp.retry_initial_delay must be positive", svc.Name, n.Name)
				}
				if n.SMTP.RetryMaxDelay == nil {
					d := 30 * duration.Second
					c.Services[i].Notifiers[j].SMTP.RetryMaxDelay = &d
				} else if *n.SMTP.RetryMaxDelay <= 0 {
					return fmt.Errorf("service %s: notifier %s: smtp.retry_max_delay must be positive", svc.Name, n.Name)
				}
				if n.SMTP.ProxyURL != "" {
					if err := validateProxyURL(n.SMTP.ProxyURL, []string{"socks5", "socks5h"}); err != nil {
						return fmt.Errorf("service %s: notifier %s: smtp.%w", svc.Name, n.Name, err)
					}
				}
			case "spug":
				if n.Spug == nil {
					return fmt.Errorf("service %s: notifier %s: spug config is required for type %s", svc.Name, n.Name, n.Type)
				}
				if n.Spug.UserID == "" {
					return fmt.Errorf("service %s: notifier %s: spug.user_id is required", svc.Name, n.Name)
				}
				if n.Spug.Timeout <= 0 {
					return fmt.Errorf("service %s: notifier %s: spug.timeout must be positive", svc.Name, n.Name)
				}
				if n.Spug.MaxAttempts == 0 {
					c.Services[i].Notifiers[j].Spug.MaxAttempts = 3
				} else if n.Spug.MaxAttempts < 0 {
					return fmt.Errorf("service %s: notifier %s: spug.max_attempts must be positive", svc.Name, n.Name)
				}
				if n.Spug.RetryInitialDelay == nil {
					d := 1 * duration.Second
					c.Services[i].Notifiers[j].Spug.RetryInitialDelay = &d
				} else if *n.Spug.RetryInitialDelay <= 0 {
					return fmt.Errorf("service %s: notifier %s: spug.retry_initial_delay must be positive", svc.Name, n.Name)
				}
				if n.Spug.RetryMaxDelay == nil {
					d := 30 * duration.Second
					c.Services[i].Notifiers[j].Spug.RetryMaxDelay = &d
				} else if *n.Spug.RetryMaxDelay <= 0 {
					return fmt.Errorf("service %s: notifier %s: spug.retry_max_delay must be positive", svc.Name, n.Name)
				}
				if n.Spug.ProxyURL != "" {
					if err := validateProxyURL(n.Spug.ProxyURL, []string{"http", "https", "socks5", "socks5h"}); err != nil {
						return fmt.Errorf("service %s: notifier %s: spug.%w", svc.Name, n.Name, err)
					}
				}
			case "telegram":
				if n.Telegram == nil {
					return fmt.Errorf("service %s: notifier %s: telegram config is required for type %s", svc.Name, n.Name, n.Type)
				}
				if n.Telegram.BotToken == "" {
					return fmt.Errorf("service %s: notifier %s: telegram.bot_token is required", svc.Name, n.Name)
				}
				if n.Telegram.ChatID == "" {
					return fmt.Errorf("service %s: notifier %s: telegram.chat_id is required", svc.Name, n.Name)
				}
				if n.Telegram.Timeout <= 0 {
					return fmt.Errorf("service %s: notifier %s: telegram.timeout must be positive", svc.Name, n.Name)
				}
				if n.Telegram.MaxAttempts == 0 {
					c.Services[i].Notifiers[j].Telegram.MaxAttempts = 3
				} else if n.Telegram.MaxAttempts < 0 {
					return fmt.Errorf("service %s: notifier %s: telegram.max_attempts must be positive", svc.Name, n.Name)
				}
				if n.Telegram.RetryInitialDelay == nil {
					d := 1 * duration.Second
					c.Services[i].Notifiers[j].Telegram.RetryInitialDelay = &d
				} else if *n.Telegram.RetryInitialDelay <= 0 {
					return fmt.Errorf("service %s: notifier %s: telegram.retry_initial_delay must be positive", svc.Name, n.Name)
				}
				if n.Telegram.RetryMaxDelay == nil {
					d := 30 * duration.Second
					c.Services[i].Notifiers[j].Telegram.RetryMaxDelay = &d
				} else if *n.Telegram.RetryMaxDelay <= 0 {
					return fmt.Errorf("service %s: notifier %s: telegram.retry_max_delay must be positive", svc.Name, n.Name)
				}
				if n.Telegram.ProxyURL != "" {
					if err := validateProxyURL(n.Telegram.ProxyURL, []string{"http", "https", "socks5", "socks5h"}); err != nil {
						return fmt.Errorf("service %s: notifier %s: telegram.%w", svc.Name, n.Name, err)
					}
				}
			default:
				return fmt.Errorf("service %s: notifier %s: unknown notifier type %s", svc.Name, n.Name, n.Type)
			}
			if n.StatusSchedule != "" {
				if _, err := cron.Parse(n.StatusSchedule); err != nil {
					return fmt.Errorf("service %s: notifier %s: invalid status_schedule %s: %w", svc.Name, n.Name, n.StatusSchedule, err)
				}
			}
		}
	}
	botNames := make(map[string]bool)
	for i, b := range c.Bots {
		if b.Name == "" {
			return fmt.Errorf("bots[%d]: name is required", i)
		}
		if botNames[b.Name] {
			return fmt.Errorf("bots[%d]: duplicate name %s", i, b.Name)
		}
		botNames[b.Name] = true
		switch b.Type {
		case "telegram":
			if b.Telegram == nil {
				return fmt.Errorf("bot %s: telegram config is required for type %s", b.Name, b.Type)
			}
			if b.Telegram.BotToken == "" {
				return fmt.Errorf("bot %s: telegram.bot_token is required", b.Name)
			}
			if len(b.Telegram.AllowedChats) == 0 {
				return fmt.Errorf("bot %s: telegram.allowed_chats must not be empty", b.Name)
			}
			if b.Telegram.ProxyURL != "" {
				if err := validateProxyURL(b.Telegram.ProxyURL, []string{"http", "https", "socks5", "socks5h"}); err != nil {
					return fmt.Errorf("bot %s: telegram.%w", b.Name, err)
				}
			}
		default:
			return fmt.Errorf("bot %s: unknown type %s", b.Name, b.Type)
		}
	}
	return nil
}
