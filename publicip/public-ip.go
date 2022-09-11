package publicip

import (
	"errors"
	"net"
)

// LookupProviderConfig is a configuration from which a lookup provider can be selected and configured
type LookupProviderConfig struct {
	Service       string
	InterfaceName string `yaml:"interface_name"`
}

// LookupProvider is an interface for a provider that can resolve the current public IP address
type LookupProvider interface {
	GetPublicIP() (net.IP, error)
	GetPublicIPv6() (net.IP, error)
}

// NewLookupProvider creates a new lookup provider from a given configuration
func NewLookupProvider(config *LookupProviderConfig) (LookupProvider, error) {
	switch config.Service {
	case "ipify":
		return NewIpifyLookupProvider(), nil
	case "amazon":
		return NewAmazonLookupProvider(), nil
	case "icanhazip":
		return NewIcanhazipLookupProvider(), nil
	case "local_interface":
		if config.InterfaceName == "" {
			return nil, errors.New("for the local_interface service, an interface_name must be provided")
		}
		return NewLocalInterfaceLookupProvider(config.InterfaceName), nil
	default:
		return nil, errors.New("'" + config.Service + "' is not a valid service")
	}
}
