package socks5

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"

	"github.com/maddsua/proxyd"
	"github.com/maddsua/proxyd/utils"
)

//	Referencing: https://datatracker.ietf.org/doc/html/rfc1929

type PasswordAuthStatus byte

const (
	PasswordAuthVersion = byte(1)

	PasswordAuthStatusSuccess    = PasswordAuthStatus(0x00)
	PasswordAuthStatusFail       = PasswordAuthStatus(0x01)
	PasswordAuthStatusSystemFail = PasswordAuthStatus(0x02)
)

type PasswordRequest struct {
	Username, Password string
}

func NewPasswordReply(status PasswordAuthStatus) *PasswordReply {
	return &PasswordReply{Status: status}
}

func ReadPasswordRequest(reader io.Reader) (*PasswordRequest, error) {

	var ulen int
	if buff, err := utils.ReadN(reader, 2); err != nil {
		return nil, fmt.Errorf("read ulen: %v", err)
	} else if version := buff[0]; version != PasswordAuthVersion {
		return nil, fmt.Errorf("protocol error: invalid version: %v", version)
	} else if ulen = int(buff[1]); ulen <= 0 {
		return nil, fmt.Errorf("protocol error: invalid ulen: %v", ulen)
	}

	//	read username content and plen together to reduce the number of read calls
	username, err := utils.ReadN(reader, int(ulen)+1)
	if err != nil {
		return nil, fmt.Errorf("read username: %v", err)
	}

	//	get password length from the last byte of the username slice
	plen := int(username[len(username)-1])
	if plen <= 0 {
		return nil, fmt.Errorf("protocol error: invalid plen: %v", ulen)
	}

	password, err := utils.ReadN(reader, plen)
	if err != nil {
		return nil, fmt.Errorf("read password: %v", err)
	}

	return &PasswordRequest{
		//	cut the plen byte off of the userame
		Username: string(username[:len(username)-1]),
		Password: string(password),
	}, nil
}

type PasswordReply struct {
	Status PasswordAuthStatus
}

func (repl *PasswordReply) Write(writer io.Writer) (int64, error) {
	var buff bytes.Buffer
	buff.WriteByte(PasswordAuthVersion)
	buff.WriteByte(byte(repl.Status))
	return io.Copy(writer, &buff)
}

func AuthenticateConnectionWithPassword(ctx context.Context, conn net.Conn, auth proxyd.ProxyAuthenticator) (*proxyd.ProxySession, error) {

	req, err := ReadPasswordRequest(conn)
	if err != nil {
		return nil, fmt.Errorf("read password negotiation request: %v", err)
	}

	status := PasswordAuthStatusSuccess

	clientIP, _ := utils.SplitIPPort(conn.RemoteAddr())

	sess, err := auth.AuthenticateWithPassword(ctx, conn.LocalAddr(), clientIP, req.Username, req.Password)
	if err != nil {
		if _, ok := err.(*proxyd.ProxyCredentialsError); ok {
			status = PasswordAuthStatusFail
		} else {
			status = PasswordAuthStatusSystemFail
		}
	}

	if _, err := NewPasswordReply(status).Write(conn); err != nil {
		return nil, fmt.Errorf("write password negotiation reply: %v", err)
	}

	return sess, err
}
