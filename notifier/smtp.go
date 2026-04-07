package notifier

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"errors"
	"fmt"
	"mime"
	"mime/quotedprintable"
	"net"
	"net/smtp"
	"strings"
	"time"

	"go.uber.org/zap"

	"mon/config"
	"mon/proxy"
)

// SMTPNotifier sends messages via SMTP.
type SMTPNotifier struct {
	name              string
	host              string
	port              int
	username          string
	password          string
	from              string
	to                []string
	tlsMode           string // "none" | "starttls" | "tls"
	tlsSkipVerify     bool
	timeout           time.Duration
	maxAttempts       int
	retryInitialDelay time.Duration
	retryMaxDelay     time.Duration
	proxyDialer       proxy.ContextDialer
	logger            *zap.SugaredLogger
}

func NewSMTPNotifier(name string, cfg *config.SMTPNotifierConfig, logger *zap.SugaredLogger) (*SMTPNotifier, error) {
	tlsMode := cfg.TLSMode
	if tlsMode == "" {
		tlsMode = "none"
	}
	proxyDialer, err := proxy.SOCKS5Dialer(cfg.ProxyURL)
	if err != nil {
		return nil, fmt.Errorf("proxy: %w", err)
	}
	return &SMTPNotifier{
		name:              name,
		host:              cfg.Host,
		port:              cfg.Port,
		username:          cfg.Username,
		password:          cfg.Password,
		from:              cfg.From,
		to:                cfg.To,
		tlsMode:           tlsMode,
		tlsSkipVerify:     cfg.TLSSkipVerify,
		timeout:           time.Duration(cfg.Timeout),
		maxAttempts:       cfg.MaxAttempts,
		retryInitialDelay: time.Duration(*cfg.RetryInitialDelay),
		retryMaxDelay:     time.Duration(*cfg.RetryMaxDelay),
		proxyDialer:       proxyDialer,
		logger:            logger,
	}, nil
}

func (s *SMTPNotifier) Type() string { return "smtp" }
func (s *SMTPNotifier) Name() string { return s.name }

func (s *SMTPNotifier) Notify(ctx context.Context, subject, body string) error {
	return withRetry(ctx, s.logger, s.name, s.maxAttempts, s.retryInitialDelay, s.retryMaxDelay, isRetryableSMTP, func(ctx context.Context) error {
		return s.send(ctx, subject, body)
	})
}

// dialRaw establishes a raw TCP connection, optionally through a SOCKS5 proxy.
func (s *SMTPNotifier) dialRaw(ctx context.Context, addr string, timeout time.Duration) (net.Conn, error) {
	if s.proxyDialer != nil {
		return s.proxyDialer.DialContext(ctx, "tcp", addr)
	}
	d := &net.Dialer{Timeout: timeout}
	return d.DialContext(ctx, "tcp", addr)
}

func (s *SMTPNotifier) send(ctx context.Context, subject, body string) error {
	// Append a random nonce to the subject to avoid spam filters deduplicating repeated identical messages.
	var nonce [4]byte
	_, _ = rand.Read(nonce[:])
	subject = fmt.Sprintf("%s (%x)", subject, nonce)

	message := buildMessage(s.from, s.to, subject, body)

	addr := fmt.Sprintf("%s:%d", s.host, s.port)
	tlsCfg := &tls.Config{
		ServerName:         s.host,
		InsecureSkipVerify: s.tlsSkipVerify, //nolint:gosec
	}

	connDeadline := time.Now().Add(s.timeout)
	if deadline, ok := ctx.Deadline(); ok {
		if deadline.Before(connDeadline) {
			connDeadline = deadline
		}
	}

	dialTimeout := time.Until(connDeadline)
	if dialTimeout <= 0 {
		return ctx.Err()
	}

	dialCtx, dialCancel := context.WithTimeout(ctx, dialTimeout)
	defer dialCancel()

	var c *smtp.Client
	var conn net.Conn
	var err error

	switch s.tlsMode {
	case "tls":
		conn, err = s.dialRaw(dialCtx, addr, dialTimeout)
		if err != nil {
			return err
		}
		tlsConn := tls.Client(conn, tlsCfg)
		if err = tlsConn.HandshakeContext(dialCtx); err != nil {
			conn.Close()
			return err
		}
		c, err = smtp.NewClient(tlsConn, s.host)
		if err != nil {
			tlsConn.Close()
			return err
		}
	default: // "none" or "starttls"
		conn, err = s.dialRaw(dialCtx, addr, dialTimeout)
		if err != nil {
			return err
		}
		c, err = smtp.NewClient(conn, s.host)
		if err != nil {
			conn.Close()
			return err
		}
		if s.tlsMode == "starttls" {
			if ok, _ := c.Extension("STARTTLS"); ok {
				if err = c.StartTLS(tlsCfg); err != nil {
					c.Close()
					return err
				}
			}
		}
	}
	defer c.Close()

	if err = conn.SetDeadline(connDeadline); err != nil {
		return err
	}

	if s.username != "" {
		auth := smtp.PlainAuth("", s.username, s.password, s.host)
		if err = c.Auth(auth); err != nil {
			return err
		}
	}

	if err = c.Mail(s.from); err != nil {
		return err
	}
	for _, rcpt := range s.to {
		if err = c.Rcpt(rcpt); err != nil {
			return err
		}
	}
	w, err := c.Data()
	if err != nil {
		return err
	}
	if _, err = w.Write([]byte(message)); err != nil {
		return err
	}
	if err = w.Close(); err != nil {
		return err
	}
	return c.Quit()
}

// buildMessage constructs a minimal RFC 5322 / RFC 2045 message.
// Headers are folded to stay within 76 characters per line (RFC 5322 §2.2.3).
// The body is encoded as quoted-printable (RFC 2045 §6.7), which enforces the
// 76-character line limit and normalises line endings to CRLF.
func buildMessage(from string, to []string, subject, body string) string {
	var sb strings.Builder

	sb.WriteString(foldHeader("From", from))
	sb.WriteString(foldHeader("To", strings.Join(to, ", ")))
	sb.WriteString(foldHeader("Subject", mime.QEncoding.Encode("UTF-8", subject)))
	sb.WriteString("Date: " + time.Now().UTC().Format(time.RFC1123Z) + "\r\n")

	var localPart [12]byte
	_, _ = rand.Read(localPart[:])
	domain := from
	if i := strings.LastIndex(from, "@"); i >= 0 {
		domain = from[i+1:]
	}
	sb.WriteString(fmt.Sprintf("Message-ID: <%x@%s>\r\n", localPart, domain))

	sb.WriteString("MIME-Version: 1.0\r\n")
	sb.WriteString("Content-Type: text/plain; charset=UTF-8\r\n")
	sb.WriteString("Content-Transfer-Encoding: quoted-printable\r\n")
	sb.WriteString("\r\n")

	w := quotedprintable.NewWriter(&sb)
	_, _ = w.Write([]byte(body))
	_ = w.Close()

	return sb.String()
}

// foldHeader formats an RFC 5322 header with line folding at whitespace
// boundaries. Lines exceeding 76 characters are folded by inserting CRLF
// immediately before a whitespace character; that whitespace becomes the first
// character of the continuation line, satisfying the "folded header" rule.
func foldHeader(name, value string) string {
	const maxLen = 76
	var sb strings.Builder
	line := name + ": " + value
	for len(line) > maxLen {
		fold := -1
		for i := maxLen; i > 0; i-- {
			if line[i] == ' ' || line[i] == '\t' {
				fold = i
				break
			}
		}
		if fold < 0 {
			for i := maxLen + 1; i < len(line); i++ {
				if line[i] == ' ' || line[i] == '\t' {
					fold = i
					break
				}
			}
		}
		if fold < 0 {
			break
		}
		sb.WriteString(line[:fold])
		sb.WriteString("\r\n")
		line = line[fold:] // whitespace is kept as the leading char of the next line
	}
	sb.WriteString(line)
	sb.WriteString("\r\n")
	return sb.String()
}

// isRetryableSMTP returns true for network-level errors (dial, timeout, etc.).
// SMTP protocol errors (auth failure, bad address) are not retried.
func isRetryableSMTP(err error) bool {
	var netErr net.Error
	return errors.As(err, &netErr)
}
