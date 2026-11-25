// Package host is this package's side of the plugin v2 line protocol
// (PLUGINS.md §4): one request line in, one answer line out, and the host's
// closed services in between. Only the two services the player needs are
// implemented here: the host proxy for HTTPS requests and the package's
// private storage. The runtime never opens a socket or a file on its own.
package host

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"strconv"
)

// maxCalls mirrors the host's per-invocation ceiling. Checking it here turns
// a loop in this runtime into an actionable error instead of a host refusal
// halfway through a page.
const maxCalls = 16

// Client is one invocation's conversation with the host. Calls are serial:
// the protocol never has two host calls in flight.
type Client struct {
	input  *bufio.Reader
	output *bufio.Writer
	calls  int
}

// NewClient wraps the invocation's streams.
func NewClient(input io.Reader, output io.Writer) *Client {
	return &Client{
		input:  bufio.NewReaderSize(input, 1<<20),
		output: bufio.NewWriterSize(output, 1<<20),
	}
}

// ReadRequestLine reads the invocation's one request line. It must use this
// reader: the buffered bytes belong to the same host conversation.
func (c *Client) ReadRequestLine() ([]byte, error) {
	line, err := c.input.ReadBytes('\n')
	if err != nil && len(line) == 0 {
		return nil, errors.New("没有收到宿主请求")
	}
	return line, nil
}

// WriteLine writes one line and flushes it.
func (c *Client) WriteLine(line []byte) error {
	if _, err := c.output.Write(append(line, '\n')); err != nil {
		return errors.New("无法写入宿主响应")
	}
	return c.output.Flush()
}

type hostCall struct {
	Type    string `json:"type"`
	ID      string `json:"id"`
	Service string `json:"service"`
	Method  string `json:"method,omitempty"`
	Params  any    `json:"params,omitempty"`
}

type hostCallResult struct {
	Type   string          `json:"type"`
	ID     string          `json:"id"`
	OK     bool            `json:"ok"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  string          `json:"error,omitempty"`
}

func (c *Client) call(ctx context.Context, service, method string, params any) (json.RawMessage, error) {
	if err := ctx.Err(); err != nil {
		return nil, errors.New("操作已超时")
	}
	if c.calls >= maxCalls {
		return nil, errors.New("一次操作里的宿主调用过多")
	}
	c.calls++
	call := hostCall{
		Type: hostCallType, ID: "c" + strconv.Itoa(c.calls),
		Service: service, Method: method, Params: params,
	}
	line, err := json.Marshal(call)
	if err != nil {
		return nil, errors.New("无法编码宿主调用")
	}
	if err := c.WriteLine(line); err != nil {
		return nil, err
	}
	for {
		response, err := c.input.ReadBytes('\n')
		if err != nil {
			return nil, errors.New("宿主调用没有响应")
		}
		var result hostCallResult
		if err := json.Unmarshal(response, &result); err != nil {
			return nil, errors.New("宿主调用响应无法读取")
		}
		if result.Type != hostCallResultType {
			return nil, errors.New("宿主返回了意外的消息")
		}
		if result.ID != call.ID {
			continue
		}
		if !result.OK {
			if result.Error == "" {
				result.Error = "宿主调用失败"
			}
			return nil, errors.New(result.Error)
		}
		return result.Result, nil
	}
}

const (
	hostCallType       = "hostCall"
	hostCallResultType = "hostCallResult"
)

// NetworkRequest is one HTTPS request to a host the manifest declared.
type NetworkRequest struct {
	URL     string            `json:"url"`
	Method  string            `json:"method,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
	Body    string            `json:"body,omitempty"`
}

// NetworkResponse is the bounded answer the host proxy hands back.
type NetworkResponse struct {
	Status int    `json:"status"`
	Body   string `json:"body"`
}

// Network performs one HTTPS request to a host the manifest declared.
func (c *Client) Network(ctx context.Context, request NetworkRequest) (NetworkResponse, error) {
	raw, err := c.call(ctx, "network.outbound", "", request)
	if err != nil {
		return NetworkResponse{}, err
	}
	var response NetworkResponse
	if err := json.Unmarshal(raw, &response); err != nil {
		return NetworkResponse{}, errors.New("无法解析网络回答")
	}
	return response, nil
}

// Fetch adapts the proxy to the narrow fetcher the music sources use. The
// Content-Type header is attached here because it is the one header the host
// lets a plugin set for a body-carrying request.
func (c *Client) Fetch(ctx context.Context, method, url string, body []byte) (int, []byte, error) {
	request := NetworkRequest{URL: url, Method: method}
	if len(body) > 0 {
		request.Body = string(body)
		request.Headers = map[string]string{"Content-Type": "application/json"}
	}
	response, err := c.Network(ctx, request)
	if err != nil {
		return 0, nil, err
	}
	return response.Status, []byte(response.Body), nil
}

// ErrMissingKey means the key has never been written, so the caller can fall
// back to defaults instead of failing the page.
var ErrMissingKey = errors.New("存储项不存在")

// StorageRead reads one key from the package's private directory.
func (c *Client) StorageRead(ctx context.Context, key string) ([]byte, error) {
	raw, err := c.call(ctx, "storage.plugin", "read", map[string]string{"key": key})
	if err != nil {
		if err.Error() == ErrMissingKey.Error() {
			return nil, ErrMissingKey
		}
		return nil, err
	}
	var file struct {
		Data string `json:"data"`
	}
	if err := json.Unmarshal(raw, &file); err != nil {
		return nil, errors.New("无法解析存储内容")
	}
	data, err := base64.StdEncoding.DecodeString(file.Data)
	if err != nil {
		return nil, errors.New("存储内容不是有效的 base64")
	}
	return data, nil
}

// StorageWrite replaces one key in the package's private directory.
func (c *Client) StorageWrite(ctx context.Context, key string, data []byte) error {
	_, err := c.call(ctx, "storage.plugin", "write", map[string]string{
		"key":  key,
		"data": base64.StdEncoding.EncodeToString(data),
	})
	return err
}
