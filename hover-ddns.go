package main

import (
	"errors"
	"flag"
	"fmt"
	"io/ioutil"
	"net"
	"os"
	"os/signal"
	"syscall"

	"github.com/dschanoeh/hover-ddns/hover"
	"github.com/dschanoeh/hover-ddns/publicip"
	"github.com/miekg/dns"
	"github.com/robfig/cron/v3"
	log "github.com/sirupsen/logrus"
	"gopkg.in/yaml.v2"
)

type Config struct {
	Username         string
	Password         string
	Domains          []DomainConfig                `yaml:"domains"`
	DisableV4        bool                          `yaml:"disable_ipv4"`
	DisableV6        bool                          `yaml:"disable_ipv6"`
	ForceUpdate      bool                          `yaml:"force_update"`
	PublicIPProvider publicip.LookupProviderConfig `yaml:"public_ip_provider"`
	DNSServer        string                        `yaml:"dns_server"`
	CronExpression   string                        `yaml:"cron_expression"`
}

type DomainConfig struct {
	DomainName string   `yaml:"domain_name"`
	Hosts      []string `yaml:"hosts"`
}

var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
	builtBy = "unknown"

	cronScheduler = cron.New()
)

func main() {
	config := Config{}
	var verbose = flag.Bool("verbose", false, "Turns on verbose information on the update process. Otherwise, only errors cause output.")
	var debug = flag.Bool("debug", false, "Turns on debug information")
	var dryRun = flag.Bool("dry-run", false, "Perform lookups but don't actually update the DNS info. Returns after a single check.")
	var configFile = flag.String("config", "", "Config file")
	var manualV4 = flag.String("manual-ipv4", "", "Specify the IP address to be submitted instead of looking it up")
	var manualV6 = flag.String("manual-ipv6", "", "Specify the IP address to be submitted instead of looking it up")
	var versionFlag = flag.Bool("version", false, "Prints version information of the hover-ddns binary")
	var onlyValidateConfig = flag.String("validate-config", "", "Only check if the provided config file is valid")

	flag.Parse()

	if *versionFlag {
		fmt.Printf("hover-ddns version %s, commit %s, built at %s by %s\n", version, commit, date, builtBy)
		os.Exit(0)
	}

	if *verbose {
		log.SetLevel(log.InfoLevel)
	} else if *debug {
		log.SetLevel(log.DebugLevel)
	} else {
		log.SetLevel(log.ErrorLevel)
	}

	if *onlyValidateConfig != "" {
		err := loadConfig(*onlyValidateConfig, &config)
		if err != nil {
			log.Error("Could not load config file: ", err)
			os.Exit(1)
		}
		if !validateConfig(&config) {
			os.Exit(1)
		}
		os.Exit(0)
	}

	if *configFile == "" {
		log.Error("Please provide a config file to read")
		flag.Usage()
		os.Exit(1)
	}

	err := loadConfig(*configFile, &config)
	if err != nil {
		log.Error("Could not load config file: ", err)
		os.Exit(1)
	}
	if !validateConfig(&config) {
		os.Exit(1)
	}

	var provider publicip.LookupProvider
	provider, err = publicip.NewLookupProvider(&config.PublicIPProvider)
	if err != nil {
		log.Error("Could not configure public ip provider: ", err)
		os.Exit(1)
	}

	// When a dry run is requested, scheduling will be ignored and a single
	// run will be executed immediately.
	if *dryRun {
		run(&config, provider, dryRun, manualV4, manualV6)
		return
	}

	// Schedule periodic calls
	executeFunction := func() {
		run(&config, provider, dryRun, manualV4, manualV6)
	}
	_, err = cronScheduler.AddFunc(config.CronExpression, executeFunction)
	if err != nil {
		log.Error("Was not able to schedule periodic execution: ", err)
		os.Exit(1)
	}
	cronScheduler.Start()

	// We'll wait here until we receive a signal
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	sig := <-c
	log.Warn("Received signal " + sig.String())
	cronScheduler.Stop()
	os.Exit(0)
}

func run(config *Config, provider publicip.LookupProvider, dryRun *bool, manualV4 *string, manualV6 *string) {
	var auth *hover.HoverAuth
	var err error

	publicV4, publicV6 := determinePublicIPs(config, provider, manualV4, manualV6)

	for _, domain := range config.Domains {
		for _, hostName := range domain.Hosts {
			log.Infof("--- Processing host %s.%s ---", hostName, domain.DomainName)
			v4, v6 := hostNeedsUpdating(domain.DomainName, hostName, publicV4, publicV6, config)

			if !*dryRun {
				// Attempt hover login when the first entry that requires updating is discovered
				if (v4 != nil || v6 != nil) && auth == nil {
					auth, err = hover.Login(config.Username, config.Password)
					if err != nil {
						log.Error("Could not log in: ", err)
						return
					}
					log.Debug("AuthCookie [" + auth.AuthCookie.Name + "]: " + auth.AuthCookie.Value)
					log.Debug("SessionCookie [" + auth.SessionCookie.Name + "]: " + auth.SessionCookie.Value)
				}

				if !(v4 == nil && v6 == nil) {
					err := hover.Update(auth, domain.DomainName, hostName, v4, v6)
					if err != nil {
						log.Error("Was not able to update hover records: ", err)
						return
					}
				}
			}
		}
	}

}

// determinePublicIPs tries to determine the current IPv4 and IPv6 addresses. If this fails or one of the versions
// is deactivated, nil is returned instead.
func determinePublicIPs(config *Config, provider publicip.LookupProvider, manualV4 *string, manualV6 *string) (net.IP, net.IP) {
	var publicV4 net.IP
	var publicV6 net.IP
	var err error

	if !config.DisableV4 {
		if *manualV4 == "" {
			log.Info("Getting public IPv4...")
			publicV4, err = provider.GetPublicIP()

			if err != nil {
				log.Warn("Failed to get public ip: ", err)
				publicV4 = nil
			}

			log.Info("Received public IP " + publicV4.String())
		} else {
			publicV4 = net.ParseIP(*manualV4)
			log.Info("Using manually provied public IPv4 " + *manualV4)

			if publicV4 == nil {
				log.Error("Provided IP '" + *manualV4 + "' is not a valid IP address - ignoring.")
			}
		}
	} else {
		publicV4 = nil
	}

	if !config.DisableV6 {
		if *manualV6 == "" {
			log.Info("Getting public IPv6...")
			publicV6, err = provider.GetPublicIPv6()

			if err != nil {
				log.Warn("Failed to get public ip: ", err)
				publicV6 = nil
			}

			log.Info("Received public IP " + publicV6.String())
		} else {
			publicV6 = net.ParseIP(*manualV6)
			log.Info("Using manually provied public IPv6 " + *manualV6)

			if publicV6 == nil {
				log.Error("Provided IP '" + *manualV6 + "' is not a valid IP address - ignoring.")
			}
		}
	} else {
		publicV6 = nil
	}

	return publicV4, publicV6
}

// hostNeedsUpdating determines if the records for the given host need updating by comparing the provided IPs with
// a DNS lookup. nil is returned for IP address types that don't need updating.
func hostNeedsUpdating(domain string, hostName string, publicV4 net.IP, publicV6 net.IP, config *Config) (net.IP, net.IP) {
	if publicV4 != nil {
		log.Info("Resolving current IPv4...")
		currentV4, err := performDNSLookup(hostName+"."+domain, config.DNSServer, dns.TypeA)
		if err != nil {
			log.Warn("Failed to resolve the current IPv4: ", err)
		}
		if currentV4 != nil {
			log.Info("Received current IPv4 " + currentV4.String())
		}

		if currentV4 != nil && publicV4 != nil && currentV4.Equal(publicV4) {
			if !config.ForceUpdate {
				log.Info("v4 DNS entry already up to date - nothing to do.")
				publicV4 = nil
			} else {
				log.Info("v4 DNS entry already up to date, but update forced...")
			}
		} else {
			log.Info("v4 IPs differ - update required...")
		}
	}

	if publicV6 != nil {
		log.Info("Resolving current IPv6...")
		currentV6, err := performDNSLookup(hostName+"."+domain, config.DNSServer, dns.TypeAAAA)
		if err != nil {
			log.Warn("Failed to resolve the current IPv6: ", err)
		}
		if currentV6 != nil {
			log.Info("Received current IPv6 " + currentV6.String())
		}

		if currentV6 != nil && publicV6 != nil && currentV6.Equal(publicV6) {
			if !config.ForceUpdate {
				log.Info("v6 DNS entry already up to date - nothing to do.")
				publicV6 = nil
			} else {
				log.Info("v6 DNS entry already up to date, but update forced...")
			}
		} else {
			log.Info("v6 IPs differ - update required...")
		}
	}

	return publicV4, publicV6
}

func loadConfig(filename string, config *Config) error {
	yamlFile, err := ioutil.ReadFile(filename)
	if err != nil {
		return err
	}

	err = yaml.Unmarshal(yamlFile, &config)
	if err != nil {
		return err
	}

	return nil
}

func validateConfig(config *Config) bool {
	if config.DNSServer == "" {
		log.Error("Invalid config: A DNS server must be provided")
		return false
	}

	if len(config.Domains) == 0 {
		log.Error("Invalid config: No domain configuration was provided")
		return false
	}

	for _, d := range config.Domains {
		if d.DomainName == "" {
			log.Error("Invalid config: A domain name must be provided")
			return false
		}

		if len(d.Hosts) == 0 {
			log.Error("Invalid config: At least one host name must be provided")
			return false
		}
	}

	if config.Password == "" {
		log.Error("Invalid config: A password must be provided")
		return false
	}

	if config.Username == "" {
		log.Error("Invalid config: A user name must be provided")
		return false
	}

	if config.PublicIPProvider.Service == "" {
		log.Error("Invalid config: A public IP service must be selected")
		return false
	}

	if config.PublicIPProvider.Service == "local-interface" && config.PublicIPProvider.InterfaceName == "" {
		log.Error("Invalid config: When selecting the local-interface provider, an interface name must be provided")
		return false
	}

	return true
}

func performDNSLookup(hostname string, dnsServer string, dnsType uint16) (net.IP, error) {
	client := dns.Client{}
	message := dns.Msg{}
	message.SetQuestion(hostname+".", dnsType)

	res, _, err := client.Exchange(&message, dnsServer)
	if res == nil {
		return nil, err
	}

	if res.Rcode != dns.RcodeSuccess {
		return nil, errors.New("invalid DNS answer")
	}

	if len(res.Answer) == 0 {
		return nil, errors.New("didn't get any results for the query")
	}

	if len(res.Answer) > 1 {
		log.Warn("Received more than one IPs - just returning the first one")
	}

	record := res.Answer[0]
	switch dnsType {
	case dns.TypeA:
		aRecord := record.(*dns.A)
		return aRecord.A, nil
	case dns.TypeAAAA:
		aRecord := record.(*dns.AAAA)
		return aRecord.AAAA, nil
	}

	return nil, errors.New("no valid record type selected")
}
