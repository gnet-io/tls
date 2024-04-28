package gnettls

import (
	"github.com/gnet-io/tls/tls"
	"github.com/panjf2000/gnet/v2"
)

func Run(eventHandler gnet.EventHandler, protoAddr string, tlsConfig *tls.Config, opts ...gnet.Option) error {
	if tlsConfig != nil {
		eventHandler = &tlsEventHandler{EventHandler: eventHandler, tlsConfig: tlsConfig}
	}
	return gnet.Run(eventHandler, protoAddr, opts...)
}
