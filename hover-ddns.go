package main

import (
	"flag"
	"fmt"
	"io/ioutil"
	"net"
	"os"

	"github.com/dschanoeh/hover-ddns/hover"
	"github.com/dschanoeh/hover-ddns/publicip"
	log "github.com/sirupsen/logrus"
	"gopkg.in/yaml.v2"
)

type Config struct {
	Username         string
	Password         string
	Hostname         string
	DomainName       string                        `yaml:"domain_name"`
	ForceUpdate      bool                          `yaml:"force_update"`
	PublicIPProvider publicip.LookupProviderConfig `yaml:"public_ip_provider"`
}

var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
	builtBy = "unknown"
)

func main() {
	var verbose = flag.Bool("verbose", false, "Turns on verbose information on the update process. Otherwise, only errors cause output.")
	var debug = flag.Bool("debug", false, "Turns on debug information")
	var dryRun = flag.Bool("dry-run", false, "Perform lookups but don't actually update the DNS info")
	var configFile = flag.String("config", "", "Config file")
	var manualIPAddress = flag.String("ip-address", "", "Specify the IP address to be submitted instead of looking it up")
	var versionFlag = flag.Bool("version", false, "Prints version information of the hover-ddns binary")

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

	if *configFile == "" {
		log.Error("Please provide a config file to read")
		flag.Usage()
		os.Exit(1)
	}

	config := Config{}

	err := loadConfig(*configFile, &config)
	if err != nil {
		log.Error("Could not load config file: ", err)
		os.Exit(1)
	}

	var provider publicip.LookupProvider
	provider, err = publicip.NewLookupProvider(&config.PublicIPProvider)
	if err != nil {
		log.Error("Could not configure public ip provider: ", err)
		os.Exit(1)
	}

	ip := net.IP{}
	if *manualIPAddress == "" {
		log.Info("Getting public IP...")
		ip, err = provider.GetPublicIP()

		if err != nil {
			log.Error("Failed to get public ip: ", err)
			os.Exit(1)
		}

		log.Info("Received public IP " + ip.String())
	} else {
		ip = net.ParseIP(*manualIPAddress)
		log.Info("Using manually provied public IP " + *manualIPAddress)

		if ip == nil {
			log.Error("Provided IP '" + *manualIPAddress + "' is not a valid IP address.")
			os.Exit(1)
		}
	}

	log.Info("Resolving current IP...")
	currentIP, err := resolveCurrentIP(config.Hostname + "." + config.DomainName)

	if err != nil {
		log.Error("Failed to resolve the current ip: ", err)
		os.Exit(1)
	}
	log.Info("Received current IP " + currentIP.String())

	if currentIP.Equal(ip) {
		if !config.ForceUpdate {
			log.Info("DNS entry already up to date - nothing to do.")
			os.Exit(0)
		} else {
			log.Info("DNS entry already up to date, but update forced...")
		}
	} else {
		log.Info("IPs differ - update required...")
	}

	if !*dryRun {
		err = hover.Update(config.Username, config.Password, config.DomainName, config.Hostname, ip)
		if err != nil {
			os.Exit(1)
		}
	}

	os.Exit(0)
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

func resolveCurrentIP(hostname string) (net.IP, error) {
	ips, err := net.LookupIP(hostname)
	if err != nil {
		return nil, err
	}

	if len(ips) > 1 {
		log.Warn("Received more than one IP address. Using the first one...")
	}

	return ips[0], nil
}
