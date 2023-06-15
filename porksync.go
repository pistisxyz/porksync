package main

import (
	"bytes"
	"encoding/json"
	"net"
	"strconv"
	"strings"

	"fmt"
	"io"

	"net/http"
	"os"

	"github.com/joho/godotenv"
	"gopkg.in/yaml.v3"
)

const CONF_PATH = "porksync.yaml"

func main() {
	godotenv.Load(".env")

	cat := ReadConf()
	remote := ParseRetrieve(Fetch())
	ip := GetMyIp()
	if remote.Status != "SUCCESS" {
		fmt.Println("Failed fetching from porkbun")
		os.Exit(1)
	}

	for entry := range cat {
		address := cat[entry].Address
		var ips net.IP
		if address == "localhost" {
			ips = ip
		} else {
			_ips, _ := net.LookupIP(address)
			ips = _ips[0]
		}
		fmt.Println(entry, cat[entry].Address)
		for _, record := range remote.Records {
			if record.Type == "A" && record.Name == entry {
				fmt.Printf("  %+v\n", record)
				octets := strings.Split(record.Content, ".")
				if len(octets) != 4 {
					fmt.Printf("Error in parsing ip %v\n", record.Content)
					os.Exit(3)
				}
				fmt.Printf("  %v %v\n", ips, record.Content)
				if !ips.Equal(net.IPv4(byteIt(octets[0]), byteIt(octets[1]), byteIt(octets[2]), byteIt(octets[3]))) {
					// TODO: Adjust IPs on porkbun via API
					fmt.Println("Mismatched IPs")
				}
			}
		}
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
		fmt.Println(err)
		os.Exit(1)
	}
}

func GetMyIp() net.IP {
	client := &http.Client{}
	dataKeys := fmt.Sprintf(`{"secretapikey": "%v", "apikey": "%v"}`, os.Getenv("SECRET_TOKEN"), os.Getenv("PUBLIC_TOKEN"))
	req, err := http.NewRequest("POST", "https://porkbun.com/api/json/v3/ping", bytes.NewBuffer([]byte(dataKeys)))
	resp, err := client.Do(req)
	if err != nil {
		fmt.Println("Porkbun API doesn't respond or no internet connection")
		fmt.Println(err)
		os.Exit(1)
	}
	var myIp MyIp
	data, err := io.ReadAll(resp.Body)
	json.Unmarshal(data, &myIp)
	ip := strings.Split(myIp.Ip, ".")
	if len(ip) != 4 {
		fmt.Printf("Error in parsing ip %v\n", myIp.Ip)
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
