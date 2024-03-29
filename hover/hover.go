package hover

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"io/ioutil"
	"net"
	"net/http"
	"strconv"
	"time"

	"go.uber.org/zap"
)

const (
	HoverSigninUrl  = "https://www.hover.com/signin"
	HoverAuthUrl    = "https://www.hover.com/api/login"
	HoverDomainsUrl = "https://www.hover.com/api/domains/"
	HoverDnsUrl     = "https://www.hover.com/api/dns/"
	RecordTTL       = 3600
)

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

type HoverAuth struct {
	SessionCookie http.Cookie
	AuthCookie    http.Cookie
}

type HoverClient struct {
	logger        *zap.SugaredLogger
	httpClient    *http.Client
	sessionCookie *http.Cookie
	authCookie    *http.Cookie
}

func NewClient(logger *zap.Logger) *HoverClient {
	tr := &http.Transport{
		MaxIdleConns:          10,
		IdleConnTimeout:       15 * time.Second,
		ResponseHeaderTimeout: 15 * time.Second,
		DisableKeepAlives:     false,
	}
	httpClient := &http.Client{
		Transport: tr,
	}

	client := HoverClient{
		logger:     logger.Sugar(),
		httpClient: httpClient,
	}
	return &client
}

func (c *HoverClient) IsAuthenticated() bool {
	if c == nil {
		return false
	}
	if c.sessionCookie == nil || c.authCookie == nil {
		return false
	}

	return true
}

// Update tries to update the DNS record for hostName with the provided IP(s).
// Provide nil for any of the addresses if that record shouldn't get updated
func (c *HoverClient) Update(domainName string, hostName string, ip4 net.IP, ip6 net.IP) error {
	if !c.IsAuthenticated() {
		return errors.New("no auth session was provided")
	}

	domainID, err := c.getDomainID(domainName)
	if err != nil {
		c.logger.Errorf("Failed to get domain ID: %s", err)
		return err
	}
	c.logger.Infof("Found domain ID %s for domain %s", domainID, domainName)

	if ip4 != nil {
		if ip4.To4() == nil {
			c.logger.Errorf("Not updating invalid address '%s'", ip4.String())
		} else {
			err = c.updateSingleRecord(domainID, hostName, ip4.String(), "A")
			if err != nil {
				c.logger.Errorf("Was not able to update IPv4 record: %s", err)
			}
		}
	}
	if ip6 != nil {
		if ip6.To16() == nil {
			c.logger.Errorf("Not updating invalid address '%s'", ip4.String())
		} else {
			err = c.updateSingleRecord(domainID, hostName, ip6.String(), "AAAA")
			if err != nil {
				c.logger.Errorf("Was not able to update IPv6 record: %s", err)
			}
		}
	}

	return nil
}

func (c *HoverClient) updateSingleRecord(domainID string, hostName string, ip string, recordType string) error {
	recordID, err := c.getRecordID(domainID, hostName, recordType)
	if err != nil {
		c.logger.Errorf("Error getting record ID: %s", err)
		return err
	}

	// Record exists, so we need to delete it before creating a new one
	if !(recordID == "") {
		c.logger.Infof("Found existing record ID %s for host name %s and type %s", domainID, hostName, recordType)
		c.logger.Info("Deleting existing record...")
		err = c.deleteRecord(recordID)
		if err != nil {
			c.logger.Errorf("Was not able to delete existing record: %s", err)
			return err
		}
	}

	// Create new record
	c.logger.Infof("Creating new record of type '%s' and IP '%s'...", recordType, ip)
	err = c.createRecord(domainID, hostName, ip, recordType)
	if err != nil {
		c.logger.Errorf("Was not able to create new record: %s ", err)
		return err
	}

	return nil
}

func (c *HoverClient) Login(username string, password string) error {
	sessionCookie := http.Cookie{}

	c.logger.Info("Logging in to Hover API...")
	// Get session cookie
	req, err := http.NewRequest(http.MethodGet, HoverSigninUrl, nil)
	if err != nil {
		return errors.New("Failed to get session cookie: " + err.Error())
	}
	resp, err := c.httpClient.Do(req)

	if err != nil {
		return errors.New("Failed to get session cookie: " + err.Error())
	}

	io.Copy(ioutil.Discard, resp.Body)
	resp.Body.Close()

	if resp.StatusCode != 200 {
		return errors.New("Failed to get session cookie: HTTP " + strconv.Itoa(resp.StatusCode))
	}
	for _, cookie := range resp.Cookies() {
		c.logger.Debugf("Found cookie: %s", cookie.Name)
		if cookie.Name == "hover_session" {
			sessionCookie = *cookie
			c.logger.Debugf("got session cookie value '%s' expires '%s'", sessionCookie.Value, sessionCookie.Expires.String())
			break
		}
	}

	// Get auth cookie
	values := map[string]string{"username": username, "password": password}
	jsonStr, _ := json.Marshal(values)

	authReq, err := http.NewRequest(http.MethodPost, HoverAuthUrl, bytes.NewBuffer(jsonStr))
	if err != nil {
		return err
	}

	authReq.AddCookie(&sessionCookie)
	authReq.Header.Set("Content-Type", "application/json")

	resp, err = c.httpClient.Do(authReq)
	if err != nil {
		return err
	}
	io.Copy(ioutil.Discard, resp.Body)
	resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return errors.New("Received status code " + strconv.Itoa(resp.StatusCode))
	}
	if err != nil {
		return err
	}

	for _, cookie := range resp.Cookies() {
		// Response returns two hoverauth cookies, the first having no value
		if cookie.Name == "hoverauth" && cookie.Value != "" {
			c.authCookie = cookie
			c.logger.Debugf("got auth cookie value '%s' expires '%s'", cookie.Value, cookie.Expires.String())
			c.sessionCookie = &sessionCookie
			return nil
		}
	}

	return errors.New("didn't receive a hoverauth cookie")
}

func (c *HoverClient) getDomainID(domainName string) (string, error) {
	req, err := http.NewRequest(http.MethodGet, HoverDomainsUrl, nil)
	if err != nil {
		return "", err
	}

	req.AddCookie(c.sessionCookie)
	req.AddCookie(c.authCookie)

	resp, err := c.httpClient.Do(req)

	if err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusOK {
		io.Copy(ioutil.Discard, resp.Body)
		resp.Body.Close()
		return "", errors.New("Received status code " + strconv.Itoa(resp.StatusCode))
	}

	defer resp.Body.Close()

	domainsBodyBytes, _ := io.ReadAll(resp.Body)
	c.logger.Debug(string(domainsBodyBytes[:]))

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

func (c *HoverClient) getRecordID(domainID string, hostName string, recordType string) (string, error) {
	recordsURL := HoverDomainsUrl + domainID + "/dns"
	req, err := http.NewRequest(http.MethodGet, recordsURL, nil)
	if err != nil {
		return "", err
	}

	req.AddCookie(c.sessionCookie)
	req.AddCookie(c.authCookie)

	recordResp, err := c.httpClient.Do(req)

	if recordResp.StatusCode != http.StatusOK {
		io.Copy(ioutil.Discard, recordResp.Body)
		recordResp.Body.Close()
		return "", errors.New("Received status code " + strconv.Itoa(recordResp.StatusCode))
	}
	if err != nil {
		return "", err
	}

	defer recordResp.Body.Close()

	bodyBytes, _ := io.ReadAll(recordResp.Body)
	c.logger.Debug(string(bodyBytes))

	var recordsResult RecordEnvelope
	err = json.Unmarshal(bodyBytes, &recordsResult)

	if err != nil {
		return "", err
	}

	c.logger.Debugf("%+v\n", recordsResult)
	if !recordsResult.Succeeded || len(recordsResult.Domains) != 1 {
		return "", errors.New("records request failed")
	}

	recordID := ""
	for _, record := range recordsResult.Domains[0].Records {
		c.logger.Debugf("Record: %s %s %s", record.Name, record.Type, record.Content)
		if record.Name == hostName && record.Type == recordType {
			recordID = record.ID
		}
	}

	return recordID, nil
}

func (c *HoverClient) createRecord(domainID string, hostName string, address string, recordType string) error {
	r := CreateRecord{
		Content: address,
		Name:    hostName,
		TTL:     RecordTTL,
		Type:    recordType,
	}

	jsonStr, err := json.Marshal(r)
	if err != nil {
		return err
	}

	recordPostURL := HoverDomainsUrl + domainID + "/dns"
	c.logger.Debugf("Creating record: %s", string(jsonStr))

	req, err := http.NewRequest(http.MethodPost, recordPostURL, bytes.NewBuffer(jsonStr))
	if err != nil {
		return err
	}

	req.AddCookie(c.sessionCookie)
	req.AddCookie(c.authCookie)
	req.Header.Set("Content-Type", "application/json")

	recordPostResponse, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer recordPostResponse.Body.Close()

	recordPostResponseBodyBytes, _ := io.ReadAll(recordPostResponse.Body)
	c.logger.Debug(string(recordPostResponseBodyBytes))

	if recordPostResponse.StatusCode != 200 {
		return errors.New("Received status code " + strconv.Itoa(recordPostResponse.StatusCode))
	}

	return nil
}

func (c *HoverClient) deleteRecord(identifier string) error {
	url := HoverDnsUrl + identifier
	req, err := http.NewRequest(http.MethodDelete, url, nil)
	if err != nil {
		return err
	}

	req.AddCookie(c.sessionCookie)
	req.AddCookie(c.authCookie)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	io.Copy(ioutil.Discard, resp.Body)
	resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return errors.New("Received status code " + strconv.Itoa(resp.StatusCode))
	}

	return nil
}
