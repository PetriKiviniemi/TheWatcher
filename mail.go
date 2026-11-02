package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"mime"
	"mime/quotedprintable"
	"net"
	"net/mail"
	"net/smtp"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	defaultSMTPHost = "smtp.gmail.com"
	defaultSMTPPort = 587
	smtpTimeout     = 60 * time.Second
)

var hostPattern = regexp.MustCompile(`^[A-Za-z0-9.-]{1,253}$`)

// EmailSettings is the user's own SMTP account. An empty To turns email off.
// Password is input only: it is encrypted into Profile.EmailSecret and never sent back to the UI.
type EmailSettings struct {
	To       string `json:"to"`
	From     string `json:"from"`
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Password string `json:"password,omitempty"`
}

func (e EmailSettings) Enabled() bool { return e.To != "" }

func (e EmailSettings) withDefaults() EmailSettings {
	if strings.TrimSpace(e.Host) == "" {
		e.Host = defaultSMTPHost
	}
	if e.Port == 0 {
		e.Port = defaultSMTPPort
	}
	return e
}

// nextEmailSecret picks the encrypted password to store with new email settings: a newly typed one,
// or the saved one while the account is unchanged. A password never carries over to another account.
func nextEmailSecret(in EmailSettings, prev Profile) ([]byte, error) {
	switch {
	case !in.Enabled():
		return nil, nil
	case in.Password != "":
		return protect([]byte(in.Password))
	case len(prev.EmailSecret) > 0 && strings.EqualFold(prev.Email.From, in.From) && strings.EqualFold(prev.Email.Host, in.Host):
		return prev.EmailSecret, nil
	default:
		return nil, errors.New("enter the app password for this email account")
	}
}

func (e EmailSettings) validated() (EmailSettings, error) {
	e.To, e.From, e.Host = strings.TrimSpace(e.To), strings.TrimSpace(e.From), strings.TrimSpace(e.Host)
	if !e.Enabled() {
		return EmailSettings{From: e.From, Host: e.Host, Port: e.Port}, nil // email off: nothing to validate
	}
	switch {
	case !isPlainAddress(e.To):
		return e, errors.New("the alert email address is not valid")
	case !isPlainAddress(e.From):
		return e, errors.New("the sender email address is not valid")
	case !hostPattern.MatchString(e.Host):
		return e, errors.New("the SMTP server name is not valid")
	case e.Port < 1 || e.Port > 65535:
		return e, errors.New("the SMTP port must be between 1 and 65535")
	case e.Port == 465:
		return e, errors.New("port 465 (SSL) is not supported; use 587, which works for Gmail and Outlook")
	case len(e.Password) > 256:
		return e, errors.New("the app password is too long")
	}
	if strings.EqualFold(e.Host, defaultSMTPHost) {
		e.Password = strings.ReplaceAll(e.Password, " ", "") // Gmail shows app passwords in groups of four
	}
	return e, nil
}

// isPlainAddress accepts a bare address like "a@b.fi"; display names and line breaks are refused
// so user input can never add mail headers.
func isPlainAddress(s string) bool {
	a, err := mail.ParseAddress(s)
	return err == nil && a.Address == s && a.Name == ""
}

// sendMail delivers one plain-text message. It always upgrades to TLS with STARTTLS before logging in,
// so the password is never sent in clear text.
func sendMail(ctx context.Context, e EmailSettings, password, subject, body string, tlsConfig *tls.Config) error {
	addr := net.JoinHostPort(e.Host, strconv.Itoa(e.Port))
	conn, err := (&net.Dialer{Timeout: smtpTimeout}).DialContext(ctx, "tcp", addr)
	if err != nil {
		return fmt.Errorf("connecting to %s: %w", addr, err)
	}
	conn.SetDeadline(time.Now().Add(smtpTimeout)) // a stalled server must not block the watcher
	stop := context.AfterFunc(ctx, func() { conn.Close() })
	defer stop()
	c, err := smtp.NewClient(conn, e.Host)
	if err != nil {
		conn.Close()
		return fmt.Errorf("talking to %s: %w", addr, err)
	}
	defer c.Close()

	if ok, _ := c.Extension("STARTTLS"); !ok {
		return fmt.Errorf("%s does not offer STARTTLS; use the server's submission port (usually 587)", e.Host)
	}
	if tlsConfig == nil {
		tlsConfig = &tls.Config{ServerName: e.Host}
	}
	if err := c.StartTLS(tlsConfig); err != nil {
		return fmt.Errorf("starting TLS: %w", err)
	}
	if err := c.Auth(smtp.PlainAuth("", e.From, password, e.Host)); err != nil {
		return fmt.Errorf("logging in (check the address and app password): %w", err)
	}
	if err := c.Mail(e.From); err != nil {
		return err
	}
	if err := c.Rcpt(e.To); err != nil {
		return fmt.Errorf("recipient refused: %w", err)
	}
	w, err := c.Data()
	if err != nil {
		return err
	}
	if _, err := w.Write(composeMail(e.From, e.To, subject, body)); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	return c.Quit()
}

func composeMail(from, to, subject, body string) []byte {
	var b bytes.Buffer
	fmt.Fprintf(&b, "From: %s\r\nTo: %s\r\n", from, to)
	fmt.Fprintf(&b, "Subject: %s\r\n", mime.QEncoding.Encode("utf-8", subject))
	fmt.Fprintf(&b, "Date: %s\r\n", time.Now().Format(time.RFC1123Z))
	b.WriteString("MIME-Version: 1.0\r\nContent-Type: text/plain; charset=utf-8\r\nContent-Transfer-Encoding: quoted-printable\r\n\r\n")
	qp := quotedprintable.NewWriter(&b)
	qp.Write([]byte(strings.ReplaceAll(body, "\n", "\r\n")))
	qp.Close()
	return b.Bytes()
}
