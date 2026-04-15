package socks5_test

import (
	"bytes"
	"net"
	"testing"

	"github.com/maddsua/proxyd/socks/socks5"
)

func Test_MethodReply_Password(t *testing.T) {

	expectRaw := []byte{
		0x05,
		0x02,
	}

	buff, err := socks5.NewMethodReply(socks5.AuthMethodPassword).MarshalBinary()
	if err != nil {
		t.Fatalf("marshal: %v", err)
		return
	}

	if !bytes.Equal(buff, expectRaw) {
		t.Fatalf("unexpected binary value:\nwant: -> %v\nhave: -> %v", expectRaw, buff)
	}
}

func Test_ReadRequest_Connect(t *testing.T) {

	raw := []byte{
		0x05,
		0x01,
		0x00,
		0x01, 1, 1, 1, 1,
		0x00, 80,
	}

	const expectString = "CONNECT 1.1.1.1:80"

	req, err := socks5.ReadRequest(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("read: %v", err)
		return
	}

	if val := req.String(); val != expectString {
		t.Fatalf("unexpected binary value: \nwant: '%v' \nhave: '%v'", expectString, val)
		return
	}
}

func Test_ReadRequest_Bind(t *testing.T) {

	raw := []byte{
		0x05,
		0x02,
		0x00,
		0x01, 127, 0, 0, 1,
		0x00, 54,
	}

	const expectString = "BIND 127.0.0.1:54"

	req, err := socks5.ReadRequest(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("read: %v", err)
		return
	}

	if val := req.String(); val != expectString {
		t.Fatalf("unexpected binary value: \nwant: '%v' \nhave: '%v'", expectString, val)
		return
	}
}

func Test_HostAddr_Parse_IP4_1(t *testing.T) {

	raw := []byte{
		0x01,
		23, 158, 72, 62,
		0x00, 80,
	}

	const expectString = "23.158.72.62:80"

	addr, err := socks5.ReadHostAddr(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("read: %v", err)
		return
	}

	if val := addr.String(); val != expectString {
		t.Fatalf("unexpected binary value: \nwant: '%v' \nhave: '%v'", expectString, val)
		return
	}
}

func Test_HostAddr_Parse_IP6_1(t *testing.T) {

	raw := []byte{
		0x04,
		0x26, 0x02, 0x02, 0x94, 0x00, 0x00, 0x00, 0x3e, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0xac, 0xab,
		0x00, 80,
	}

	const expectString = "[2602:294:0:3e::acab]:80"

	addr, err := socks5.ReadHostAddr(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("read: %v", err)
		return
	}

	if val := addr.String(); val != expectString {
		t.Fatalf("unexpected binary value: \nwant: '%v' \nhave: '%v'", expectString, val)
		return
	}
}

func Test_HostAddr_Parse_Hostname_1(t *testing.T) {

	raw := []byte{
		0x03,
		8,
		'm', 'y', 'i', 'p', '.', 'w', 't', 'f',
		0x00, 80,
	}

	const expectString = "myip.wtf:80"

	addr, err := socks5.ReadHostAddr(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("read: %v", err)
		return
	}

	if val := addr.String(); val != expectString {
		t.Fatalf("unexpected binary value: \nwant: '%v' \nhave: '%v'", expectString, val)
		return
	}
}

func Test_HostAddr_Serialize_Empty(t *testing.T) {

	expectRaw := []byte{
		0x01,
		0, 0, 0, 0,
		0x00, 0x00,
	}

	addr := socks5.HostAddr{}

	buff, err := addr.MarshalBinary()
	if err != nil {
		t.Fatalf("marshal: %v", err)
		return
	}

	if !bytes.Equal(buff, expectRaw) {
		t.Fatalf("unexpected binary value:\nwant: -> %v\nhave: -> %v", expectRaw, buff)
	}
}

func Test_HostAddr_Serialize_Nil(t *testing.T) {

	expectRaw := []byte{
		0x01,
		0, 0, 0, 0,
		0x00, 0x00,
	}

	var addr *socks5.HostAddr

	buff, err := addr.MarshalBinary()
	if err != nil {
		t.Fatalf("marshal: %v", err)
		return
	}

	if !bytes.Equal(buff, expectRaw) {
		t.Fatalf("unexpected binary value:\nwant: -> %v\nhave: -> %v", expectRaw, buff)
	}
}

func Test_HostAddr_Serialize_IP4_1(t *testing.T) {

	expectRaw := []byte{
		0x01,
		142, 250, 203, 206,
		0x00, 80,
	}

	addr := socks5.HostAddr{
		IP:   net.ParseIP("142.250.203.206"),
		Port: 80,
	}

	buff, err := addr.MarshalBinary()
	if err != nil {
		t.Fatalf("marshal: %v", err)
		return
	}

	if !bytes.Equal(buff, expectRaw) {
		t.Fatalf("unexpected binary value:\nwant: -> %v\nhave: -> %v", expectRaw, buff)
	}
}

func Test_HostAddr_Serialize_IP6_1(t *testing.T) {

	expectRaw := []byte{
		0x04,
		0x2a, 0x00, 0x14, 0x50, 0x40, 0x16, 0x08, 0x02, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x20, 0x0e,
		0x00, 80,
	}

	addr := socks5.HostAddr{
		IP:   net.ParseIP("2a00:1450:4016:802::200e"),
		Port: 80,
	}

	buff, err := addr.MarshalBinary()
	if err != nil {
		t.Fatalf("marshal: %v", err)
		return
	}

	if !bytes.Equal(buff, expectRaw) {
		t.Fatalf("unexpected binary value:\nwant: -> %v\nhave: -> %v", expectRaw, buff)
	}
}

func Test_HostAddr_Serialize_Hostname_1(t *testing.T) {

	expectRaw := []byte{
		0x03,
		10, 'g', 'o', 'o', 'g', 'l', 'e', '.', 'c', 'o', 'm',
		0x00, 80,
	}

	addr := socks5.HostAddr{
		Hostname: "google.com",
		Port:     80,
	}

	buff, err := addr.MarshalBinary()
	if err != nil {
		t.Fatalf("marshal: %v", err)
		return
	}

	if !bytes.Equal(buff, expectRaw) {
		t.Fatalf("unexpected binary value:\nwant: -> %v\nhave: -> %v", expectRaw, buff)
	}
}

func Test_Reply_NotAllowed(t *testing.T) {

	expectRaw := []byte{
		0x05,
		0x02,
		0x00,
		0x01, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x00,
	}

	buff, err := socks5.NewReply(socks5.ReplyCodeConnectionNotAllowed, nil).MarshalBinary()
	if err != nil {
		t.Fatalf("marshal: %v", err)
		return
	}

	if !bytes.Equal(buff, expectRaw) {
		t.Fatalf("unexpected binary value:\nwant: -> %v\nhave: -> %v", expectRaw, buff)
	}
}

func Test_Reply_NotAllowed_WithAddr(t *testing.T) {

	expectRaw := []byte{
		0x05,
		0x05,
		0x00,
		0x01, 127, 0, 0, 1,
		0x00, 80,
	}

	buff, err := socks5.NewReply(socks5.ReplyCodeConnectionRefused, &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 80}).MarshalBinary()
	if err != nil {
		t.Fatalf("marshal: %v", err)
		return
	}

	if !bytes.Equal(buff, expectRaw) {
		t.Fatalf("unexpected binary value:\nwant: -> %v\nhave: -> %v", expectRaw, buff)
	}
}
