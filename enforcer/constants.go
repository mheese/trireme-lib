package enforcer

import "time"

const (
	// TCPAuthenticationOptionBaseLen specifies the length of base TCP Authentication Option packet
	TCPAuthenticationOptionBaseLen = 4
	// TCPAuthenticationOptionAckLen specifies the length of TCP Authentication Option in the ack packet
	TCPAuthenticationOptionAckLen = 20
	// PortNumberLabelString is the label to use for port numbers
	PortNumberLabelString = "$sys:port"
	// TransmitterLabel is the name of the label used to identify the Transmitter Context
	TransmitterLabel = "AporetoContextID"
	// DefaultNetwork to be used
	DefaultNetwork = "0.0.0.0/0"

	// DefaultExternalIPTimeout is the default used for the cache for External IPTimeout.
	DefaultExternalIPTimeout = time.Second * 600

	// ExternalServiceResponseTimeOut is the timeout used waiting for a response from an external
	// service before removing it from the cahce
	ExternalServiceResponseTimeOut = time.Second * 3
)
