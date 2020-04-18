package publicip

import "net"

// Resolver is an interface for a provider that can resolve the current public IP address
type Resolver interface {
	GetPublicIP() (net.IP, error)
}
