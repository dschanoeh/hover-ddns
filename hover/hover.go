package hover

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"strconv"

	log "github.com/sirupsen/logrus"
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

// Update tries to update the DNS record for hostName with the provided IP(s).
// Provide nil for any of the addresses if that record shouldn't get updated
func Update(auth *HoverAuth, domainName string, hostName string, ip4 net.IP, ip6 net.IP) error {
	client := &http.Client{}

	if auth == nil {
		return errors.New("no auth session was provided")
	}

	domainID, err := getDomainID(client, auth.SessionCookie, auth.AuthCookie, domainName)
	if err != nil {
		log.Error("Failed to get domain ID: ", err)
		return err
	}
	log.Infof("Found domain ID %s for domain %s", domainID, domainName)

	if ip4 != nil {
		if ip4.To4() == nil {
			log.Error(fmt.Sprintf("Not updating invalid address '%s'", ip4.String()))
		} else {
			err = updateSingleRecord(client, auth.SessionCookie, auth.AuthCookie, domainID, hostName, ip4.String(), "A")
			if err != nil {
				log.Error("Was not able to update IPv4 record:", err)
			}
		}
	}
	if ip6 != nil {
		if ip6.To16() == nil {
			log.Error(fmt.Sprintf("Not updating invalid address '%s'", ip4.String()))
		} else {
			err = updateSingleRecord(client, auth.SessionCookie, auth.AuthCookie, domainID, hostName, ip6.String(), "AAAA")
			if err != nil {
				log.Error("Was not able to update IPv6 record:", err)
			}
		}
	}

	return nil
}

func updateSingleRecord(client *http.Client, sessionCookie http.Cookie, authCookie http.Cookie, domainID string, hostName string, ip string, recordType string) error {
	recordID, err := getRecordID(client, sessionCookie, authCookie, domainID, hostName, recordType)
	if err != nil {
		log.Error("Error getting record ID: ", err)
		return err
	}

	// Record exists, so we need to delete it before creating a new one
	if !(recordID == "") {
		log.Infof("Found existing record ID %s for host name %s and type %s", domainID, hostName, recordType)
		log.Info("Deleting existing record...")
		err = deleteRecord(client, sessionCookie, authCookie, recordID)
		if err != nil {
			log.Error("Was not able to delete existing record: ", err)
			return err
		}
	}

	// Create new record
	log.Infof("Creating new record of type '%s' and IP '%s'...", recordType, ip)
	err = createRecord(client, sessionCookie, authCookie, domainID, hostName, ip, recordType)
	if err != nil {
		log.Error("Was not able to create new record: ", err)
		return err
	}

	return nil
}

func Login(username string, password string) (*HoverAuth, error) {

	signinURL := "https://www.hover.com/signin"
	authURL := "https://www.hover.com/api/login"

	client := &http.Client{}
	sessionCookie := http.Cookie{}
	authCookie := http.Cookie{}

	log.Info("Getting Hover auth cookie...")
	// Get session cookie
	resp, err := http.Get(signinURL)
	if err != nil {
		return nil, errors.New("Failed to get session cookie: " + err.Error())
	}
	if resp.StatusCode != 200 {
		return nil, errors.New("Failed to get session cookie: HTTP " + strconv.Itoa(resp.StatusCode))
	}
	for _, cookie := range resp.Cookies() {
		log.Debug("Found cookie: ", cookie.Name)
		if cookie.Name == "hover_session" {
			log.Debug("got session cookie")
			sessionCookie = *cookie
			break
		}
	}

	// Get auth cookie
	values := map[string]string{"username": username, "password": password}
	jsonStr, _ := json.Marshal(values)

	req, err := http.NewRequest("POST", authURL, bytes.NewBuffer(jsonStr))
	if err != nil {
		return nil, err
	}

	req.AddCookie(&sessionCookie)
	req.Header.Set("Content-Type", "application/json")

	resp, err = client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := ioutil.ReadAll(resp.Body)
		log.Debug(string(bodyBytes))
		return nil, errors.New("Received status code " + strconv.Itoa(resp.StatusCode))
	}
	if err != nil {
		return nil, err
	}

	for _, cookie := range resp.Cookies() {
		// Response returns two hoverauth cookies, the first having no value
		if cookie.Name == "hoverauth" && cookie.Value != "" {
			authCookie = *cookie
			var auth = HoverAuth{
				AuthCookie:    authCookie,
				SessionCookie: sessionCookie,
			}
			return &auth, nil
		}
	}

	return nil, errors.New("didn't receive a hoverauth cookie")
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

	if err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusOK {
		return "", errors.New("Received status code " + strconv.Itoa(resp.StatusCode))
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

func getRecordID(client *http.Client, sessionCookie http.Cookie, authCookie http.Cookie, domainID string, hostName string, recordType string) (string, error) {
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
		return "", errors.New("records request failed")
	}

	recordID := ""
	for _, record := range recordsResult.Domains[0].Records {
		log.Debug(fmt.Sprintf("Record: %s %s %s", record.Name, record.Type, record.Content))
		if record.Name == hostName && record.Type == recordType {
			recordID = record.ID
		}
	}

	return recordID, nil
}

func createRecord(client *http.Client, sessionCookie http.Cookie, authCookie http.Cookie, domainID string, hostName string, address string, recordType string) error {
	r := CreateRecord{}
	r.Content = address
	r.Name = hostName
	r.TTL = 3600
	r.Type = recordType

	jsonStr, err := json.Marshal(r)
	if err != nil {
		return err
	}

	recordPostURL := "https://www.hover.com/api/domains/" + domainID + "/dns"
	log.Debug("Creating record: " + string(jsonStr))

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
