package main

import (
	"bytes"
	"encoding/json"
	"net"
	"os/exec"
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
	_ "github.com/k0kubun/pp/v3"
	"gopkg.in/yaml.v3"
)

var (
	logFile   *os.File
	LOG_PATH  string
	CONF_PATH string
	dryRun    = false
)

func init() {
	godotenv.Load(".env")

	if runtime.GOOS == "linux" {
		LOG_PATH = "/var/log/porksync.log"
		CONF_PATH = "/etc/porksync/"
	} else {
		LOG_PATH, _ = filepath.Abs("./porksync.log")
		CONF_PATH, _ = filepath.Abs("./porksync/")
	}

	if os.Getenv("PORKSYNC_LOG_PATH") != "" {
		LOG_PATH, _ = filepath.Abs(os.Getenv("PORKSYNC_LOG_PATH") + "/porksync.log")
	}
	if os.Getenv("PORKSYNC_CONF_PATH") != "" {
		CONF_PATH, _ = filepath.Abs(os.Getenv("PORKSYNC_CONF_PATH"))
	}

	file, err := os.OpenFile(LOG_PATH, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0644)
	// CatchErr(err)
	_ = err
	logFile = file

	for _, arg := range os.Args {
		if (arg == "--dry") || (arg == "-d") {
			dryRun = true
		}
	}
}

func main() {
	defer logFile.Close()

	CatchErr(filepath.Walk(CONF_PATH, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if !info.IsDir() && info.Name()[0] != '_' {
			CheckDomains(ReadConf(path), path)
		}

		return nil
	}))
}

func CheckDomains(cat Catalogue, path string) {
	if cat["sk"] == nil {
		fmt.Printf("%v: 'sk' is required!\n", path)
		return
	}
	if cat["pk"] == nil {
		fmt.Printf("%v: 'pk' is required!\n", path)
		return
	}
	sk, pk := cat["sk"].(string), cat["pk"].(string)

	myIp := GetMyIp(sk, pk)

	for domainName := range cat {
		if domainName == "pk" || domainName == "sk" {
			continue
		}
		remote := ParseRetrieve(Fetch(domainName, pk, sk))
		if remote.Status != "SUCCESS" {
			Log("Failed fetching from porkbun for %v", domainName)
			os.Exit(1)
		}
		Log("Starting routine check for %v", domainName)

		var ips net.IP
		subDomains := cat[domainName].(Catalogue)
		for subDomain := range subDomains {
			domainNameAlt := domainName
			var address string
			if subDomain == "__address" {
				address = subDomains[subDomain].(string)
			} else if subDomain == "__ssl" {
				isCertValid(subDomains[subDomain].(string), domainName, sk, pk)
				continue
			} else {
				address = subDomains[subDomain].(Catalogue)["address"].(string)
				domainNameAlt = subDomain + "." + domainName
			}
			if address == "localhost" {
				ips = myIp
			} else {
				_ips, _ := net.LookupIP(address)
				ips = _ips[0]
			}

			IpCompare(remote, domainNameAlt, ips, sk, pk)
		}
	}
}

func IpCompare(remote Retireve, domainName string, ips net.IP, sk string, pk string) {
	for _, record := range remote.Records {
		if record.Type == "A" && record.Name == domainName {
			remoteIp := ParseIP(record.Content)
			if !ips.Equal(remoteIp) {
				Log(fmt.Sprintf("Mismatched IPs %v <> %v", ips, remoteIp))
				UpdateDomainRecord(record, ips.String(), sk, pk)
			}
		}
	}
}

func UpdateDomainRecord(domain Record, newIp string, sk string, pk string) {
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
	}`, sk, pk, subDomain, domain.Type, newIp)
	req, _ := http.NewRequest("POST", fmt.Sprintf("https://porkbun.com/api/json/v3/dns/edit/%v/%v", domainName, domain.Id), bytes.NewBuffer([]byte(data)))
	Log("%+v", domain)
	if dryRun {
		return
	}
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

func Fetch(domainName string, pk string, sk string) []byte {
	client := &http.Client{}

	data := fmt.Sprintf(`{"secretapikey": "%v", "apikey": "%v"}`, sk, pk)
	req, err := http.NewRequest("POST", "https://porkbun.com/api/json/v3/dns/retrieve/"+domainName, bytes.NewBuffer([]byte(data)))
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

func ReadConf(conf_path string) Catalogue {
	var cat Catalogue
	data, err := os.ReadFile(conf_path)
	CatchErr(err)
	err = yaml.Unmarshal(data, &cat)
	CatchErr(err)
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

func GetMyIp(sk string, pk string) net.IP {
	client := &http.Client{}
	dataKeys := fmt.Sprintf(`{"secretapikey": "%v", "apikey": "%v"}`, sk, pk)
	req, _ := http.NewRequest("POST", "https://api-ipv4.porkbun.com/api/json/v3/ping", bytes.NewBuffer([]byte(dataKeys)))
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

func isCertValid(path string, name, sk, pk string) {
	cmd := exec.Command("openssl", "x509", "-checkend", "86400", "-noout", "-in", path+"domain.cert.pem")
	b, _ := cmd.Output()
	fmt.Println(string(b))
	if !strings.Contains(string(b), "not") {
		fmt.Println("expired")
		dataKeys := fmt.Sprintf(`{"secretapikey": "%v", "apikey": "%v"}`, sk, pk)
		req, _ := http.NewRequest("POST", "https://porkbun.com/api/json/v3/ssl/retrieve/"+name, bytes.NewBuffer([]byte(dataKeys)))
		client := &http.Client{}
		resp, err := client.Do(req)
		CatchErr(err)
		bytes, err := io.ReadAll(resp.Body)
		CatchErr(err)
		certs := Certs{}
		err = json.Unmarshal(bytes, &certs)
		CatchErr(err)
		if certs.Status != "SUCCESS" {
			fmt.Println("Failed getting certificates for "+name)
			return
		}

		os.Remove(path+"domain.cert.pem")
		file, err := os.Create(path+"domain.cert.pem")
		CatchErr(err)
		_, err = file.Write([]byte(certs.Cert))
		CatchErr(err)

		os.Remove(path+"intermediate.cert.pem")
		file, err = os.Create(path+"intermediate.cert.pem")
		CatchErr(err)
		_, err = file.Write([]byte(certs.Intermid))
		CatchErr(err)

		os.Remove(path+"private.key.pem")
		file, err = os.Create(path+"private.key.pem")
		CatchErr(err)
		_, err = file.Write([]byte(certs.Intermid))
		CatchErr(err)

		os.Remove(path+"public.key.pem")
		file, err = os.Create(path+"public.key.pem")
		CatchErr(err)
		_, err = file.Write([]byte(certs.Intermid))
		CatchErr(err)

		fmt.Println("Updated certificates")
	}
}

type Catalogue map[string]interface{}

type Certs struct {
	Status   string `json:"status"`
	Intermid string `json:"intermediatecertificate"`
	Cert     string `json:"certificatechain"`
	Private  string `json:"privatekey"`
	Public   string `json:"publickey"`
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
