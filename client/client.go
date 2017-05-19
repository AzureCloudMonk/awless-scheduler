package client

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type Client struct {
	ServiceURL  *url.URL
	serviceInfo *ServiceInfo
	httpClient  *http.Client
}

type ServiceInfo struct {
	Uptime       string
	ServiceAddr  string
	UnixSockMode bool
}

func New(discoveryURL string) (*Client, error) {
	httpClient := &http.Client{Timeout: 3 * time.Second}
	resp, err := httpClient.Get(discoveryURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	v := &ServiceInfo{}
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("cannot read body at discovery enpoint '%s', status code %s. Error: %s", discoveryURL, resp.Status, err)
	}
	if err := json.Unmarshal(body, v); err != nil {
		return nil, fmt.Errorf("cannot unmarshal json at discovery enpoint '%s': %s. Body was:\n%s", discoveryURL, err, body)
	}

	if v.UnixSockMode {
		return newUnixSock(v.ServiceAddr), nil
	}

	addr, err := url.Parse(v.ServiceAddr)
	if err != nil {
		return nil, fmt.Errorf("cannot parse scheduler service addr %s: %s", v.ServiceAddr, err)
	}

	return &Client{
		ServiceURL:  addr,
		httpClient:  httpClient,
		serviceInfo: v,
	}, nil
}

func newUnixSock(u string) *Client {
	return &Client{
		ServiceURL: &url.URL{Host: "unixsock", Scheme: "http"}, // context info only
		httpClient: &http.Client{
			Timeout: 3 * time.Second,
			Transport: &http.Transport{
				DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
					return net.Dial("unix", u)
				},
			},
		},
	}
}

type Task struct {
	Content  string
	RunAt    time.Time
	RevertAt time.Time
	Region   string
}

type Form struct {
	Region, RunIn, RevertIn string
	Template                string
}

func (c *Client) Ping() error {
	addr := *c.ServiceURL

	resp, err := c.httpClient.Get(addr.String())
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	return notOKStatus(addr.String(), resp)
}

func (c *Client) ServiceInfo() ServiceInfo {
	return *c.serviceInfo
}

func (c *Client) List() ([]*Task, error) {
	var tasks []*Task

	addr := *c.ServiceURL
	addr.Path = "tasks"

	resp, err := c.httpClient.Get(addr.String())
	if err != nil {
		return tasks, err
	}
	defer resp.Body.Close()

	if err = notOKStatus(addr.String(), resp); err != nil {
		return tasks, err
	}

	if err = json.NewDecoder(resp.Body).Decode(&tasks); err != nil {
		return tasks, err
	}

	return tasks, nil
}

func (c *Client) Post(f Form) error {
	addr := *c.ServiceURL
	addr.Path = "tasks"
	query := addr.Query()
	query.Add("region", f.Region)
	if f.RunIn != "" {
		query.Add("run", f.RunIn)
	}
	if f.RevertIn != "" {
		query.Add("revert", f.RevertIn)
	}
	addr.RawQuery = query.Encode()

	resp, err := c.httpClient.Post(
		addr.String(),
		"application/text",
		strings.NewReader(f.Template),
	)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if err := notOKStatus(addr.String(), resp); err != nil {
		return err
	}

	return nil
}

func notOKStatus(addr string, resp *http.Response) error {
	if code := resp.StatusCode; code != http.StatusOK {
		body, _ := ioutil.ReadAll(resp.Body)
		return fmt.Errorf("Got %d status instead of 200 from '%s': %q", code, addr, body)
	}

	return nil
}
