package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/maddsua/proxyd/rpc"
	"github.com/maddsua/proxyd/rpc/model"
)

type RPCClientConfig struct {
	EndpointURL string `json:"endpoint_url" yaml:"endpoint_url"`
	SecretToken string `json:"secret_token" yaml:"secret_token"`
}

type Client struct {
	EndpointURL string
	Token       *rpc.InstanceToken
}

func (client *Client) Ready(ctx context.Context) error {
	return sendRPC(
		ctx,
		http.MethodGet,
		client.EndpointURL,
		model.ProcedureReady,
		client.Token,
		nil,
	)
}

func (client *Client) ReportStatus(ctx context.Context, params model.InstanceStatus) error {
	return sendRPC(
		ctx,
		http.MethodPost,
		client.EndpointURL,
		model.ProcedureStatus,
		client.Token,
		params,
	)
}

func (client *Client) ReportTraffic(ctx context.Context, params model.InstanceTrafficUpdate) error {
	return sendRPC(
		ctx,
		http.MethodPost,
		client.EndpointURL,
		model.ProcedureTraffic,
		client.Token,
		params,
	)
}

func (client *Client) GetProxyTable(ctx context.Context) (*model.ProxyTable, error) {
	return receiveRPC[model.ProxyTable](
		ctx,
		http.MethodGet,
		client.EndpointURL,
		model.ProcedureProxyTable,
		client.Token,
	)
}

type ClientError struct {
	Operation string
	Cause     string
}

func (err *ClientError) Error() string {
	return fmt.Sprintf("%s: %v", err.Operation, err.Cause)
}

func sendRPC(ctx context.Context, method string, endpointURL string, procedure string, token *rpc.InstanceToken, params any) error {
	_, err := exchangeRPC[any](ctx, method, endpointURL, procedure, token, params)
	return err
}

func receiveRPC[R any](ctx context.Context, method string, endpointURL string, procedure string, token *rpc.InstanceToken) (*R, error) {
	data, err := exchangeRPC[R](ctx, method, endpointURL, procedure, token, nil)
	if data == nil && err == nil {
		return nil, fmt.Errorf("empty data response")
	}
	return data, err
}

func exchangeRPC[R any](ctx context.Context, method string, endpointURL string, procedure string, token *rpc.InstanceToken, params any) (*R, error) {

	var bodyReader io.ReadWriter
	if params != nil {
		bodyReader = &bytes.Buffer{}
		if err := json.NewEncoder(bodyReader).Encode(params); err != nil {
			return nil, &ClientError{
				Operation: "encode request body",
				Cause:     err.Error(),
			}
		}
	}

	req, err := http.NewRequest(method, endpointURL, bodyReader)
	if err != nil {
		return nil, &ClientError{
			Operation: "create http request",
			Cause:     err.Error(),
		}
	}

	req.URL.Path = procedureEndpointUrl(req.URL.Path, procedure)

	if token != nil {
		req.Header.Set("Authorization", "Bearer "+token.String())
	}

	if bodyReader != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := http.DefaultClient.Do(req.WithContext(ctx))
	if err != nil {

		if urlerr, ok := err.(*url.Error); ok {
			err = urlerr.Err
		}

		return nil, &ClientError{
			Operation: "rest",
			Cause:     err.Error(),
		}
	}

	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNoContent {
		return nil, nil
	}

	if contentType := resp.Header.Get("Content-Type"); !strings.Contains(contentType, "json") {

		if contentType == "" {
			contentType = "<unknown>"
		}

		return nil, &ClientError{
			Operation: "decode response",
			Cause:     fmt.Sprintf("received a non-json response (%s; status %d)", contentType, resp.StatusCode),
		}
	}

	var result model.Result[R]
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, &ClientError{
			Operation: "decode response",
			Cause:     err.Error(),
		}
	}

	if resp.StatusCode >= http.StatusBadRequest && (result.Error == nil || result.Error.Message == "") {
		return nil, &ClientError{
			Operation: "decode response",
			Cause:     fmt.Sprintf("received a non-ok status code (%d) but no error", resp.StatusCode),
		}
	} else if resp.StatusCode != http.StatusNoContent && result.Data == nil {
		return nil, &ClientError{
			Operation: "decode response",
			Cause:     fmt.Sprintf("received a non-empty status code (%d) but no data", resp.StatusCode),
		}
	}

	if result.Error != nil {
		return nil, result.Error
	}

	return result.Data, nil
}

func procedureEndpointUrl(base, procedure string) string {
	return fmt.Sprintf("%s/proxyd/rpc/v1/%s",
		strings.TrimRight(base, "\\/"),
		procedure,
	)
}
