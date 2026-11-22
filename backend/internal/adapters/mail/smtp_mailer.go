package mail

import (
	"crypto/tls"
	"fmt"
	"net"
	"net/smtp"
	"strings"
	"time"

	"github.com/opentermius/opentermius/internal/domain"
)

type SMTPMailer struct{}

func NewSMTPMailer() *SMTPMailer {
	return &SMTPMailer{}
}

func sanitizeHeader(v string) string {
	v = strings.ReplaceAll(v, "\r", "")
	v = strings.ReplaceAll(v, "\n", "")
	return strings.TrimSpace(v)
}

func (m *SMTPMailer) Send(cfg *domain.SMTPSettings, toEmail, subject, plainBody string) error {
	if cfg == nil || !cfg.Enabled {
		return fmt.Errorf("SMTP is disabled")
	}
	host := strings.TrimSpace(cfg.Host)
	fromEmail := sanitizeHeader(cfg.FromEmail)
	toEmail = sanitizeHeader(toEmail)
	if host == "" || fromEmail == "" || toEmail == "" {
		return fmt.Errorf("incomplete SMTP configuration (host, from_email, recipient required)")
	}
	port := cfg.Port
	if port <= 0 {
		port = 587
	}

	fromName := sanitizeHeader(cfg.FromName)
	if fromName == "" {
		fromName = "OpenTermius Gateway"
	}
	subject = sanitizeHeader(subject)

	msg := strings.Builder{}
	msg.WriteString(fmt.Sprintf("From: %s <%s>\r\n", fromName, fromEmail))
	msg.WriteString(fmt.Sprintf("To: %s\r\n", toEmail))
	msg.WriteString(fmt.Sprintf("Subject: %s\r\n", subject))
	msg.WriteString("MIME-Version: 1.0\r\n")
	msg.WriteString("Content-Type: text/plain; charset=UTF-8\r\n")
	msg.WriteString("\r\n")
	msg.WriteString(plainBody)

	addr := net.JoinHostPort(host, fmt.Sprintf("%d", port))
	dialer := &net.Dialer{Timeout: 10 * time.Second}

	var client *smtp.Client
	var err error

	if cfg.UseTLS && port == 465 {
		tlsConn, dialErr := tls.DialWithDialer(dialer, "tcp", addr, &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12})
		if dialErr != nil {
			return fmt.Errorf("SMTP TLS dial failed: %w", dialErr)
		}
		client, err = smtp.NewClient(tlsConn, host)
		if err != nil {
			_ = tlsConn.Close()
			return fmt.Errorf("SMTP client init failed: %w", err)
		}
	} else {
		conn, dialErr := dialer.Dial("tcp", addr)
		if dialErr != nil {
			return fmt.Errorf("SMTP dial failed: %w", dialErr)
		}
		client, err = smtp.NewClient(conn, host)
		if err != nil {
			_ = conn.Close()
			return fmt.Errorf("SMTP client init failed: %w", err)
		}
		if cfg.UseTLS {
			if ok, _ := client.Extension("STARTTLS"); ok {
				if err := client.StartTLS(&tls.Config{ServerName: host, MinVersion: tls.VersionTLS12}); err != nil {
					_ = client.Close()
					return fmt.Errorf("SMTP STARTTLS failed: %w", err)
				}
			}
		}
	}
	defer client.Close()

	if cfg.Username != "" {
		auth := smtp.PlainAuth("", cfg.Username, cfg.Password, host)
		if err := client.Auth(auth); err != nil {
			return fmt.Errorf("SMTP authentication failed: %w", err)
		}
	}

	if err := client.Mail(fromEmail); err != nil {
		return fmt.Errorf("SMTP MAIL FROM failed: %w", err)
	}
	if err := client.Rcpt(toEmail); err != nil {
		return fmt.Errorf("SMTP RCPT TO failed: %w", err)
	}
	w, err := client.Data()
	if err != nil {
		return fmt.Errorf("SMTP DATA failed: %w", err)
	}
	if _, err := w.Write([]byte(msg.String())); err != nil {
		_ = w.Close()
		return fmt.Errorf("SMTP body write failed: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("SMTP data close failed: %w", err)
	}
	_ = client.Quit()
	return nil
}
