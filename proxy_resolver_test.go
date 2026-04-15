package proxyd_test

import (
	"context"
	"testing"
	"time"

	"github.com/maddsua/proxyd"
)

func Test_DnsTestCache_Single(t *testing.T) {

	var totalCalls int

	tester := proxyd.DNSTester{
		ResultTTL: time.Second,
		Control: func(server string) error {
			totalCalls++
			return nil
		},
	}

	for range 2 {
		if err := tester.Test(context.Background(), "1.1.1.1"); err != nil {
			t.Fatal("didn't expect the cloudflare dns to fail, tbh; err:", err)
		}
	}

	if totalCalls != 1 {
		t.Fatal("invalid number of dns calls", totalCalls)
	}
}

func Test_DnsTestCache_Multiple(t *testing.T) {

	var totalCalls int

	tester := proxyd.DNSTester{
		ResultTTL: time.Second,
		Control: func(server string) error {
			totalCalls++
			return nil
		},
	}

	for range 2 {
		if err := tester.Test(context.Background(), "1.1.1.1"); err != nil {
			t.Fatal("didn't expect the cloudflare dns to fail, tbh; err:", err)
		}
	}

	for range 4 {
		if err := tester.Test(context.Background(), "8.8.8.8"); err != nil {
			t.Fatal("didn't expect the cloudflare dns to fail, tbh; err:", err)
		}
	}

	if totalCalls != 2 {
		t.Fatal("invalid number of dns calls", totalCalls)
	}
}

func Test_DnsTestCache_Expire_1(t *testing.T) {

	var totalCalls int

	tester := proxyd.DNSTester{
		ResultTTL: time.Second,
		Control: func(server string) error {
			totalCalls++
			return nil
		},
	}

	for range 5 {
		if err := tester.Test(context.Background(), "1.1.1.1"); err != nil {
			t.Fatal("didn't expect the cloudflare dns to fail, tbh; err:", err)
		}
	}

	t.Log("faking a delay so that cached entries can expire")
	time.Sleep(750 * time.Millisecond)

	for range 4 {
		if err := tester.Test(context.Background(), "1.1.1.1"); err != nil {
			t.Fatal("didn't expect the cloudflare dns to fail, tbh; err:", err)
		}
	}

	if totalCalls != 1 {
		t.Fatal("invalid number of dns calls", totalCalls)
	}
}

func Test_DnsTestCache_Expire_2(t *testing.T) {

	var totalCalls int

	tester := proxyd.DNSTester{
		ResultTTL: time.Second,
		Control: func(server string) error {
			totalCalls++
			return nil
		},
	}

	for range 5 {
		if err := tester.Test(context.Background(), "1.1.1.1"); err != nil {
			t.Fatal("didn't expect the cloudflare dns to fail, tbh; err:", err)
		}
	}

	t.Log("faking a delay so that cached entries can expire")
	time.Sleep(2 * time.Second)

	for range 4 {
		if err := tester.Test(context.Background(), "1.1.1.1"); err != nil {
			t.Fatal("didn't expect the cloudflare dns to fail, tbh; err:", err)
		}
	}

	if totalCalls != 2 {
		t.Fatal("invalid number of dns calls", totalCalls)
	}
}
