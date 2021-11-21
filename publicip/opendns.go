package publicip

import (
	"errors"
	"net"

	"github.com/miekg/dns"
)

const (
	dnsServer   = "208.67.222.222:53"    // resolver1.opendns.com
	dnsServerV6 = "[2620:119:35::35]:53" // resolver1.opendns.com
	dnsTarget   = "myip.opendns.com"
)

// OpenDNSLookupProvider is a lookup provider using the OpenDNS DNS interface
type OpenDNSLookupProvider struct {
	client    dns.Client
	message   dns.Msg
	messageV6 dns.Msg
}

// NewOpenDNSLookupProvider creates a new OpenDNSLookupProvider
func NewOpenDNSLookupProvider() *OpenDNSLookupProvider {
	r := OpenDNSLookupProvider{}

	r.client = dns.Client{}
	r.message.SetQuestion(dnsTarget+".", dns.TypeA)
	r.messageV6.SetQuestion(dnsTarget+".", dns.TypeAAAA)

	return &r
}

// GetPublicIP returns the current public IP or nil if an error occured
func (r *OpenDNSLookupProvider) GetPublicIP() (net.IP, error) {
	ip := net.IP{}
	res, _, err := r.client.Exchange(&r.message, dnsServer)
	if err != nil {
		return nil, err
	}

	if len(res.Answer) == 0 {
		return nil, errors.New("didn't get any results for the query")
	}

	for _, ans := range res.Answer {
		Arecord := ans.(*dns.A)
		ip = Arecord.A
	}

	return ip, nil
}

// GetPublicIPV6 returns the current public IPv6 address or nil if an error occured
func (r *OpenDNSLookupProvider) GetPublicIPv6() (net.IP, error) {
	ip := net.IP{}
	res, _, err := r.client.Exchange(&r.messageV6, dnsServerV6)
	if err != nil {
		return nil, err
	}

	if len(res.Answer) == 0 {
		return nil, errors.New("didn't get any results for the query")
	}

	for _, ans := range res.Answer {
		Arecord := ans.(*dns.AAAA)
		ip = Arecord.AAAA
	}

	return ip, nil
}
