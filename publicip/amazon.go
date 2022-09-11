package publicip

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
)

const (
	AmazonCheckIpAddress = "https://checkip.amazonaws.com"
)

// AmazonLookupProvider is a public IP lookup provider using the checkip.amazonaws.com API
type AmazonLookupProvider struct {
	zeroDialer   net.Dialer
	httpClientV4 *http.Client
	httpClientV6 *http.Client
}

// AmazonLookupProvider creates a new Amazon lookup provider
func NewAmazonLookupProvider() *AmazonLookupProvider {
	provider := AmazonLookupProvider{}

	transportV4 := http.DefaultTransport.(*http.Transport).Clone()
	transportV4.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		return provider.zeroDialer.DialContext(ctx, "tcp4", addr)
	}
	provider.httpClientV4 = &http.Client{
		Transport: transportV4,
	}

	transportV6 := http.DefaultTransport.(*http.Transport).Clone()
	transportV6.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		return provider.zeroDialer.DialContext(ctx, "tcp6", addr)
	}
	provider.httpClientV6 = &http.Client{
		Transport: transportV6,
	}

	return &provider
}

// GetPublicIP returns the current public IP or nil if an error occurred
func (p *AmazonLookupProvider) GetPublicIP() (net.IP, error) {
	return p.getAddress(false)
}

func (p *AmazonLookupProvider) GetPublicIPv6() (net.IP, error) {
	return nil, errors.New("provider doesn't support IPv6 yet")
}

func (p *AmazonLookupProvider) getAddress(v6 bool) (net.IP, error) {
	var client *http.Client
	if v6 {
		client = p.httpClientV6
	} else {
		client = p.httpClientV4
	}

	resp, err := client.Get(AmazonCheckIpAddress)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, errors.New("received status code " + strconv.Itoa(resp.StatusCode))
	}

	defer resp.Body.Close()

	ipBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	ipString := strings.TrimSpace((string)(ipBytes))

	ip := net.ParseIP(ipString)
	if ip == nil {
		return nil, errors.New("'" + ipString + "' is not a valid IP address.")
	}

	return ip, nil
}
