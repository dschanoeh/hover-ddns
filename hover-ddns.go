package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"os"
	"strconv"

	log "github.com/sirupsen/logrus"
	"gopkg.in/yaml.v2"
)

type _Config struct {
	Username    string
	Password    string
	Hostname    string
	DomainName  string `yaml:"domain_name"`
	ForceUpdate bool   `yaml:"force_update"`
}

type DomainEnvelope struct {
	Succeeded bool `json:"succeeded"`
	Domains   []Domain
}

type Domain struct {
	ID         string `json:"id"`
	DomainName string `json:"domain_name"`
}

type RecordEnvelope struct {
	Succeeded bool `json:"succeeded"`
	Domains   []RecordDomain
}

type RecordDomain struct {
	Records []Record `json:"entries"`
}

type Record struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Type    string `json:"type"`
	Content string `json:"content"`
}

type CreateRecord struct {
	Name    string `json:"name"`
	Type    string `json:"type"`
	Content string `json:"content"`
	TTL     int    `json:"ttl"`
}

func main() {
	var verbose = flag.Bool("verbose", false, "Turns on verbose information on the update process. Otherwise, only errors cause output.")
	var debug = flag.Bool("debug", false, "Turns on debug information")
	var configFile = flag.String("config", "", "Config file")
	var manualIPAddress = flag.String("ip-address", "", "Specify the IP address to be submitted instead of looking it up")

	flag.Parse()

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

	config := _Config{}

	err := loadConfig(*configFile, &config)
	if err != nil {
		log.Error("Could not load config file: ", err)
		os.Exit(1)
	}

	ip := ""
	if *manualIPAddress == "" {
		log.Info("Getting public IP...")
		ip, err = getPublicIP()

		if err != nil {
			log.Error("Failed to get public ip", err)
			os.Exit(1)
		}

		log.Info("Received public IP " + ip)
	} else {
		ip = *manualIPAddress
		log.Info("Using manually provied public IP " + ip)
	}

	log.Info("Resolving current IP...")
	currentIP, err := resolveCurrentIP(config.Hostname + "." + config.DomainName)

	if err != nil {
		log.Error("Failed to resolve the current ip: ", err)
		os.Exit(1)
	}
	log.Info("Received current IP " + currentIP)

	if currentIP == ip {
		if !config.ForceUpdate {
			log.Info("DNS entry already up to date - nothing to do.")
			os.Exit(0)
		} else {
			log.Info("DNS entry already up to date, but update forced...")
		}
	} else {
		log.Info("IPs differ - update required...")
	}

	log.Info("Getting Hover auth cookie...")
	client := &http.Client{}

	sessionCookie, authCookie, err := getHoverAuthCookie(client, config.Username, config.Password)
	if err != nil {
		log.Error("Failed to get auth cookie", err)
		os.Exit(1)
	}

	log.Debug("AuthCookie [" + authCookie.Name + "]: " + authCookie.Value)
	log.Debug("SessionCookie [" + sessionCookie.Name + "]: " + sessionCookie.Value)

	domainID, err := getDomainID(client, sessionCookie, authCookie, config.DomainName)
	if err != nil {
		log.Error("Failed to get domain ID: ", err)
		os.Exit(1)
	}
	log.Info("Found domain ID: " + domainID)

	recordID, err := getRecordID(client, sessionCookie, authCookie, domainID, config.Hostname)
	if err != nil {
		log.Error("Error getting record ID: ", err)
		os.Exit(1)
	}

	// Record exists, so we need to delete it before creating a new one
	if !(recordID == "") {
		log.Info("Found existing record ID: " + recordID)
		log.Info("Deleting...")
		err = deleteRecord(client, sessionCookie, authCookie, recordID)
		if err != nil {
			log.Error("Was not able to delete existing record: ", err)
			os.Exit(1)
		}
	}

	// Create new record
	log.Info("Creating new record...")
	err = createRecord(client, sessionCookie, authCookie, domainID, config.Hostname, ip)
	if err != nil {
		log.Error("Was not able to create new record: ", err)
		os.Exit(1)
	}

	os.Exit(0)
}

func loadConfig(filename string, config *_Config) error {
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

func getPublicIP() (string, error) {
	url := "https://api.ipify.org?format=text"

	resp, err := http.Get(url)
	if err != nil || resp.StatusCode != http.StatusOK {
		return "", err
	}

	defer resp.Body.Close()

	ip, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	return string(ip), nil
}

func resolveCurrentIP(hostname string) (string, error) {
	ips, err := net.LookupIP(hostname)
	if err != nil {
		return "", err
	}

	if len(ips) > 1 {
		log.Warn("Received more than one IP address. Using the first one...")
	}

	return ips[0].String(), nil
}

func getHoverAuthCookie(client *http.Client, username string, password string) (http.Cookie, http.Cookie, error) {

	signinURL := "https://www.hover.com/signin"
	authURL := "https://www.hover.com/signin/auth.json"

	sessionCookie := http.Cookie{}
	authCookie := http.Cookie{}

	// Get session cookie
	resp, err := http.Get(signinURL)
	if resp.StatusCode != 200 {
		return sessionCookie, authCookie, errors.New("Received sessionstatus code " + strconv.Itoa(resp.StatusCode))
	}
	for _, cookie := range resp.Cookies() {
		log.Info(cookie.Name)
		if cookie.Name == "hover_session" {
			log.Info("got session cookie")
			sessionCookie = *cookie
			break
		}
	}

	// Get auth cookie
	values := map[string]string{"username": username, "password": password}
	jsonStr, _ := json.Marshal(values)

	req, err := http.NewRequest("POST", authURL, bytes.NewBuffer(jsonStr))
	if err != nil {
		return sessionCookie, authCookie, err
	}

	req.AddCookie(&sessionCookie)
	req.Header.Set("Content-Type", "application/json")

	resp, err = client.Do(req)

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := ioutil.ReadAll(resp.Body)
		log.Info(string(bodyBytes))
		return sessionCookie, authCookie, errors.New("Received status code " + strconv.Itoa(resp.StatusCode))
	}
	if err != nil {
		return sessionCookie, authCookie, err
	}

	for _, cookie := range resp.Cookies() {
		// Response returns two hoverauth cookies, the first having no value
		if cookie.Name == "hoverauth" && cookie.Value != "" {
			authCookie = *cookie
			return sessionCookie, authCookie, nil
		}
	}

	return sessionCookie, authCookie, errors.New("Didn't receive a hoverauth cookie")
}

func getDomainID(client *http.Client, sessionCookie http.Cookie, authCookie http.Cookie, domainName string) (string, error) {
	domainsURL := "https://www.hover.com/api/domains/"

	req, err := http.NewRequest("GET", domainsURL, nil)
	if err != nil {
		return "", err
	}

	req.AddCookie(&sessionCookie)
	req.AddCookie(&authCookie)

	resp, err := client.Do(req)

	if resp.StatusCode != http.StatusOK {
		return "", errors.New("Received status code " + strconv.Itoa(resp.StatusCode))
	}
	if err != nil {
		return "", err
	}

	defer resp.Body.Close()

	domainsBodyBytes, _ := ioutil.ReadAll(resp.Body)
	log.Debug(string(domainsBodyBytes))

	var result DomainEnvelope
	err = json.Unmarshal(domainsBodyBytes, &result)

	if err != nil {
		return "", err
	}
	if !result.Succeeded {
		return "", errors.New("Domain request failed")
	}

	domainID := ""
	for _, domain := range result.Domains {
		if domain.DomainName == domainName {
			domainID = domain.ID
		}
	}

	if domainID == "" {
		return "", errors.New("Could not find domain '" + domainName + "' in list of domains")
	}

	return domainID, nil
}

func getRecordID(client *http.Client, sessionCookie http.Cookie, authCookie http.Cookie, domainID string, hostName string) (string, error) {
	recordsURL := "https://www.hover.com/api/domains/" + domainID + "/dns"
	req, err := http.NewRequest("GET", recordsURL, nil)
	if err != nil {
		return "", err
	}

	req.AddCookie(&sessionCookie)
	req.AddCookie(&authCookie)

	recordResp, err := client.Do(req)

	if recordResp.StatusCode != http.StatusOK {
		return "", errors.New("Received status code " + strconv.Itoa(recordResp.StatusCode))
	}
	if err != nil {
		return "", err
	}

	defer recordResp.Body.Close()

	bodyBytes, _ := ioutil.ReadAll(recordResp.Body)
	log.Debug(string(bodyBytes))

	var recordsResult RecordEnvelope
	err = json.Unmarshal(bodyBytes, &recordsResult)

	if err != nil {
		return "", err
	}

	log.Debug(fmt.Sprintf("%+v\n", recordsResult))
	if !recordsResult.Succeeded || len(recordsResult.Domains) != 1 {
		return "", errors.New("Records request failed")
	}

	recordID := ""
	for _, record := range recordsResult.Domains[0].Records {
		log.Debug(fmt.Sprintf("Record: %s %s %s", record.Name, record.Type, record.Content))
		if record.Name == hostName && record.Type == "A" {
			recordID = record.ID
		}
	}

	return recordID, nil
}

func createRecord(client *http.Client, sessionCookie http.Cookie, authCookie http.Cookie, domainID string, hostName string, address string) error {
	r := CreateRecord{}
	r.Content = address
	r.Name = hostName
	r.TTL = 3600
	r.Type = "A"

	jsonStr, err := json.Marshal(r)
	if err != nil {
		return err
	}

	recordPostURL := "https://www.hover.com/api/domains/" + domainID + "/dns"

	req, err := http.NewRequest("POST", recordPostURL, bytes.NewBuffer(jsonStr))
	if err != nil {
		return err
	}

	req.AddCookie(&sessionCookie)
	req.AddCookie(&authCookie)
	req.Header.Set("Content-Type", "application/json")

	recordPostResponse, err := client.Do(req)
	if err != nil {
		return err
	}
	defer recordPostResponse.Body.Close()

	recordPostResponseBodyBytes, _ := ioutil.ReadAll(recordPostResponse.Body)
	log.Debug(string(recordPostResponseBodyBytes))

	if recordPostResponse.StatusCode != 200 {
		return errors.New("Received status code " + strconv.Itoa(recordPostResponse.StatusCode))
	}

	return nil
}

func deleteRecord(client *http.Client, sessionCookie http.Cookie, authCookie http.Cookie, identifier string) error {
	url := "https://www.hover.com/api/dns/" + identifier
	req, err := http.NewRequest("DELETE", url, nil)
	if err != nil {
		return err
	}

	req.AddCookie(&sessionCookie)
	req.AddCookie(&authCookie)

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return errors.New("Received status code " + strconv.Itoa(resp.StatusCode))
	}

	return nil
}
