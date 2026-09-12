package main

// serve is the HTTP server the curl scenarios talk to, so that they need
// no network and no service outside the suite:
//
//	e2ehelper serve -addr FILE
//
// listens on a free port of 127.0.0.1, writes host:port to FILE and
// answers until it is stopped. HTTP/1.x responses are written by hand,
// byte for byte, because the scenarios are about what a server may put
// on the wire that Go's own server never would: a status line with no
// reason phrase, a header name in odd case, an interim response before
// the final one, a proxy's answer to CONNECT. A connection that opens
// with the HTTP/2 preface is handed to Go's server, which speaks HTTP/2
// without TLS to a client that knows to (curl --http2-prior-knowledge).
//
// The paths:
//
//	/plain        200 with two Set-Cookie headers and names in mixed case
//	/no-reason    204 with a status line that ends after the code
//	/redirect     301 to /plain
//	/chain        302 to /redirect
//	/early-hints  103 Early Hints, then 200
//	/continue     100 Continue, then 200
//	/http10       an HTTP/1.0 200
//	anything else 404
//
// A CONNECT request is answered "200 Connection established" and the
// connection is then read as a tunnel to this same server, which is what
// curl --proxytunnel sees from a proxy.

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

// h2Preface opens every HTTP/2 connection.
const h2Preface = "PRI * HTTP/2.0\r\n\r\nSM\r\n\r\n"

func serve(args []string) error {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	addrFile := fs.String("addr", "", "write the address listened on here")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *addrFile == "" {
		return errors.New("serve: -addr is required")
	}
	ln, err := new(net.ListenConfig).Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	h2 := newConnListener(ln.Addr())
	srv := &http.Server{Handler: http.HandlerFunc(h2Handler), ReadHeaderTimeout: 10 * time.Second}
	srv.Protocols = new(http.Protocols)
	srv.Protocols.SetUnencryptedHTTP2(true)
	go func() { _ = srv.Serve(h2) }()
	// The address is written once the listener is open, and written
	// whole: a reader that sees the file sees the address.
	tmp := *addrFile + ".tmp"
	if err := os.WriteFile(tmp, []byte(ln.Addr().String()+"\n"), 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, *addrFile); err != nil {
		return err
	}
	for {
		conn, err := ln.Accept()
		if err != nil {
			return err
		}
		go handleConn(conn, h2)
	}
}

// handleConn reads HTTP/1.x requests off one connection, or hands the
// connection to the HTTP/2 server when it opens with the preface.
func handleConn(conn net.Conn, h2 *connListener) {
	br := bufio.NewReader(conn)
	if peek, err := br.Peek(len(h2Preface)); err == nil && string(peek) == h2Preface {
		h2.hand(&bufferedConn{Conn: conn, r: br})
		return
	}
	defer func() { _ = conn.Close() }()
	serveHTTP1(conn, br)
}

// serveHTTP1 answers requests until the client closes the connection.
func serveHTTP1(w io.Writer, br *bufio.Reader) {
	for {
		method, target, err := readRequest(br)
		if err != nil {
			return
		}
		if method == http.MethodConnect {
			fmt.Fprint(w, "HTTP/1.1 200 Connection established\r\n\r\n")
			continue
		}
		fmt.Fprint(w, response(method, target))
	}
}

// readRequest reads a request line and its headers, and discards the
// headers: the answer depends on the method and the path alone.
func readRequest(br *bufio.Reader) (method, target string, err error) {
	line, err := br.ReadString('\n')
	if err != nil {
		return "", "", err
	}
	fields := strings.Fields(line)
	if len(fields) != 3 {
		return "", "", fmt.Errorf("bad request line %q", line)
	}
	for {
		h, err := br.ReadString('\n')
		if err != nil {
			return "", "", err
		}
		if strings.TrimRight(h, "\r\n") == "" {
			break
		}
	}
	target = fields[1]
	if i := strings.Index(target, "://"); i >= 0 {
		// A request through a proxy names the whole URL.
		if j := strings.IndexByte(target[i+3:], '/'); j >= 0 {
			target = target[i+3+j:]
		} else {
			target = "/"
		}
	}
	return fields[0], target, nil
}

const body = "hello\n"

// response is the whole answer to one request, interim responses first.
func response(method, target string) string {
	var b strings.Builder
	final := func(status string, headers ...string) {
		b.WriteString(status)
		b.WriteString("\r\n")
		for _, h := range headers {
			b.WriteString(h)
			b.WriteString("\r\n")
		}
		if method == http.MethodHead {
			b.WriteString("\r\n")
			return
		}
		fmt.Fprintf(&b, "\r\n%s", body)
	}
	length := fmt.Sprintf("Content-Length: %d", len(body))
	switch target {
	case "/plain":
		final("HTTP/1.1 200 OK",
			"Date: Fri, 11 Sep 2026 12:00:00 GMT",
			"Content-Type: text/plain; charset=utf-8",
			length,
			"Set-Cookie: session=abc123; Path=/; HttpOnly",
			"Set-Cookie: theme=dark; Path=/",
			"X-Request-ID: 7f3a",
			"x-lower-case: yes",
			"X-Empty:",
			"Cache-Control: no-cache, no-store")
	case "/no-reason":
		b.WriteString("HTTP/1.1 204\r\nDate: Fri, 11 Sep 2026 12:00:00 GMT\r\n\r\n")
	case "/redirect":
		final("HTTP/1.1 301 Moved Permanently", "Location: /plain", "Content-Length: 0")
	case "/chain":
		final("HTTP/1.1 302 Found", "Location: /redirect", "Content-Length: 0")
	case "/early-hints":
		b.WriteString("HTTP/1.1 103 Early Hints\r\nLink: </style.css>; rel=preload; as=style\r\n\r\n")
		final("HTTP/1.1 200 OK", "Content-Type: text/html", "Link: </style.css>; rel=preload; as=style", length)
	case "/continue":
		b.WriteString("HTTP/1.1 100 Continue\r\n\r\n")
		final("HTTP/1.1 200 OK", "Content-Type: text/plain", length)
	case "/http10":
		final("HTTP/1.0 200 OK", "Server: e2ehelper", length)

	default:
		final("HTTP/1.1 404 Not Found", "Content-Type: text/plain", length)
	}
	return b.String()
}

// h2Handler answers HTTP/2 requests: the same paths, as far as Go's
// server lets them be said.
func h2Handler(w http.ResponseWriter, r *http.Request) {
	h := w.Header()
	switch r.URL.Path {
	case "/plain":
		h.Set("Content-Type", "text/plain; charset=utf-8")
		h.Add("Set-Cookie", "session=abc123; Path=/; HttpOnly")
		h.Add("Set-Cookie", "theme=dark; Path=/")
		h.Set("Date", "Fri, 11 Sep 2026 12:00:00 GMT")
		w.WriteHeader(http.StatusOK)
	case "/redirect":
		h.Set("Location", "/plain")
		h.Set("Date", "Fri, 11 Sep 2026 12:00:00 GMT")
		w.WriteHeader(http.StatusMovedPermanently)
	case "/early-hints":
		h.Set("Link", "</style.css>; rel=preload; as=style")
		w.WriteHeader(http.StatusEarlyHints)
		h.Set("Content-Type", "text/html")
		h.Set("Date", "Fri, 11 Sep 2026 12:00:00 GMT")
		w.WriteHeader(http.StatusOK)
	default:
		h.Set("Date", "Fri, 11 Sep 2026 12:00:00 GMT")
		w.WriteHeader(http.StatusNotFound)
	}
	if r.Method != http.MethodHead {
		_, _ = io.WriteString(w, body)
	}
}

// connListener is a net.Listener fed by hand, which is how connections
// that turn out to be HTTP/2 reach the HTTP/2 server.
type connListener struct {
	addr  net.Addr
	conns chan net.Conn
	once  sync.Once
	done  chan struct{}
}

func newConnListener(addr net.Addr) *connListener {
	return &connListener{addr: addr, conns: make(chan net.Conn), done: make(chan struct{})}
}

func (l *connListener) hand(c net.Conn) {
	select {
	case l.conns <- c:
	case <-l.done:
		_ = c.Close()
	}
}

func (l *connListener) Accept() (net.Conn, error) {
	select {
	case c := <-l.conns:
		return c, nil
	case <-l.done:
		return nil, net.ErrClosed
	}
}

func (l *connListener) Close() error {
	l.once.Do(func() { close(l.done) })
	return nil
}

func (l *connListener) Addr() net.Addr { return l.addr }

// bufferedConn is a connection whose first bytes were already read into
// a buffer while deciding what it was.
type bufferedConn struct {
	net.Conn
	r *bufio.Reader
}

func (c *bufferedConn) Read(p []byte) (int, error) { return c.r.Read(p) }
