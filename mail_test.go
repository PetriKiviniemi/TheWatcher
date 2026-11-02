package main

import (
	"bufio"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"net"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeSMTP is a minimal submission server: STARTTLS, AUTH PLAIN, one message per session.
type fakeSMTP struct {
	host      string
	port      int
	client    *tls.Config // trusts the fake server's certificate
	noTLS     bool        // pretend STARTTLS is not offered
	rejectPwd bool

	mu     sync.Mutex
	logins []string // "user:password"
	mails  []string
}

func newFakeSMTP(t *testing.T) *fakeSMTP {
	t.Helper()
	certSrv := httptest.NewTLSServer(http.NotFoundHandler()) // borrow its 127.0.0.1 certificate
	t.Cleanup(certSrv.Close)
	pool := x509.NewCertPool()
	pool.AddCert(certSrv.Certificate())
	serverTLS := &tls.Config{Certificates: certSrv.TLS.Certificates}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	f := &fakeSMTP{host: "127.0.0.1", port: ln.Addr().(*net.TCPAddr).Port, client: &tls.Config{RootCAs: pool, ServerName: "127.0.0.1"}}
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go f.serve(conn, serverTLS)
		}
	}()
	return f
}

func (f *fakeSMTP) serve(conn net.Conn, serverTLS *tls.Config) {
	defer func() { conn.Close() }()
	tp := textproto.NewConn(conn)
	secure := false
	tp.PrintfLine("220 fake ready")
	for {
		line, err := tp.ReadLine()
		if err != nil {
			return
		}
		cmd, arg, _ := strings.Cut(line, " ")
		switch strings.ToUpper(cmd) {
		case "EHLO", "HELO":
			if !secure && !f.noTLS {
				tp.PrintfLine("250-fake\r\n250-STARTTLS\r\n250 AUTH PLAIN")
			} else {
				tp.PrintfLine("250-fake\r\n250 AUTH PLAIN")
			}
		case "STARTTLS":
			tp.PrintfLine("220 go ahead")
			tlsConn := tls.Server(conn, serverTLS)
			if tlsConn.Handshake() != nil {
				return
			}
			conn, secure = tlsConn, true
			tp = textproto.NewConn(conn)
		case "AUTH":
			raw, _ := base64.StdEncoding.DecodeString(strings.TrimPrefix(arg, "PLAIN "))
			parts := strings.Split(string(raw), "\x00")
			f.mu.Lock()
			f.logins = append(f.logins, parts[1]+":"+parts[2])
			f.mu.Unlock()
			if f.rejectPwd {
				tp.PrintfLine("535 bad credentials")
			} else {
				tp.PrintfLine("235 ok")
			}
		case "MAIL", "RCPT":
			tp.PrintfLine("250 ok")
		case "DATA":
			tp.PrintfLine("354 send it")
			data, _ := tp.ReadDotBytes()
			f.mu.Lock()
			f.mails = append(f.mails, string(data))
			f.mu.Unlock()
			tp.PrintfLine("250 queued")
		case "QUIT":
			tp.PrintfLine("221 bye")
			return
		default:
			tp.PrintfLine("502 unknown")
		}
	}
}

func (f *fakeSMTP) settings() EmailSettings {
	return EmailSettings{To: "buyer@example.com", From: "me@gmail.example", Host: f.host, Port: f.port}
}

func (f *fakeSMTP) loginList() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.logins...)
}

func (f *fakeSMTP) sent() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.mails...)
}

func TestSendMailOverStartTLS(t *testing.T) {
	f := newFakeSMTP(t)
	err := sendMail(context.Background(), f.settings(), "app-pass", "3 new plot(s) for änna", "Kellonlahdentie 5\nrow two", f.client)
	if err != nil {
		t.Fatal(err)
	}
	mails := f.sent()
	if logins := f.loginList(); len(mails) != 1 || logins[0] != "me@gmail.example:app-pass" {
		t.Fatalf("want one mail sent after login, got %d mails, logins %v", len(mails), logins)
	}
	for _, want := range []string{"To: buyer@example.com", "Subject: =?utf-8?q?", "Kellonlahdentie 5"} {
		if !strings.Contains(mails[0], want) {
			t.Errorf("mail lacks %q:\n%s", want, mails[0])
		}
	}
}

func TestSendMailRefusesWithoutTLS(t *testing.T) {
	f := newFakeSMTP(t)
	f.noTLS = true
	err := sendMail(context.Background(), f.settings(), "app-pass", "s", "b", f.client)
	if err == nil || !strings.Contains(err.Error(), "STARTTLS") {
		t.Fatalf("want a STARTTLS error, got %v", err)
	}
	if len(f.loginList()) != 0 {
		t.Fatal("the password must never be sent over an unencrypted connection")
	}
}

func TestSendMailReportsBadPassword(t *testing.T) {
	f := newFakeSMTP(t)
	f.rejectPwd = true
	if err := sendMail(context.Background(), f.settings(), "wrong", "s", "b", f.client); err == nil || !strings.Contains(err.Error(), "app password") {
		t.Fatalf("want a login error mentioning the app password, got %v", err)
	}
}

func TestSendMailStopsWhenCancelled(t *testing.T) {
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	defer ln.Close()
	go func() { // accepts but never greets
		c, err := ln.Accept()
		if err == nil {
			defer c.Close()
			bufio.NewReader(c).ReadString('\n')
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	e := EmailSettings{To: "a@b.fi", From: "c@d.fi", Host: "127.0.0.1", Port: ln.Addr().(*net.TCPAddr).Port}
	done := make(chan error, 1)
	go func() { done <- sendMail(ctx, e, "p", "s", "b", nil) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("sendMail must stop as soon as its context is cancelled")
	}
}

func TestEmailValidation(t *testing.T) {
	ok := EmailSettings{To: "buyer@example.com", From: "me@gmail.com", Host: "smtp.gmail.com", Port: 587, Password: "abcd efgh ijkl mnop"}
	got, err := ok.validated()
	if err != nil || got.Password != "abcdefghijklmnop" {
		t.Fatalf("valid Gmail settings rejected or spaces kept: %v %q", err, got.Password)
	}
	bad := []EmailSettings{
		{To: "not-an-address", From: "me@gmail.com", Host: "smtp.gmail.com", Port: 587},
		{To: "Evil <a@b.fi>", From: "me@gmail.com", Host: "smtp.gmail.com", Port: 587},
		{To: "a@b.fi\r\nBcc: victim@x.fi", From: "me@gmail.com", Host: "smtp.gmail.com", Port: 587},
		{To: "a@b.fi", From: "me@gmail.com", Host: "smtp.gmail.com; rm", Port: 587},
		{To: "a@b.fi", From: "me@gmail.com", Host: "smtp.gmail.com", Port: 70000},
		{To: "a@b.fi", From: "me@gmail.com", Host: "smtp.gmail.com", Port: 465},
	}
	for _, e := range bad {
		if _, err := e.validated(); err == nil {
			t.Errorf("want error for %+v", e)
		}
	}
	if off, err := (EmailSettings{From: "x", Password: "secret"}).validated(); err != nil || off.Password != "" {
		t.Errorf("disabled email must validate and drop the password: %v %+v", err, off)
	}
}

func TestEmailPasswordIsKeptPrivate(t *testing.T) {
	store, w, _ := newTestApp(t)
	h := newServer(store, w, "47321", nil, func() {})
	put := func(body string) (int, string) {
		r := httptest.NewRequest(http.MethodPut, "/api/profiles/anna", strings.NewReader(body))
		r.Host = "127.0.0.1:47321"
		r.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, r)
		return rec.Code, rec.Body.String()
	}
	settings := func(from, password string) string {
		return `{"topic":"","intervalMinutes":30,"filter":{"unitClassIds":[],"keywords":[]},` +
			`"email":{"to":"buyer@example.com","from":"` + from + `","host":"smtp.gmail.com","port":587,"password":"` + password + `"}}`
	}

	if code, _ := put(settings("me@gmail.com", "")); code != http.StatusBadRequest {
		t.Fatalf("enabling email without a password must fail, got %d", code)
	}
	code, body := put(settings("me@gmail.com", "app-pass-123"))
	if code != http.StatusOK || strings.Contains(body, "app-pass-123") || !strings.Contains(body, `"emailPasswordSet":true`) {
		t.Fatalf("the password must be stored but never returned: %d %s", code, body)
	}
	if code, _ := put(settings("me@gmail.com", "")); code != http.StatusOK {
		t.Fatalf("re-saving the same account must keep the saved password, got %d", code)
	}
	if code, _ := put(settings("other@gmail.com", "")); code != http.StatusBadRequest {
		t.Fatalf("a saved password must not carry over to another account, got %d", code)
	}
	if p := store.Get("anna"); p.Email.Password != "" || string(p.EmailSecret) != "app-pass-123" {
		t.Fatalf("only the protected copy may be stored: %+v", p.Email)
	}
}

func TestWatcherEmailsNewPlots(t *testing.T) {
	store, w, f := newTestApp(t)
	smtpSrv := newFakeSMTP(t)
	w.smtpTLS = smtpSrv.client
	secret, _ := protect([]byte("app-pass"))
	store.Update("anna", func(p *Profile) { p.Email, p.EmailSecret = smtpSrv.settings(), secret })
	start := time.Now()
	checkAt(w, start)

	f.sites = append(f.sites, site(func(s *Site) { s.Id = 2; s.Address = "Uusitie 1" }))
	checkAt(w, start.Add(31*time.Minute))
	mails := smtpSrv.sent()
	if len(mails) != 1 || !strings.Contains(mails[0], "Uusitie 1") || len(f.sent) != 0 {
		t.Fatalf("want one email (and no push without a topic), got %d mails, %d pushes", len(mails), len(f.sent))
	}
}

func TestBrokenEmailDoesNotRepeatPush(t *testing.T) {
	store, w, f := newTestApp(t)
	smtpSrv := newFakeSMTP(t)
	smtpSrv.rejectPwd = true
	w.smtpTLS = smtpSrv.client
	store.Update("anna", func(p *Profile) { p.Topic, p.Email, p.EmailSecret = "topic1", smtpSrv.settings(), []byte("x") })
	start := time.Now()
	checkAt(w, start)

	f.sites = append(f.sites, site(func(s *Site) { s.Id = 2 }))
	checkAt(w, start.Add(31*time.Minute))
	if len(f.sent) != 1 || !strings.Contains(store.Get("anna").LastError, "email") {
		t.Fatalf("push must go out and the email failure be shown; pushes=%d err=%q", len(f.sent), store.Get("anna").LastError)
	}
	checkAt(w, start.Add(62*time.Minute))
	p := store.Get("anna")
	if len(f.sent) != 1 {
		t.Fatalf("a failing email must not make the push repeat, got %d pushes", len(f.sent))
	}
	if p.LastError != "" || p.EmailError == "" {
		t.Fatalf("the email problem must stay visible after a quiet check: last=%q email=%q", p.LastError, p.EmailError)
	}
}
