package mail

import (
	"crypto/tls"
	"fmt"
	"net"
	"net/smtp"
	"strings"

	"github.com/Dima201030/ligaBackend/internal/config"
)

// Sender delivers rendered messages.
type Sender interface {
	Send(to string, msg Message) error
}

// SMTPSender sends via net/smtp (STARTTLS on 587, plain TLS on 465).
type SMTPSender struct {
	cfg config.Config
}

// NewSMTPSender builds a sender from config.
func NewSMTPSender(cfg config.Config) *SMTPSender {
	return &SMTPSender{cfg: cfg}
}

// Send delivers one message.
func (s *SMTPSender) Send(to string, msg Message) error {
	if !s.cfg.HasSMTP() {
		return fmt.Errorf("smtp not configured")
	}
	to = strings.TrimSpace(to)
	if to == "" {
		return fmt.Errorf("empty recipient")
	}
	from := s.cfg.SMTPFrom
	addr := fmt.Sprintf("%s:%d", s.cfg.SMTPHost, s.cfg.SMTPPort)
	body := buildMIME(from, to, msg.Subject, msg.TextBody)

	if s.cfg.SMTPPort == 465 {
		return s.sendTLS(addr, from, to, body)
	}
	return s.sendStartTLS(addr, from, to, body)
}

func (s *SMTPSender) sendStartTLS(addr, from, to string, body []byte) error {
	host := s.cfg.SMTPHost
	c, err := smtp.Dial(addr)
	if err != nil {
		return err
	}
	defer c.Close()

	if ok, _ := c.Extension("STARTTLS"); ok {
		tlsCfg := &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12}
		if err := c.StartTLS(tlsCfg); err != nil {
			return err
		}
	}
	if err := s.auth(c, host); err != nil {
		return err
	}
	if err := c.Mail(from); err != nil {
		return err
	}
	if err := c.Rcpt(to); err != nil {
		return err
	}
	w, err := c.Data()
	if err != nil {
		return err
	}
	if _, err := w.Write(body); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	return c.Quit()
}

func (s *SMTPSender) sendTLS(addr, from, to string, body []byte) error {
	host := s.cfg.SMTPHost
	tlsCfg := &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12}
	conn, err := tls.Dial("tcp", addr, tlsCfg)
	if err != nil {
		return err
	}
	defer conn.Close()
	c, err := smtp.NewClient(conn, host)
	if err != nil {
		return err
	}
	defer c.Close()
	if err := s.auth(c, host); err != nil {
		return err
	}
	if err := c.Mail(from); err != nil {
		return err
	}
	if err := c.Rcpt(to); err != nil {
		return err
	}
	w, err := c.Data()
	if err != nil {
		return err
	}
	if _, err := w.Write(body); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	return c.Quit()
}

func (s *SMTPSender) auth(c *smtp.Client, host string) error {
	user := strings.TrimSpace(s.cfg.SMTPUser)
	if user == "" {
		return nil
	}
	auth := smtp.PlainAuth("", user, s.cfg.SMTPPassword, host)
	return c.Auth(auth)
}

func buildMIME(from, to, subject, text string) []byte {
	var b strings.Builder
	b.WriteString("From: " + from + "\r\n")
	b.WriteString("To: " + to + "\r\n")
	b.WriteString("Subject: " + sanitizeHeader(subject) + "\r\n")
	b.WriteString("MIME-Version: 1.0\r\n")
	b.WriteString("Content-Type: text/plain; charset=UTF-8\r\n")
	b.WriteString("Content-Transfer-Encoding: 8bit\r\n")
	b.WriteString("\r\n")
	b.WriteString(strings.ReplaceAll(text, "\n", "\r\n"))
	if !strings.HasSuffix(text, "\n") {
		b.WriteString("\r\n")
	}
	return []byte(b.String())
}

func sanitizeHeader(s string) string {
	s = strings.ReplaceAll(s, "\r", "")
	s = strings.ReplaceAll(s, "\n", "")
	return s
}

// ResolveSMTPAddr is a tiny helper for tests/docs.
func ResolveSMTPAddr(host string, port int) string {
	return net.JoinHostPort(host, fmt.Sprintf("%d", port))
}
