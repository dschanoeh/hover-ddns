package publicip

import (
	"errors"
	"io/ioutil"
	"net"
	"net/http"
	"strconv"
	"time"

	log "github.com/sirupsen/logrus"
)

const (
	url          = "https://api.ipify.org?format=text"
	urlV6        = "https://api6.ipify.org?format=text"
	numOfRetries = 3
	timeout      = 1000
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
	var resp *http.Response

	for i := 0; i < numOfRetries; i++ {
		ret, err := r.getResponse()

		if ret != nil && ret.StatusCode == http.StatusOK {
			resp = ret
			break
		} else {
			log.Warn("Request failed: ", err)
			time.Sleep(timeout * time.Millisecond)
		}
	}

	if resp == nil {
		return nil, errors.New("Was not able to get a valid response")
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

func (r *IpifyLookupProvider) GetPublicIPv6() (net.IP, error) {
	return nil, errors.New("Provider doesn't support IPv6 yet")
}

func (r *IpifyLookupProvider) getResponse() (*http.Response, error) {
	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, errors.New("Received status code " + strconv.Itoa(resp.StatusCode))
	}

	return resp, nil
}
