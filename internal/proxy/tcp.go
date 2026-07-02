package proxy

import (
	"context"
	"crypto/tls"
	"errors"
	"io"
	"log/slog"
	"net"
	"runtime/debug"
	"sync"
	"sync/atomic"
	"time"

	"github.com/colebrumley/tlspxy/internal/metrics"
)

// Counter counts the proxy's traffic
type Counter struct {
	to, from uint64
}

// To adds bytes to the to counter
func (p *Counter) To(b uint64) {
	atomic.AddUint64(&p.to, b)
}

// From adds bytes to the from counter
func (p *Counter) From(b uint64) {
	atomic.AddUint64(&p.from, b)
}

// Total returns the count
func (p *Counter) Total() (to, from uint64) {
	to = atomic.LoadUint64(&p.to)
	from = atomic.LoadUint64(&p.from)
	return
}

// InterruptHandler writes info when an os signal is encountered.
func (p *Counter) InterruptHandler() {
	to, from := p.Total()
	slog.Info("Proxy shutting down", "component", "tcp", "sent", to, "received", from)
}

// TCPProxy is the wrapper object for a proxy connection. It tracks the amount
// of data sent and received, local and remote server settings, TLS config,
// and any connection errors.
type TCPProxy struct {
	Counter                *Counter
	ServerAddr, RemoteAddr string
	ServerConn, RemoteConn net.Conn
	RemoteTLSConf          *tls.Config
	ErrorSignal            chan bool
	Ctx                    context.Context
	connCtx                context.Context
	CloseOnce              sync.Once
	ConnID                 int
	ShowContent            bool
	Log                    *slog.Logger
	ReadTimeout            time.Duration
	WriteTimeout           time.Duration
	IdleTimeout            time.Duration
	// HandshakeTimeout bounds the client TLS handshake when the server-side
	// connection is a *tls.Conn. Defaults to 10s when zero.
	HandshakeTimeout time.Duration
	// DialTimeout bounds backend connection establishment (TCP dial, plus TLS
	// handshake for TLS backends). Defaults to 30s when zero to preserve the
	// historical behavior for direct constructors (e.g. tests); main.go wires
	// the configured remote.timeouts.dial value (default 10s).
	DialTimeout time.Duration
	// ProxyProto, when set to "v1" or "v2", causes a HAProxy PROXY protocol
	// header carrying the real client source/destination to be written as the
	// first bytes on each backend connection.
	ProxyProto string
}

func (p *TCPProxy) err(s string, err error) {
	// io.EOF and net.ErrClosed are benign: EOF is a normal peer close, and
	// ErrClosed is what the surviving pipe goroutine sees once the connection
	// is torn down locally during teardown. Neither should be logged or
	// counted as a real error.
	if err != io.EOF && !errors.Is(err, net.ErrClosed) {
		p.Log.Warn(s, "error", err)
		if metrics.Enabled.Load() {
			metrics.ErrorsTotal.WithLabelValues("connection").Inc()
		}
	}
	p.CloseOnce.Do(func() { p.ErrorSignal <- true })
}

// Start begins proxying between the server and remote connections.
func (p *TCPProxy) Start() {
	if metrics.Enabled.Load() {
		metrics.ConnectionsActive.Inc()
		metrics.ConnectionsTotal.Inc()
		defer metrics.ConnectionsActive.Dec()
	}
	defer func() {
		if r := recover(); r != nil {
			p.Log.Error("Panic in connection handler", "error", r, "stack", string(debug.Stack()))
			p.CloseOnce.Do(func() { p.ErrorSignal <- true })
		}
	}()
	defer p.ServerConn.Close()

	// Default to background context if none was set.
	if p.Ctx == nil {
		p.Ctx = context.Background()
	}

	// Create a per-connection context so the watcher goroutine is always
	// cleaned up when Start() returns, even for normal connection closure.
	connCtx, connCancel := context.WithCancel(p.Ctx)
	p.connCtx = connCtx
	defer connCancel()

	// If the server-side connection is a TLS connection, complete the client
	// handshake before dialing the backend. Go's tls.Listener handshakes
	// lazily on first read, so without this gate any bare TCP connection (port
	// scanners, health probes) would consume a backend connection. Only after a
	// successful handshake do we dial. Non-TLS listeners skip this so
	// server-speaks-first protocols (SMTP, MySQL) still work.
	if tlsConn, ok := p.ServerConn.(*tls.Conn); ok {
		hsTimeout := p.HandshakeTimeout
		if hsTimeout <= 0 {
			hsTimeout = 10 * time.Second
		}
		hsCtx, hsCancel := context.WithTimeout(p.Ctx, hsTimeout)
		err := tlsConn.HandshakeContext(hsCtx)
		hsCancel()
		if err != nil {
			// Logged at Info, not Warn/Error: failed handshakes are routine
			// (port scanners, bare TCP probes) and must not spam error logs or
			// consume a backend connection.
			p.Log.Info("Client TLS handshake failed, closing without dialing backend",
				"src", p.ServerConn.RemoteAddr().String(),
				"error", err,
			)
			p.CloseOnce.Do(func() { p.ErrorSignal <- true })
			return
		}
	}

	var (
		rConn net.Conn
		err   error
		isTLS bool
	)

	dialTimeout := p.DialTimeout
	if dialTimeout <= 0 {
		dialTimeout = 30 * time.Second
	}
	dialCtx, dialCancel := context.WithTimeout(p.Ctx, dialTimeout)
	defer dialCancel()

	if p.RemoteTLSConf != nil {
		isTLS = true
		p.Log.Debug("Dialing remote")
		dialer := &tls.Dialer{
			NetDialer: &net.Dialer{},
			Config:    p.RemoteTLSConf,
		}
		rConn, err = dialer.DialContext(dialCtx, "tcp", p.RemoteAddr)
	} else {
		isTLS = false
		dialer := &net.Dialer{}
		rConn, err = dialer.DialContext(dialCtx, "tcp", p.RemoteAddr)
	}
	if err != nil {
		p.err("Remote connection failed", err)
		return
	}
	p.RemoteConn = rConn
	defer p.RemoteConn.Close()

	// Send the PROXY protocol header (if configured) as the first bytes on the
	// backend connection, carrying the real client source and the local
	// destination address.
	if p.ProxyProto != "" {
		hdr, herr := ProxyHeader(p.ProxyProto, p.ServerConn.RemoteAddr(), p.ServerConn.LocalAddr())
		if herr != nil {
			p.err("Failed to build PROXY protocol header", herr)
			return
		}
		if len(hdr) > 0 {
			if p.WriteTimeout > 0 {
				p.RemoteConn.SetWriteDeadline(time.Now().Add(p.WriteTimeout))
			}
			if _, werr := p.RemoteConn.Write(hdr); werr != nil {
				p.err("Failed to write PROXY protocol header", werr)
				return
			}
			if p.WriteTimeout > 0 {
				p.RemoteConn.SetWriteDeadline(time.Time{})
			}
		}
	}

	p.Log.Info("Connection opened",
		"src", p.ServerConn.RemoteAddr().String(),
		"dst", p.RemoteConn.RemoteAddr().String(),
		"tls", isTLS,
	)

	// Watch for context cancellation and close both connections to
	// unblock any blocking Read/Write calls in the pipe goroutines.
	// Uses connCtx so this goroutine exits when Start() returns.
	go func() {
		select {
		case <-connCtx.Done():
			// Only close connections if the parent context was cancelled
			// (e.g. process shutdown). If connCtx was cancelled because
			// Start() is returning normally, the defers handle cleanup.
			if p.Ctx.Err() != nil {
				p.Log.Info("Context cancelled, closing connections")
				p.ServerConn.Close()
				p.RemoteConn.Close()
			}
		case <-p.ErrorSignal:
			// ErrorSignal already fired; the main select below will also
			// receive it (buffered channel) or has already handled it.
			// Re-send so the main select below can still consume it.
			select {
			case p.ErrorSignal <- true:
			default:
			}
			return
		}
	}()

	go p.Pipe(p.ServerConn, p.RemoteConn)
	go p.Pipe(p.RemoteConn, p.ServerConn)

	select {
	case <-p.ErrorSignal:
	case <-p.Ctx.Done():
	}
	p.Log.Info("Connection closed")
}

// Pipe copies data between src and dst, tracking bytes transferred.
func (p *TCPProxy) Pipe(src, dst net.Conn) {
	defer func() {
		if r := recover(); r != nil {
			p.Log.Error("Panic in pipe", "error", r, "stack", string(debug.Stack()))
			p.CloseOnce.Do(func() { p.ErrorSignal <- true })
		}
	}()
	islocal := src == p.ServerConn
	direction := "recv"
	if islocal {
		direction = "send"
	}

	// Consult the per-connection context so that a local teardown (connCancel
	// fired when Start() returns) makes the surviving pipe goroutine exit
	// quietly instead of misclassifying net.ErrClosed as a real error. Fall
	// back to the root context if Pipe is invoked without Start().
	ctx := p.connCtx
	if ctx == nil {
		ctx = p.Ctx
	}

	buff := make([]byte, 0xffff)
	for {
		// Check for context cancellation before each read.
		if ctx != nil && ctx.Err() != nil {
			return
		}

		// Set read deadline: use IdleTimeout for reads (time waiting for data),
		// or ReadTimeout if set and IdleTimeout is not.
		if p.IdleTimeout > 0 {
			src.SetReadDeadline(time.Now().Add(p.IdleTimeout))
		} else if p.ReadTimeout > 0 {
			src.SetReadDeadline(time.Now().Add(p.ReadTimeout))
		}

		n, err := src.Read(buff)
		if err != nil {
			// If the context was cancelled, the read error is expected
			// from the connection being closed. Don't log it as an error.
			if ctx != nil && ctx.Err() != nil {
				return
			}
			// Handle timeout errors gracefully at Info level.
			var netErr net.Error
			if errors.As(err, &netErr) && netErr.Timeout() {
				p.Log.Info("Connection timed out", "direction", direction, "side", "read")
				p.CloseOnce.Do(func() { p.ErrorSignal <- true })
				return
			}
			p.err("Read failed", err)
			return
		}
		b := buff[:n]

		if p.ShowContent {
			p.Log.Debug("Data transferred", "direction", direction, "bytes", n, "data", string(b))
		} else {
			p.Log.Debug("Data transferred", "direction", direction, "bytes", n)
		}

		// Set write deadline if configured.
		if p.WriteTimeout > 0 {
			dst.SetWriteDeadline(time.Now().Add(p.WriteTimeout))
		}

		written := 0
		for written < len(b) {
			n, err = dst.Write(b[written:])
			written += n
			if n == 0 && err == nil {
				p.err("Write failed", io.ErrNoProgress)
				return
			}
			if err != nil {
				if ctx != nil && ctx.Err() != nil {
					return
				}
				// Handle timeout errors gracefully at Info level.
				var netErr net.Error
				if errors.As(err, &netErr) && netErr.Timeout() {
					p.Log.Info("Connection timed out", "direction", direction, "side", "write")
					p.CloseOnce.Do(func() { p.ErrorSignal <- true })
					return
				}
				p.err("Write failed", err)
				return
			}
		}
		if islocal {
			p.Counter.To(uint64(written))
			if metrics.Enabled.Load() {
				metrics.BytesSentTotal.Add(float64(written))
			}
		} else {
			p.Counter.From(uint64(written))
			if metrics.Enabled.Load() {
				metrics.BytesReceivedTotal.Add(float64(written))
			}
		}
	}
}

// SingleJoiningSlash joins two URL path segments with exactly one slash between them.
func SingleJoiningSlash(a, b string) string {
	aslash := len(a) > 0 && a[len(a)-1] == '/'
	bslash := len(b) > 0 && b[0] == '/'
	switch {
	case aslash && bslash:
		return a + b[1:]
	case !aslash && !bslash:
		return a + "/" + b
	}
	return a + b
}
