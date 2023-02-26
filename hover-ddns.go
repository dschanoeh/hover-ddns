package main

import (
	"errors"
	"flag"
	"fmt"
	"net"
	"os"
	"os/signal"
	"syscall"

	"github.com/dschanoeh/hover-ddns/hover"
	"github.com/dschanoeh/hover-ddns/publicip"
	"github.com/miekg/dns"
	"github.com/robfig/cron/v3"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
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

	loggingConfig := zap.NewProductionConfig()

	if *verbose {
		loggingConfig.Level.SetLevel(zapcore.InfoLevel)
	} else if *debug {
		loggingConfig.Level.SetLevel(zapcore.DebugLevel)
	} else {
		loggingConfig.Level.SetLevel(zapcore.ErrorLevel)
	}

	logger := zap.Must(loggingConfig.Build())
	sugaredLogger := logger.Sugar()

	if *onlyValidateConfig != "" {
		err := loadConfig(*onlyValidateConfig, &config)
		if err != nil {
			sugaredLogger.Error("Could not load config file: ", err)
			os.Exit(1)
		}
		if !validateConfig(logger, &config) {
			os.Exit(1)
		}
		os.Exit(0)
	}

	if *configFile == "" {
		sugaredLogger.Error("Please provide a config file to read")
		flag.Usage()
		os.Exit(1)
	}

	err := loadConfig(*configFile, &config)
	if err != nil {
		sugaredLogger.Error("Could not load config file: ", err)
		os.Exit(1)
	}
	if !validateConfig(logger, &config) {
		os.Exit(1)
	}

	var provider publicip.LookupProvider
	provider, err = publicip.NewLookupProvider(logger, &config.PublicIPProvider)
	if err != nil {
		sugaredLogger.Error("Could not configure public ip provider: ", err)
		os.Exit(1)
	}

	// Perform a first run immediately
	sugaredLogger.Info("Performing first update")
	run(logger, &config, provider, dryRun, manualV4, manualV6)

	// If a dry-run was requested, we're done now and can terminate
	if *dryRun {
		return
	}

	// Schedule periodic calls
	executeFunction := func() {
		run(logger, &config, provider, dryRun, manualV4, manualV6)
	}
	_, err = cronScheduler.AddFunc(config.CronExpression, executeFunction)
	if err != nil {
		sugaredLogger.Error("Was not able to schedule periodic execution: ", err)
		os.Exit(1)
	}
	cronScheduler.Start()
	logger.Info("Waiting for future scheduled updates")

	// We'll wait here until we receive a signal
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	sig := <-c
	sugaredLogger.Warn("Received signal " + sig.String())
	cronScheduler.Stop()
	os.Exit(0)
}

func run(logger *zap.Logger, config *Config, provider publicip.LookupProvider, dryRun *bool, manualV4 *string, manualV6 *string) {
	var auth *hover.HoverAuth
	var client *hover.HoverClient
	var err error
	sugaredLogger := logger.Sugar()

	publicV4, publicV6 := determinePublicIPs(logger, config, provider, manualV4, manualV6)

	for _, domain := range config.Domains {
		for _, hostName := range domain.Hosts {
			sugaredLogger.Infof("--- Processing host %s.%s ---", hostName, domain.DomainName)
			v4, v6 := hostNeedsUpdating(logger, domain.DomainName, hostName, publicV4, publicV6, config)

			if !*dryRun {
				// Attempt hover login when the first entry that requires updating is discovered
				if (v4 != nil || v6 != nil) && auth == nil {
					client = hover.NewClient(logger)
					auth, err = client.Login(config.Username, config.Password)
					if err != nil {
						sugaredLogger.Error("Could not log in: ", err)
						return
					}
					sugaredLogger.Debug("AuthCookie [" + auth.AuthCookie.Name + "]: " + auth.AuthCookie.Value)
					sugaredLogger.Debug("SessionCookie [" + auth.SessionCookie.Name + "]: " + auth.SessionCookie.Value)
				}

				if !(v4 == nil && v6 == nil) {
					err := client.Update(auth, domain.DomainName, hostName, v4, v6)
					if err != nil {
						sugaredLogger.Error("Was not able to update hover records: ", err)
						return
					}
				}
			}
		}
	}

}

// determinePublicIPs tries to determine the current IPv4 and IPv6 addresses. If this fails or one of the versions
// is deactivated, nil is returned instead.
func determinePublicIPs(logger *zap.Logger, config *Config, provider publicip.LookupProvider, manualV4 *string, manualV6 *string) (net.IP, net.IP) {
	var publicV4 net.IP
	var publicV6 net.IP
	var err error
	sugaredLogger := logger.Sugar()

	if !config.DisableV4 {
		if *manualV4 == "" {
			sugaredLogger.Info("Getting public IPv4...")
			publicV4, err = provider.GetPublicIP()

			if err != nil {
				sugaredLogger.Warn("Failed to get public ip: ", err)
				publicV4 = nil
			}

			sugaredLogger.Info("Received public IP " + publicV4.String())
		} else {
			publicV4 = net.ParseIP(*manualV4)
			sugaredLogger.Info("Using manually provied public IPv4 " + *manualV4)

			if publicV4 == nil {
				sugaredLogger.Error("Provided IP '" + *manualV4 + "' is not a valid IP address - ignoring.")
			}
		}
	} else {
		publicV4 = nil
	}

	if !config.DisableV6 {
		if *manualV6 == "" {
			sugaredLogger.Info("Getting public IPv6...")
			publicV6, err = provider.GetPublicIPv6()

			if err != nil {
				sugaredLogger.Warn("Failed to get public ip: ", err)
				publicV6 = nil
			}

			sugaredLogger.Info("Received public IP " + publicV6.String())
		} else {
			publicV6 = net.ParseIP(*manualV6)
			sugaredLogger.Info("Using manually provied public IPv6 " + *manualV6)

			if publicV6 == nil {
				sugaredLogger.Error("Provided IP '" + *manualV6 + "' is not a valid IP address - ignoring.")
			}
		}
	} else {
		publicV6 = nil
	}

	return publicV4, publicV6
}

// hostNeedsUpdating determines if the records for the given host need updating by comparing the provided IPs with
// a DNS lookup. nil is returned for IP address types that don't need updating.
func hostNeedsUpdating(logger *zap.Logger, domain string, hostName string, publicV4 net.IP, publicV6 net.IP, config *Config) (net.IP, net.IP) {
	sugaredLogger := logger.Sugar()
	if publicV4 != nil {
		sugaredLogger.Info("Resolving current IPv4...")
		currentV4, err := performDNSLookup(logger, hostName+"."+domain, config.DNSServer, dns.TypeA)
		if err != nil {
			sugaredLogger.Warn("Failed to resolve the current IPv4: ", err)
		}
		if currentV4 != nil {
			sugaredLogger.Info("Received current IPv4 " + currentV4.String())
		}

		if currentV4 != nil && publicV4 != nil && currentV4.Equal(publicV4) {
			if !config.ForceUpdate {
				sugaredLogger.Info("v4 DNS entry already up to date - nothing to do.")
				publicV4 = nil
			} else {
				sugaredLogger.Info("v4 DNS entry already up to date, but update forced...")
			}
		} else {
			sugaredLogger.Info("v4 IPs differ - update required...")
		}
	}

	if publicV6 != nil {
		sugaredLogger.Info("Resolving current IPv6...")
		currentV6, err := performDNSLookup(logger, hostName+"."+domain, config.DNSServer, dns.TypeAAAA)
		if err != nil {
			sugaredLogger.Warn("Failed to resolve the current IPv6: ", err)
		}
		if currentV6 != nil {
			sugaredLogger.Info("Received current IPv6 " + currentV6.String())
		}

		if currentV6 != nil && publicV6 != nil && currentV6.Equal(publicV6) {
			if !config.ForceUpdate {
				sugaredLogger.Info("v6 DNS entry already up to date - nothing to do.")
				publicV6 = nil
			} else {
				sugaredLogger.Info("v6 DNS entry already up to date, but update forced...")
			}
		} else {
			sugaredLogger.Info("v6 IPs differ - update required...")
		}
	}

	return publicV4, publicV6
}

func loadConfig(filename string, config *Config) error {
	yamlFile, err := os.ReadFile(filename)
	if err != nil {
		return err
	}

	err = yaml.Unmarshal(yamlFile, &config)
	if err != nil {
		return err
	}

	return nil
}

func validateConfig(logger *zap.Logger, config *Config) bool {
	if config.DNSServer == "" {
		logger.Error("Invalid config: A DNS server must be provided")
		return false
	}

	if len(config.Domains) == 0 {
		logger.Error("Invalid config: No domain configuration was provided")
		return false
	}

	for _, d := range config.Domains {
		if d.DomainName == "" {
			logger.Error("Invalid config: A domain name must be provided")
			return false
		}

		if len(d.Hosts) == 0 {
			logger.Error("Invalid config: At least one host name must be provided")
			return false
		}
	}

	if config.Password == "" {
		logger.Error("Invalid config: A password must be provided")
		return false
	}

	if config.Username == "" {
		logger.Error("Invalid config: A user name must be provided")
		return false
	}

	if config.PublicIPProvider.Service == "" {
		logger.Error("Invalid config: A public IP service must be selected")
		return false
	}

	if config.PublicIPProvider.Service == "local-interface" && config.PublicIPProvider.InterfaceName == "" {
		logger.Error("Invalid config: When selecting the local-interface provider, an interface name must be provided")
		return false
	}

	return true
}

func performDNSLookup(logger *zap.Logger, hostname string, dnsServer string, dnsType uint16) (net.IP, error) {
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
		logger.Warn("Received more than one IPs - just returning the first one")
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
