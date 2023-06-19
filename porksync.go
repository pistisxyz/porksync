package main

import (
	"bytes"
	"encoding/json"
	"net"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"fmt"
	"io"

	"net/http"
	"os"

	"github.com/joho/godotenv"
	"gopkg.in/yaml.v3"
)

var (
	logFile   *os.File
	LOG_PATH  string
	CONF_PATH string
)

func _init() {
	if runtime.GOOS == "linux" {
		LOG_PATH = "/var/log/porksync.log"
		CONF_PATH = "/etc/porksync.yaml"
	} else {
		LOG_PATH, _ = filepath.Abs("./porksync.log")
		CONF_PATH, _ = filepath.Abs("./porksync.yaml")
	}

	if os.Getenv("PORKSYNC_LOG_PATH") != "" {
		LOG_PATH, _ = filepath.Abs(os.Getenv("PORKSYNC_LOG_PATH") + "/porksync.log")
	}
	if os.Getenv("PORKSYNC_YAML_PATH") != "" {
		CONF_PATH, _ = filepath.Abs(os.Getenv("PORKSYNC_YAML_PATH") + "/porksync.yaml")
	}

	if _, err := os.Stat(LOG_PATH); err != nil {
		file, err := os.Create(LOG_PATH)
		CatchErr(err)
		logFile = file
	} else {
		file, err := os.Open(LOG_PATH)
		CatchErr(err)
		logFile = file
	}
}

func main() {
	godotenv.Load(".env")

	if os.Getenv("SECRET_TOKEN") == "" || os.Getenv("PUBLIC_TOKEN") == "" {
		fmt.Println("Missing SECRET_TOKEN or PUBLIC_TOKEN")
		os.Exit(1)
	}

	_init()

	cat := ReadConf()
	remote := ParseRetrieve(Fetch())
	myIp := GetMyIp()
	if remote.Status != "SUCCESS" {
		fmt.Println("Failed fetching from porkbun")
		os.Exit(1)
	}

	for entry := range cat {
		address := cat[entry].Address
		var ips net.IP

		if address == "localhost" {
			ips = myIp
		} else {
			_ips, _ := net.LookupIP(address)
			ips = _ips[0]
		}

		for _, record := range remote.Records {
			if record.Type == "A" && record.Name == entry {
				remoteIp := ParseIP(record.Content)
				if !ips.Equal(remoteIp) {
					Log(fmt.Sprintf("Mismatched IPs %v <> %v", ips, remoteIp))
					UpdateDomainRecord(record, ips.String())
				}
			}
		}
	}
}

func UpdateDomainRecord(domain Record, newIp string) {
	client := &http.Client{}
	_arr := strings.Split(domain.Name, ".")
	domainName := strings.Join(_arr[len(_arr)-2:], ".")
	subDomain := strings.Join(_arr[:len(_arr)-2], ".")
	data := fmt.Sprintf(`{
		"secretapikey": "%v",
		"apikey": "%v",
		"name": "%v",
		"type": "%v",
		"content": "%v"
	}`, os.Getenv("SECRET_TOKEN"), os.Getenv("PUBLIC_TOKEN"), subDomain, domain.Type, newIp)
	req, _ := http.NewRequest("POST", fmt.Sprintf("https://porkbun.com/api/json/v3/dns/edit/%v/%v", domainName, domain.Id), bytes.NewBuffer([]byte(data)))
	Log("%+v", domain)
	Log("https://porkbun.com/api/json/v3/dns/edit/%v/%v", domain.Name, domain.Id)
	res, err := client.Do(req)
	CatchErr(err)

	body, _ := io.ReadAll(res.Body)
	Log(string(body))
}

func Log(log string, a ...any) {
	fmt.Printf(log+"\n", a...)
	if logFile != nil {
		body := fmt.Sprintf("[%v] ", time.Now().Format("2006/01/02 15:04:05")) + (fmt.Sprintf(log+"\n", a...))
		logFile.Write([]byte(body))
	}
}

func Fetch() []byte {
	client := &http.Client{}

	data := fmt.Sprintf(`{"secretapikey": "%v", "apikey": "%v"}`, os.Getenv("SECRET_TOKEN"), os.Getenv("PUBLIC_TOKEN"))
	req, err := http.NewRequest("POST", "https://porkbun.com/api/json/v3/dns/retrieve/megumax.moe", bytes.NewBuffer([]byte(data)))
	CatchErr(err)

	resp, err := client.Do(req)
	CatchErr(err)

	body, err := io.ReadAll(resp.Body)
	CatchErr(err)

	return body
}

func ParseIP(ip string) net.IP {
	octets := strings.Split(ip, ".")
	if len(octets) != 4 {
		Log(fmt.Sprintf("Error in parsing ip %v\n", ip))
		os.Exit(2)
		return nil
	}
	return net.IPv4(byteIt(octets[0]), byteIt(octets[1]), byteIt(octets[2]), byteIt(octets[3]))
}

func ReadConf() Catalogue {
	cat := Catalogue{}
	data, err := os.ReadFile(CONF_PATH)
	CatchErr(err)
	yaml.Unmarshal(data, cat)
	return cat
}

func ParseRetrieve(b []byte) Retireve {
	var r Retireve
	CatchErr(json.Unmarshal(b, &r))
	return r
}

func CatchErr(err error) {
	if err != nil {
		Log(err.Error())
		os.Exit(1)
	}
}

func GetMyIp() net.IP {
	client := &http.Client{}
	dataKeys := fmt.Sprintf(`{"secretapikey": "%v", "apikey": "%v"}`, os.Getenv("SECRET_TOKEN"), os.Getenv("PUBLIC_TOKEN"))
	req, _ := http.NewRequest("POST", "https://porkbun.com/api/json/v3/ping", bytes.NewBuffer([]byte(dataKeys)))
	resp, err := client.Do(req)
	if err != nil {
		Log("Porkbun API doesn't respond or no internet connection")
		Log(err.Error())
		os.Exit(1)
	}
	var myIp MyIp
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		Log("invalid Porkbun response")
		Log(err.Error())
		os.Exit(1)
	}
	json.Unmarshal(data, &myIp)
	ip := strings.Split(myIp.Ip, ".")
	if len(ip) != 4 {
		Log(fmt.Sprintf("Error in parsing ip %v\n", myIp.Ip))
		os.Exit(2)
	}
	return net.IPv4(byteIt(ip[0]), byteIt(ip[1]), byteIt(ip[2]), byteIt(ip[3]))
}

func byteIt(s string) byte {
	b, err := (strconv.Atoi(s))
	CatchErr(err)
	return byte(b)
}

type Catalogue map[string]struct {
	Address string `yaml:"address"`
}

type Retireve struct {
	Status  string   `json:"status"`
	Records []Record `json:"records"`
}

type Record struct {
	Id      string `json:"id"`
	Name    string `json:"name"`
	Content string `json:"content"`
	Type    string `json:"type"`
	Ttl     string `json:"ttl"`
	Prio    string `json:"prio"`
	Notes   string `json:"notes"`
}

type MyIp struct {
	Status string `json:"status"`
	Ip     string `json:"yourIp"`
}
