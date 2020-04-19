package publicip

import (
	"errors"
	"log"
	"net"

	"github.com/miekg/dns"
)

const (
	dnsServer = "resolver1.opendns.com:53"
	dnsTarget = "myip.opendns.com"
)

// OpenDNSLookupProvider is a lookup provider using the OpenDNS DNS interface
type OpenDNSLookupProvider struct {
	client  dns.Client
	message dns.Msg
}

// NewOpenDNSLookupProvider creates a new OpenDNSLookupProvider
func NewOpenDNSLookupProvider() *OpenDNSLookupProvider {
	r := OpenDNSLookupProvider{}

	r.client = dns.Client{}
	r.message.SetQuestion(dnsTarget+".", dns.TypeA)

	return &r
}

// GetPublicIP returns the current public IP or nil if an error occured
func (r *OpenDNSLookupProvider) GetPublicIP() (net.IP, error) {
	ip := net.IP{}

	res, t, err := r.client.Exchange(&r.message, dnsServer)
	if err != nil {
		return nil, err
	}
	log.Printf("Took %v", t)
	if len(res.Answer) == 0 {
		return nil, errors.New("Didn't get any results for the query")
	}

	for _, ans := range res.Answer {
		Arecord := ans.(*dns.A)
		log.Printf("%s", Arecord.A)
		ip = Arecord.A

	}

	return ip, nil
}
