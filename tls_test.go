package gnettls

import (
	"bytes"
	"fmt"
	"log"
	"testing"
	"time"

	"github.com/gnet-io/tls/tls"
	"github.com/panjf2000/gnet/v2"
	"github.com/panjf2000/gnet/v2/pkg/pool/goroutine"
)

func TestTLSServer(t *testing.T) {
	addr := fmt.Sprintf("tcp://:%d", 8443)
	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{mustLoadCertificate()},
	}
	hs := &httpsServer{
		addr: addr,
		pool: goroutine.Default(),
	}

	options := []gnet.Option{
		gnet.WithMulticore(true),
		gnet.WithTCPKeepAlive(time.Minute * 5),
		gnet.WithReusePort(true),
	}

	log.Fatal(Run(hs, hs.addr, tlsConfig, options...))
}

type httpsServer struct {
	gnet.BuiltinEventEngine

	addr string
	eng  gnet.Engine
	pool *goroutine.Pool
}

func (hs *httpsServer) OnTraffic(c gnet.Conn) (action gnet.Action) {
	// read all get http request
	// TODO decode http codec
	// TODO handling http request and response content, should decode http request for yourself
	// Must read the complete HTTP packet before responding.
	if hs.isHTTPRequestComplete(c) {
		_, _ = c.Next(-1)
		// for example hello response
		_, _ = c.Write([]byte("HTTP/1.1 200 OK\r\nContent-Type: text/plain\r\nContent-Length: 12\r\n\r\nHello world!"))
	}
	return
}

func (hs *httpsServer) isHTTPRequestComplete(c gnet.Conn) bool {
	buf, _ := c.Peek(c.InboundBuffered())
	return bytes.Contains(buf, []byte("\r\n\r\n"))
}

func (hs *httpsServer) OnClose(c gnet.Conn, err error) (action gnet.Action) {
	// logging.Infof("Closed connection on %s, error: %v", c.RemoteAddr().String(), err)
	return
}

func mustLoadCertificate() tls.Certificate {
	cert, err := tls.LoadX509KeyPair("server.crt", "server.key")
	if err != nil {
		log.Fatalf("Failed to load server certificate: %v", err)
	}
	return cert
}
