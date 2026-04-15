package radiuspkg

import (
	"context"
	"errors"
	"fmt"

	radius "github.com/maddsua/layeh-radius"
	"github.com/maddsua/layeh-radius/rfc2866"
	"github.com/maddsua/layeh-radius/rfc3576"
	"github.com/maddsua/proxyd"
)

type Client struct {
	AuthAddr string
	AcctAddr string
	DacAddr  string
	Secret   string
}

func (auth *Client) Authorize(ctx context.Context, params AuthorizationParams) (*PeerAuthorization, error) {

	addr := auth.AuthAddr
	if addr == "" {
		return nil, errors.New("empty auth addr")
	}

	req := radius.New(radius.CodeAccessRequest, []byte(auth.Secret))

	if err := params.ToPacket(req); err != nil {
		return nil, err
	}

	reply, err := radius.Exchange(ctx, req, auth.AuthAddr)
	if err != nil {
		return nil, err
	}

	switch reply.Code {
	case radius.CodeAccessReject:
		return nil, &proxyd.ProxyCredentialsError{}
	case radius.CodeAccessAccept:
		return PeerAuthFromPacket(reply), nil
	default:
		return nil, fmt.Errorf("unexpected reply code: %v", reply.Code)
	}
}

func (auth *Client) AccountTraffic(ctx context.Context, params AccountingParams) error {

	req := radius.New(radius.CodeAccountingRequest, []byte(auth.Secret))

	if err := params.ToPacket(req); err != nil {
		return err
	}

	addr := auth.AcctAddr
	if addr == "" {
		addr = auth.AuthAddr
	}

	if addr == "" {
		return errors.New("empty auth/acct addr")
	}

	reply, err := radius.Exchange(ctx, req, addr)
	if err != nil {
		return err
	}

	if reply.Code != radius.CodeAccountingResponse {
		return fmt.Errorf("unexpected reply code: %v", reply.Code)
	}

	return nil
}

func (auth *Client) SendDM(accountingSessionID string) error {

	addr := auth.DacAddr
	if addr == "" {
		return errors.New("empty dac addr")
	}

	packet := radius.New(radius.CodeDisconnectRequest, []byte(auth.Secret))

	if err := rfc2866.AcctSessionID_SetString(packet, accountingSessionID); err != nil {
		return fmt.Errorf("rfc2866.AcctSessionID_SetString: %v", err)
	}

	reply, err := radius.Exchange(context.Background(), packet, addr)
	if err != nil {
		return err
	}

	switch reply.Code {
	case radius.CodeDisconnectACK:
		return nil
	case radius.CodeDisconnectNAK:
		return fmt.Errorf("DM request rejected: %v", rfc3576.ErrorCause_Get(reply))
	default:
		return fmt.Errorf("unexpected reply code: %v", reply.Code)
	}
}

func (auth *Client) SendCoA(peer *PeerAuthorization) error {

	addr := auth.DacAddr
	if addr == "" {
		return errors.New("empty dac addr")
	}

	packet := radius.New(radius.CodeCoARequest, []byte(auth.Secret))

	if err := peer.ToPacket(packet); err != nil {
		return err
	}

	reply, err := radius.Exchange(context.Background(), packet, addr)
	if err != nil {
		return err
	}

	switch reply.Code {
	case radius.CodeCoAACK:
		return nil
	case radius.CodeCoANAK:
		return fmt.Errorf("CoA request rejected: %v", rfc3576.ErrorCause_Get(reply))
	default:
		return fmt.Errorf("unexpected reply code: %v", reply.Code)
	}
}
