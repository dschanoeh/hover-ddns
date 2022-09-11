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

// GetPublicIP returns the current public IP or nil if an error occurred
func (r *LocalInterfaceLookupProvider) GetPublicIP() (net.IP, error) {
	return r.getAddress(false)
}

func (r *LocalInterfaceLookupProvider) GetPublicIPv6() (net.IP, error) {
	return r.getAddress(true)
}

func (r *LocalInterfaceLookupProvider) getAddress(v6 bool) (net.IP, error) {
	ip := net.IP{}
	found := false

	interfaces, err := net.Interfaces()
	if err != nil {
		return nil, err
	}
	for _, i := range interfaces {
		if i.Name == r.interfaceName {
			log.Debug("Found interface " + i.Name)
			addrs, err := i.Addrs()
			if err != nil {
				return nil, err
			}
			for _, addr := range addrs {
				log.Debug("Looking at address " + addr.String())
				switch v := addr.(type) {
				case *net.IPNet:
					if v6 && (v.IP.IsGlobalUnicast() && len(v.IP) == net.IPv6len) ||
						!v6 && (v.IP.IsGlobalUnicast() && len(v.IP) == net.IPv4len) {
						log.Debug("This is our address!")
						ip = v.IP
						found = true
						break
					}
					continue
				default:
					log.Warn("Skipping address type that hover-ddns doesn't understand")
				}
				if found {
					break
				}
			}
		}
	}

	if !found {
		return nil, errors.New("was not able to find IP address on interface '" + r.interfaceName + "'")
	}

	return ip, nil
}
