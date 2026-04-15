package proxyd

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"net"
	"strconv"
	"sync"
	"time"

	"github.com/maddsua/proxyd/utils"
)

const DefaultDnsTimeout = 15 * time.Second
const DefaultDnsTestTimeout = 5 * time.Second
const DefaultDnsTestWarnTimeout = 2 * time.Second
const DefaultDnsTestResultTTL = time.Minute

type DNSLookupError struct {
	Query string
}

func (err *DNSLookupError) Error() string {
	return fmt.Sprintf("no results for '%s'", err.Query)
}

var DefaultDnsProbeNames = []string{
	"one.one.one.one",
	"google.com",
	"ripe.net",
	"icann.org",
}

type ProxyDNSResolver struct {
	ServerAddr string
	Dialer     net.Dialer
}

func (dns *ProxyDNSResolver) ServerName() string {

	addr := dns.ServerAddr
	if host, _, err := net.SplitHostPort(addr); err == nil {
		return host
	} else if addr != "" {
		return addr
	}

	return "<default>"
}

func (dns *ProxyDNSResolver) Resolver() *net.Resolver {
	serverAddr, err := dnsServerAddr(dns.ServerAddr)
	if err != nil {
		return net.DefaultResolver
	}
	return resolverWithDialAddr(dns.Dialer, serverAddr)
}

func (dns *ProxyDNSResolver) ResolveDestination(ctx context.Context, ipnetwork, addr string) (string, error) {

	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return "", fmt.Errorf("invalid destination addr: %v", err)
	}

	ipaddr, err := dns.LookupIpNetwork(ctx, ipnetwork, host)
	if err != nil {
		return "", err
	}

	return net.JoinHostPort(ipaddr.String(), port), nil
}

func (dns *ProxyDNSResolver) LookupIpNetwork(ctx context.Context, ipnetwork, host string) (hostIP net.IP, err error) {

	switch ipnetwork {
	case "ip", "ip4", "ip6":
	default:
		return nil, fmt.Errorf("invalid ip network: '%v'", ipnetwork)
	}

	if hostIP = net.ParseIP(host); hostIP == nil {

		ctx, cancel := context.WithTimeout(ctx, DefaultDnsTimeout)
		defer cancel()

		addrList, err := dns.Resolver().LookupIP(ctx, ipnetwork, host)
		if err != nil {
			if _, isAddrError := err.(*net.AddrError); ctx.Err() != nil || isAddrError {
				return nil, &DNSLookupError{Query: host}
			}
			return nil, err
		} else if len(addrList) == 0 {
			return nil, &DNSLookupError{Query: host}
		}

		hostIP = addrList[0]
	}

	switch ipnetwork {

	// enforce version match when a network is specified
	case "ip4", "ip6":
		if dstAddrVer := utils.IPNetwork(hostIP); dstAddrVer != ipnetwork {
			return nil, &IpVersionError{RemoteNet: dstAddrVer, LocalNet: ipnetwork}
		}
	}

	if err = DestinationIPAllowed(hostIP); err != nil {
		return nil, err
	}

	return hostIP, nil
}

func resolverWithDialAddr(dialer net.Dialer, dialAddr string) *net.Resolver {
	return &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
			return dialer.DialContext(ctx, network, dialAddr)
		},
	}
}

func dnsServerAddr(addr string) (string, error) {

	host, port, _ := net.SplitHostPort(addr)
	if port == "" {
		host = addr
		port = "53"
	} else if err := isValidDnsPort(port); err != nil {
		return "", err
	}

	if host == "" {
		return "", fmt.Errorf("empty DNS host")
	}

	return net.JoinHostPort(host, port), nil
}

func isValidDnsPort(port string) error {

	portNumber, _ := strconv.Atoi(port)
	if portNumber <= 1 || portNumber >= math.MaxUint16 {
		return fmt.Errorf("invalid DNS port: '%v'", port)
	}

	return nil
}

type IpVersionError struct {
	RemoteNet, LocalNet string
}

func (err *IpVersionError) Error() string {
	return fmt.Sprintf("ip version mismatch: %v-->%v", err.LocalNet, err.RemoteNet)
}

func DestinationIPAllowed(ip net.IP) error {

	if ip.IsLoopback() || ip.IsPrivate() || ip.IsUnspecified() {
		return &NetworkPolicyError{
			Message: "local destination addresses not allowed",
		}
	}

	return nil
}

type NetworkPolicyError struct {
	Message string
}

func (err *NetworkPolicyError) Error() string {
	if err.Message != "" {
		return err.Message
	}
	return "forbidden by the network policy"
}

type DNSTester struct {
	ResultTTL time.Duration
	Dialer    net.Dialer
	Control   func(server string) error

	entries utils.ExpireMap[error]
}

func (dt *DNSTester) Test(ctx context.Context, addr string) error {

	ttl := dt.ResultTTL
	if ttl < time.Second {
		ttl = DefaultDnsTestResultTTL
	}

	if entry := dt.entries.Get(addr); entry != nil {
		return entry.Val
	}

	ctx, cancel := context.WithTimeout(ctx, DefaultDnsTestTimeout)
	defer cancel()

	resultChan := make(chan error, 1)

	go func() {

		if dt.Control != nil {
			if err := dt.Control(addr); err != nil {
				resultChan <- err
				return
			}
		}

		resultChan <- dt.LookupTestRecords(ctx, addr, nil)
	}()

	warnTimer := time.After(DefaultDnsTestWarnTimeout)

	for {
		select {
		case <-warnTimer:
			deadline, _ := ctx.Deadline()
			slog.Warn("DNS server test takes longer than expected",
				slog.String("addr", addr),
				slog.Duration("max_wait", time.Until(deadline)))
		case result := <-resultChan:
			return dt.entries.Set(addr, result, ttl).Val
		}
	}
}

func (state *DNSTester) LookupTestRecords(ctx context.Context, serverAddr string, names []string) error {

	serverAddr, err := dnsServerAddr(serverAddr)
	if err != nil {
		return fmt.Errorf("invalid server address: %v", err)
	}

	resolver := resolverWithDialAddr(state.Dialer, serverAddr)

	if len(names) == 0 {
		names = DefaultDnsProbeNames
	}

	var wg sync.WaitGroup
	wg.Add(len(names))

	errChan := make(chan error, len(names)+1)

	for _, name := range names {
		go func() {
			if _, err := resolver.LookupNS(ctx, name); err == nil {
				errChan <- err
			}
			wg.Done()
		}()
	}

	go func() {
		wg.Wait()
		errChan <- fmt.Errorf("dns server unreachable")
	}()

	return <-errChan
}
