package publicip

import (
	"errors"
	"io/ioutil"
	"net"
	"net/http"
)

const (
	url = "https://api.ipify.org?format=text"
)

// IpifyLookupProvider is a public IP lookup provider using the ipify.org API
type IpifyLookupProvider struct {
}

// NewIpifyLookupProvider creates a new Ipify lookup provider
func NewIpifyLookupProvider() *IpifyLookupProvider {
	return &IpifyLookupProvider{}
}

// GetPublicIP returns the current public IP or nil if an error occured
func (r *IpifyLookupProvider) GetPublicIP() (net.IP, error) {
	ip := net.IP{}
	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, errors.New("Received status code " + string(resp.StatusCode))
	}

	defer resp.Body.Close()

	ipBytes, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	ipString := string(ipBytes)

	ip = net.ParseIP(ipString)
	if ip == nil {
		return nil, errors.New("'" + ipString + "' is not a valid IP address.")
	}

	return ip, nil
}
