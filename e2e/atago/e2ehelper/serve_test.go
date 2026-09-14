package main

import (
	"bufio"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// startServe runs serve and waits, up to the test's deadline, for the
// address it writes.
func startServe(t *testing.T) string {
	t.Helper()
	addrFile := filepath.Join(t.TempDir(), "addr")
	// serve answers until the process ends, which for a test binary is
	// when the tests are done.
	go func() { _ = serve([]string{"-addr", addrFile}) }()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	for {
		if b, err := os.ReadFile(addrFile); err == nil {
			return strings.TrimSpace(string(b))
		}
		select {
		case <-ctx.Done():
			t.Fatal("serve wrote no address")
		case <-tick.C:
		}
	}
}

// exchange sends raw request text on one connection and returns what came
// back once the server has answered and the client has closed its side.
func exchange(t *testing.T, addr, request string, stop string) string {
	t.Helper()
	conn, err := new(net.Dialer).DialContext(context.Background(), "tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(30 * time.Second))
	if _, err := io.WriteString(conn, request); err != nil {
		t.Fatal(err)
	}
	var got strings.Builder
	br := bufio.NewReader(conn)
	for !strings.Contains(got.String(), stop) {
		line, err := br.ReadString('\n')
		got.WriteString(line)
		if err != nil {
			break
		}
	}
	return got.String()
}

func TestServeAnswersHTTP1ByHand(t *testing.T) {
	addr := startServe(t)
	for _, tc := range []struct {
		request, want string
	}{
		{"GET /plain HTTP/1.1\r\nHost: x\r\n\r\n", "x-lower-case: yes\r\nX-Empty:\r\nCache-Control: no-cache, no-store\r\n\r\nhello\n"},
		{"GET /no-reason HTTP/1.1\r\nHost: x\r\n\r\n", "HTTP/1.1 204\r\n"},
		{"GET /chain HTTP/1.1\r\nHost: x\r\n\r\n", "HTTP/1.1 302 Found\r\nLocation: /redirect\r\n"},
		{"GET /redirect HTTP/1.1\r\nHost: x\r\n\r\n", "HTTP/1.1 301 Moved Permanently\r\n"},
		{"GET /early-hints HTTP/1.1\r\nHost: x\r\n\r\n", "HTTP/1.1 103 Early Hints\r\n"},
		{"GET /continue HTTP/1.1\r\nHost: x\r\n\r\n", "HTTP/1.1 100 Continue\r\n\r\nHTTP/1.1 200 OK\r\n"},
		{"GET /http10 HTTP/1.0\r\nHost: x\r\n\r\n", "HTTP/1.0 200 OK\r\nServer: e2ehelper\r\n"},
		{"GET /missing HTTP/1.1\r\nHost: x\r\n\r\n", "HTTP/1.1 404 Not Found\r\n"},
		// A request through a proxy names the whole URL.
		{"GET http://example.test/plain HTTP/1.1\r\nHost: x\r\n\r\n", "HTTP/1.1 200 OK\r\n"},
		{"GET http://example.test HTTP/1.1\r\nHost: x\r\n\r\n", "HTTP/1.1 404 Not Found\r\n"},
		// CONNECT is answered as a proxy does, and the connection then
		// carries the next request.
		{"CONNECT example.test:80 HTTP/1.1\r\n\r\nGET /http10 HTTP/1.1\r\nHost: x\r\n\r\n", "HTTP/1.1 200 Connection established\r\n\r\nHTTP/1.0 200 OK\r\n"},
	} {
		if got := exchange(t, addr, tc.request, tc.want); !strings.Contains(got, tc.want) {
			t.Errorf("%q answered\n%q\nwant %q in it", tc.request, got, tc.want)
		}
	}
	// HEAD has no body.
	if got := exchange(t, addr, "HEAD /plain HTTP/1.1\r\nHost: x\r\n\r\n", "no-store\r\n\r\n"); strings.Contains(got, "hello") {
		t.Errorf("HEAD answered with a body: %q", got)
	}
	// A request line that is not one ends the connection without an answer.
	if got := exchange(t, addr, "not a request line at all\r\n\r\n", "\x00"); got != "" {
		t.Errorf("a bad request line was answered: %q", got)
	}
}

func TestServeSpeaksHTTP2WithoutTLS(t *testing.T) {
	addr := startServe(t)
	tr := &http.Transport{Protocols: new(http.Protocols)}
	tr.Protocols.SetUnencryptedHTTP2(true)
	client := &http.Client{Transport: tr, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	for path, want := range map[string]int{"/plain": 200, "/redirect": 301, "/early-hints": 200, "/missing": 404} {
		req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://"+addr+path, nil)
		if err != nil {
			t.Fatal(err)
		}
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		b, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if resp.StatusCode != want || resp.ProtoMajor != 2 || string(b) != body {
			t.Errorf("%s: %d over HTTP/%d, body %q", path, resp.StatusCode, resp.ProtoMajor, b)
		}
	}
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodHead, "http://"+addr+"/plain", nil)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if len(resp.Header.Values("Set-Cookie")) != 2 {
		t.Errorf("HEAD /plain: %v", resp.Header)
	}
}

func TestServeNeedsAnAddressFile(t *testing.T) {
	if err := serve(nil); err == nil || !strings.Contains(err.Error(), "-addr is required") {
		t.Errorf("serve() = %v", err)
	}
	if err := serve([]string{"-bogus"}); err == nil {
		t.Error("an unknown flag was accepted")
	}
	if err := serve([]string{"-addr", filepath.Join(t.TempDir(), "missing", "addr")}); err == nil {
		t.Error("an address file in a missing directory was accepted")
	}
}

// A listener fed by hand hands over what it is given until it is closed,
// and then refuses both.
func TestConnListener(t *testing.T) {
	l := newConnListener(&net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 1})
	a, b := net.Pipe()
	defer b.Close()
	go l.hand(a)
	if c, err := l.Accept(); err != nil || c != a {
		t.Errorf("Accept = %v, %v", c, err)
	}
	if l.Addr().String() != "127.0.0.1:1" {
		t.Errorf("Addr = %s", l.Addr())
	}
	_ = l.Close()
	_ = l.Close()
	if _, err := l.Accept(); !errors.Is(err, net.ErrClosed) {
		t.Errorf("Accept after Close = %v", err)
	}
	c, d := net.Pipe()
	defer d.Close()
	l.hand(c)
	if _, err := c.Write([]byte("x")); err == nil {
		t.Error("a connection handed to a closed listener was left open")
	}
}
