// Package github asks GitHub for hardn's latest release: the one network
// request hardn makes, and only where hardn.yml's checkReleases turns the
// release check on (version.Checker, whose Latest this is; proposal P-15;
// docs/engine/screen-release-check.md). Nothing else in hardn imports
// net/http.
package github

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	// URL is the one URL asked: install.sh's.
	URL = "https://api.github.com/repos/abbott/hardn/releases/latest"
	// Host is its host; a redirect anywhere else is not followed.
	Host = "api.github.com"
	// Timeout bounds the whole request, the connection and the answer's
	// read included.
	Timeout = 5 * time.Second
	// MaxAnswer is the most of an answer that is read.
	MaxAnswer = 64 << 10
)

// Client makes the request.
type Client struct {
	// Version is this build's version, "0.9.0", for the User-Agent.
	Version string
	// Transport makes the request; nil is Transport(). Tests give a fake:
	// no test makes a network request.
	Transport http.RoundTripper
	// Timeout is Timeout where zero: the tests' shorter one.
	Timeout time.Duration
}

// Transport is the transport of hardn's one request: Go's default proxy
// from the environment, TLS 1.2 or later, and nothing kept open.
func Transport() http.RoundTripper {
	return &http.Transport{
		Proxy:               http.ProxyFromEnvironment,
		TLSClientConfig:     &tls.Config{MinVersion: tls.VersionTLS12},
		TLSHandshakeTimeout: Timeout,
		DisableKeepAlives:   true,
	}
}

// errRedirect refuses a redirect off Host or off HTTPS.
var errRedirect = errors.New("redirected to another host")

// Latest asks URL for the latest release and returns its tag_name as the
// answer gives it: untrusted, for the caller to check (version.ParseTag).
// It sends a User-Agent and an Accept header and nothing else: no token,
// no cookie, no query, no body, nothing about the host. Only tag_name is
// read from the answer: its name, body and URLs are never looked at. It
// returns within Timeout, or as soon as ctx is done.
func (c Client) Latest(ctx context.Context) (string, error) {
	timeout := c.Timeout
	if timeout == 0 {
		timeout = Timeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, URL, http.NoBody)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "hardn/"+strings.TrimPrefix(c.Version, "v"))
	req.Header.Set("Accept", "application/vnd.github+json")
	transport := c.Transport
	if transport == nil {
		transport = Transport()
	}
	client := &http.Client{
		Transport: transport,
		CheckRedirect: func(next *http.Request, _ []*http.Request) error {
			if next.URL.Scheme != "https" || next.URL.Host != Host {
				return errRedirect
			}
			return nil
		},
	}
	late := fmt.Errorf("no answer from %s in %s", Host, timeout)
	resp, err := client.Do(req)
	if err != nil {
		switch {
		case errors.Is(err, errRedirect):
			return "", errRedirect
		case errors.Is(err, context.DeadlineExceeded):
			return "", late
		}
		return "", fmt.Errorf("no answer from %s: %w", Host, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("%s answered status %d", Host, resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, MaxAnswer+1))
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return "", late
	case err != nil:
		return "", fmt.Errorf("the answer was not read: %w", err)
	case len(body) > MaxAnswer:
		return "", fmt.Errorf("the answer is larger than %d bytes", MaxAnswer)
	}
	var answer struct {
		TagName string `json:"tag_name"`
	}
	if json.Unmarshal(body, &answer) != nil {
		return "", errors.New("the answer is not JSON")
	}
	return answer.TagName, nil
}
