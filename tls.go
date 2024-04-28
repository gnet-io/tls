package gnettls

import (
	"bytes"
	"errors"
	"io"
	"net"
	"runtime/debug"
	"time"

	"github.com/gnet-io/tls/tls"
	"github.com/panjf2000/gnet/v2"
	"github.com/panjf2000/gnet/v2/pkg/logging"
	bbPool "github.com/panjf2000/gnet/v2/pkg/pool/bytebuffer"
)

type tlsConn struct {
	raw           gnet.Conn
	rawTLSConn    *tls.Conn
	inboundBuffer *bytes.Buffer
	ctx           interface{}
}

func (c *tlsConn) Read(p []byte) (n int, err error) {
	return c.inboundBuffer.Read(p)
}

func (c *tlsConn) WriteTo(w io.Writer) (n int64, err error) {
	return c.inboundBuffer.WriteTo(w)
}

func (c *tlsConn) Next(n int) (buf []byte, err error) {
	if n < 0 || n > c.inboundBuffer.Len() {
		n = c.inboundBuffer.Len()
	}
	return c.inboundBuffer.Next(n), nil
}

func (c *tlsConn) Peek(n int) (buf []byte, err error) {
	if c.inboundBuffer.Len() < n {
		return nil, io.ErrShortBuffer
	}
	return c.inboundBuffer.Bytes()[:n], nil
}

func (c *tlsConn) Discard(n int) (discarded int, err error) {
	r, err := io.CopyN(io.Discard, c.inboundBuffer, int64(n))
	return int(r), err
}

func (c *tlsConn) InboundBuffered() (n int) {
	return c.inboundBuffer.Len()
}

func (c *tlsConn) Write(p []byte) (n int, err error) {
	return c.rawTLSConn.Write(p)
}

func (c *tlsConn) ReadFrom(r io.Reader) (n int64, err error) {
	return c.inboundBuffer.ReadFrom(r)
}

func (c *tlsConn) Writev(bs [][]byte) (n int, err error) {
	bb := bbPool.Get()
	defer bbPool.Put(bb)
	for i := range bs {
		_, _ = bb.Write(bs[i])
	}
	return c.Write(bb.Bytes())
}

func (c *tlsConn) Flush() (err error) {
	return c.raw.Flush()
}

func (c *tlsConn) OutboundBuffered() (n int) {
	return c.raw.OutboundBuffered()
}

func (c *tlsConn) AsyncWrite(buf []byte, callback gnet.AsyncCallback) (err error) {
	_, err = c.Write(buf)
	return callback(c, err)
}

func (c *tlsConn) AsyncWritev(bs [][]byte, callback gnet.AsyncCallback) (err error) {
	_, err = c.Writev(bs)
	return callback(c, err)
}

func (c *tlsConn) Fd() int {
	return c.raw.Fd()
}

func (c *tlsConn) Dup() (int, error) {
	return c.raw.Dup()
}

func (c *tlsConn) SetReadBuffer(bytes int) error {
	return c.raw.SetReadBuffer(bytes)
}

func (c *tlsConn) SetWriteBuffer(bytes int) error {
	return c.raw.SetWriteBuffer(bytes)
}

func (c *tlsConn) SetLinger(sec int) error {
	return c.raw.SetLinger(sec)
}

func (c *tlsConn) SetKeepAlivePeriod(d time.Duration) error {
	return c.raw.SetKeepAlivePeriod(d)
}

func (c *tlsConn) SetNoDelay(noDelay bool) error {
	return c.raw.SetNoDelay(noDelay)
}

func (c *tlsConn) Context() (ctx interface{}) {
	return c.ctx
}

func (c *tlsConn) SetContext(ctx interface{}) {
	c.ctx = ctx
}

func (c *tlsConn) LocalAddr() (addr net.Addr) {
	return c.raw.LocalAddr()
}

func (c *tlsConn) RemoteAddr() (addr net.Addr) {
	return c.raw.RemoteAddr()
}

func (c *tlsConn) Wake(callback gnet.AsyncCallback) (err error) {
	return c.raw.Wake(callback)
}

func (c *tlsConn) CloseWithCallback(callback gnet.AsyncCallback) (err error) {
	return c.raw.CloseWithCallback(callback)
}

func (c *tlsConn) Close() (err error) {
	return c.raw.Close()
}

func (c *tlsConn) SetDeadline(t time.Time) (err error) {
	return c.raw.SetDeadline(t)
}

func (c *tlsConn) SetReadDeadline(t time.Time) (err error) {
	return c.raw.SetReadDeadline(t)
}

func (c *tlsConn) SetWriteDeadline(t time.Time) (err error) {
	return c.SetWriteDeadline(t)
}

type tlsEventHandler struct {
	gnet.EventHandler
	tlsConfig *tls.Config
}

func (h *tlsEventHandler) OnOpen(c gnet.Conn) (out []byte, action gnet.Action) {
	// upgrade gnet.Conn to TLSConn
	tc := tls.Server(c, h.tlsConfig)
	c.SetContext(&tlsConn{
		raw:           c,
		rawTLSConn:    tc,
		inboundBuffer: bytes.NewBuffer(make([]byte, 0, 512)),
	})
	// The code here does not need call OnOpen now; it can be deferred until the handshake complete
	return
}

func (h *tlsEventHandler) OnTraffic(c gnet.Conn) (action gnet.Action) {
	tc := c.Context().(*tlsConn)

	// TLS handshake
	buffered := tc.raw.InboundBuffered()
	noReadCount := 0
	for !tc.rawTLSConn.HandshakeCompleted() && tc.raw.InboundBuffered() > 0 {
		if noReadCount >= 10 {
			logging.Errorf("max retry handshake inbound buffered: %d", tc.raw.InboundBuffered())
			return gnet.Close
		}
		err := tc.rawTLSConn.Handshake()
		// no data is read
		if buffered == tc.raw.InboundBuffered() {
			noReadCount++
		} else {
			noReadCount = 0
		}

		buffered = tc.raw.InboundBuffered()

		// data not enough wait for next round
		if errors.Is(err, tls.ErrNotEnough) {
			return gnet.None
		}

		if err != nil {
			logging.Error(err)
			return gnet.Close
		}

		// handshake completed to fire OnOpen
		if tc.rawTLSConn.HandshakeCompleted() {
			// fire OnOpen when handshake completed
			out, act := h.EventHandler.OnOpen(tc)
			if act != gnet.None {
				return act
			}
			if len(out) > 0 {
				if _, err := tc.Write(out); err != nil {
					return gnet.Close
				}
			}
			break
		}
	}

	if !tc.rawTLSConn.HandshakeCompleted() {
		return gnet.None
	}

	bb := bbPool.Get()
	defer bbPool.Put(bb)
	n, err := bb.ReadFrom(tc.rawTLSConn)
	if err != nil {
		// close when the error is not ErrNotEnough or EOF
		if !(errors.Is(err, io.EOF) || errors.Is(err, tls.ErrNotEnough)) {
			logging.Errorf("tls conn OnTraffic err: %v, stack: %s", err, debug.Stack())
			return gnet.Close
		}
	}

	if n > 0 {
		tc.inboundBuffer.Write(bb.Bytes())
	}

	if tc.inboundBuffer.Len() > 0 {
		return h.EventHandler.OnTraffic(tc)
	}

	return gnet.None
}
