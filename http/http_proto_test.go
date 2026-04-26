package http_test

import (
	"bufio"
	"net/http"
	"strings"
	"testing"

	http_pkg "github.com/maddsua/proxyd/http"
)

func Test_CredentialsParse_1(t *testing.T) {

	rawRequest := strings.Join([]string{
		`CONNECT server.example.com:80 HTTP/1.1`,
		`proxy-authorization: basic bWFkZHN1YTo6VktJP09tdml7VyMwKF5Q`,
	}, "\r\n") + "\r\n\r\n"

	req, err := http.ReadRequest(bufio.NewReader(strings.NewReader(rawRequest)))
	if err != nil {
		t.Fatalf("parse: %v", err)
		return
	}

	userinfo, err := http_pkg.RequestCredentials(req)
	if err != nil {
		t.Fatalf("parse: %v", err)
		return
	}

	if userinfo == nil {
		t.Fatalf("no credentaisl detected")
		return
	}

	if userinfo.Username != `maddsua` {
		t.Fatalf("unexpected username: '%v'", userinfo.Username)
		return
	}

	if userinfo.Password != `:VKI?Omvi{W#0(^P` {
		t.Fatalf("unexpected password: '%v'", userinfo.Password)
		return
	}
}

func Test_CredentialsParse_2(t *testing.T) {

	rawRequest := strings.Join([]string{
		`CONNECT server.example.com:80 HTTP/1.1`,
		`proxy-authorization: Basic bWFkZHN1YTokMSNnM3NoO2JlaGViOW1mXnJAYzph`,
	}, "\r\n") + "\r\n\r\n"

	req, err := http.ReadRequest(bufio.NewReader(strings.NewReader(rawRequest)))
	if err != nil {
		t.Fatalf("parse: %v", err)
		return
	}

	userinfo, err := http_pkg.RequestCredentials(req)
	if err != nil {
		t.Fatalf("parse: %v", err)
		return
	}

	if userinfo == nil {
		t.Fatalf("no credentaisl detected")
		return
	}

	if userinfo.Username != `maddsua` {
		t.Fatalf("unexpected username: '%v'", userinfo.Username)
		return
	}

	if userinfo.Password != `$1#g3sh;beheb9mf^r@c:a` {
		t.Fatalf("unexpected password: '%v'", userinfo.Password)
		return
	}
}

func Test_ConnectDestinationParse_1(t *testing.T) {

	rawRequest := strings.Join([]string{
		`CONNECT one.one.one.one:443 HTTP/1.1`,
		`Host: one.one.one.one:443`,
		`Proxy-Authorization: basic amltbXk6MTIzNDU=`,
	}, "\r\n") + "\r\n\r\n"

	req, err := http.ReadRequest(bufio.NewReader(strings.NewReader(rawRequest)))
	if err != nil {
		t.Fatalf("parse: %v", err)
		return
	}

	dest, err := http_pkg.DestinationAddr(req)
	if err != nil {
		t.Fatalf("destination: %v", err)
		return
	}

	if dest != `one.one.one.one:443` {
		t.Fatalf("unexpected destination: '%v'", dest)
		return
	}
}

func Test_ConnectDestinationParse_2(t *testing.T) {

	rawRequest := strings.Join([]string{
		`CONNECT one.one.one.one:443 HTTP/1.1`,
		`Proxy-Authorization: basic amltbXk6MTIzNDU=`,
	}, "\r\n") + "\r\n\r\n"

	req, err := http.ReadRequest(bufio.NewReader(strings.NewReader(rawRequest)))
	if err != nil {
		t.Fatalf("parse: %v", err)
		return
	}

	dest, err := http_pkg.DestinationAddr(req)
	if err != nil {
		t.Fatalf("destination: %v", err)
		return
	}

	if dest != `one.one.one.one:443` {
		t.Fatalf("unexpected destination: '%v'", dest)
		return
	}
}

func Test_ForwardDestinationParse_1(t *testing.T) {

	rawRequest := strings.Join([]string{
		`GET http://one.one.one.one/ HTTP/1.1`,
		`Host: one.one.one.one:80`,
		`Proxy-Authorization: basic amltbXk6MTIzNDU=`,
		`Proxy-Connection: keep-alive`,
		`User-Agent: Mozilla/5.0 (Windows NT 10.0; Win64; x64)`,
		`Accept: text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8`,
		`Accept-Encoding: gzip, deflate`,
		`Connection: keep-alive`,
	}, "\r\n") + "\r\n\r\n"

	req, err := http.ReadRequest(bufio.NewReader(strings.NewReader(rawRequest)))
	if err != nil {
		t.Fatalf("parse: %v", err)
		return
	}

	dest, err := http_pkg.DestinationAddr(req)
	if err != nil {
		t.Fatalf("destination: %v", err)
		return
	}

	if dest != `one.one.one.one:80` {
		t.Fatalf("unexpected destination: '%v'", dest)
		return
	}
}

func Test_ForwardDestinationParse_2(t *testing.T) {

	rawRequest := strings.Join([]string{
		`GET http://one.one.one.one/ HTTP/1.1`,
		`Proxy-Authorization: basic amltbXk6MTIzNDU=`,
		`Proxy-Connection: keep-alive`,
		`User-Agent: Mozilla/5.0 (Windows NT 10.0; Win64; x64)`,
		`Accept: text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8`,
		`Accept-Encoding: gzip, deflate`,
		`Connection: keep-alive`,
	}, "\r\n") + "\r\n\r\n"

	req, err := http.ReadRequest(bufio.NewReader(strings.NewReader(rawRequest)))
	if err != nil {
		t.Fatalf("parse: %v", err)
		return
	}

	dest, err := http_pkg.DestinationAddr(req)
	if err != nil {
		t.Fatalf("destination: %v", err)
		return
	}

	if dest != `one.one.one.one:80` {
		t.Fatalf("unexpected destination: '%v'", dest)
		return
	}
}

func Test_ForwardDestinationParse_3(t *testing.T) {

	rawRequest := strings.Join([]string{
		`GET / HTTP/1.1`,
		`Host: one.one.one.one`,
		`Proxy-Authorization: basic amltbXk6MTIzNDU=`,
		`User-Agent: Mozilla/5.0 (Windows NT 10.0; Win64; x64)`,
		`Accept: text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8`,
		`Accept-Encoding: gzip, deflate`,
		`Connection: keep-alive`,
	}, "\r\n") + "\r\n\r\n"

	req, err := http.ReadRequest(bufio.NewReader(strings.NewReader(rawRequest)))
	if err != nil {
		t.Fatalf("parse: %v", err)
		return
	}

	dest, err := http_pkg.DestinationAddr(req)
	if err != nil {
		t.Fatalf("destination: %v", err)
		return
	}

	if dest != `one.one.one.one:80` {
		t.Fatalf("unexpected destination: '%v'", dest)
		return
	}
}
