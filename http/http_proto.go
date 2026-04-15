package http

import (
	"encoding/base64"
	"errors"
	"fmt"
	"math"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

type UserInfo struct {
	Username, Password string
}

func ProxyRequestCredentials(req *http.Request) (*UserInfo, error) {

	proxyAuth := req.Header.Get("Proxy-Authorization")
	if proxyAuth == "" {
		return nil, nil
	}

	schema, token, _ := strings.Cut(proxyAuth, " ")
	if !strings.EqualFold(schema, "Basic") {
		return nil, fmt.Errorf("invalid auth schema '%s'", schema)
	}

	userauth, err := base64.StdEncoding.DecodeString(token)
	if err != nil {
		return nil, err
	}

	username, password, _ := strings.Cut(string(userauth), ":")
	if username == "" {
		return nil, errors.New("username is empty")
	} else if password == "" {
		return nil, errors.New("password is empty")
	}

	return &UserInfo{
		Username: username,
		Password: password,
	}, nil
}

// Evaluates destination address of a proxy request
// Returned format is 'host:port', where host can be either an IP address or a hostname
func ProxyDestinationAddr(req *http.Request) (string, error) {

	var destAddr = func(addr string) (string, error) {

		host, port, err := net.SplitHostPort(addr)
		if err != nil {
			return "", fmt.Errorf("invalid address: %v", err)
		}

		if hostNorm := strings.TrimSpace(host); hostNorm == "" || hostNorm != host {
			return "", fmt.Errorf("invalid host '%v'", host)
		}

		portNumber, err := strconv.Atoi(port)
		if err != nil || portNumber <= 0 || portNumber >= math.MaxUint16 {
			return "", fmt.Errorf("invalid port '%v'", port)
		}

		return addr, nil
	}

	var forwardDest = func(dest string) (string, error) {
		if host, port, err := net.SplitHostPort(dest); err == nil {
			return destAddr(net.JoinHostPort(host, port))
		}
		return destAddr(net.JoinHostPort(dest, "80"))
	}

	if req.Method == http.MethodConnect {
		if !strings.Contains(req.RequestURI, "/") {
			return destAddr(req.RequestURI)
		}
		return destAddr(req.Host)
	}

	forwardUrl, err := url.Parse(req.RequestURI)
	if err != nil {
		return "", fmt.Errorf("invalid forward request url: %v", err)
	}

	if forwardUrl.Host != "" {
		return forwardDest(forwardUrl.Host)
	}

	return forwardDest(req.Host)
}

func ProxyClientIP(req *http.Request) net.IP {
	host, _, err := net.SplitHostPort(req.RemoteAddr)
	if err != nil {
		return nil
	}
	return net.ParseIP(host)
}
