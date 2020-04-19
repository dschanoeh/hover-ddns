package publicip

import (
	"errors"
	"net"

	log "github.com/sirupsen/logrus"
)

// LocalInterfaceLookupProvider is a lookup provider that will extract the public IP address from
// a given interface
type LocalInterfaceLookupProvider struct {
	interfaceName string
}

// NewLocalInterfaceLookupProvider creates a new lookup provider
func NewLocalInterfaceLookupProvider(interfaceName string) *LocalInterfaceLookupProvider {
	r := LocalInterfaceLookupProvider{interfaceName}
	return &r
}

// GetPublicIP returns the current public IP or nil if an error occured
func (r *LocalInterfaceLookupProvider) GetPublicIP() (net.IP, error) {
	ip := net.IP{}
	found := false

	ifaces, err := net.Interfaces()
	if err != nil {
		return nil, err
	}
	for _, i := range ifaces {
		if i.Name == r.interfaceName {
			addrs, err := i.Addrs()
			if err != nil {
				return nil, err
			}
			for _, addr := range addrs {
				switch v := addr.(type) {
				case *net.IPNet:
					if v.IP.IsGlobalUnicast() && len(v.IP) == net.IPv4len {
						ip = v.IP
						found = true
						break
					}
					continue
				default:
					log.Warn("Received an address type that this code doesn't handle")
				}
			}
		}
	}

	if found {
		return ip, nil
	}
	return nil, errors.New("Was not able to find IP address on interface '" + r.interfaceName + "'")
}
