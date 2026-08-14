// Package olx is a thin read-only client for the olx.ro public offer API.
//
// Two notes on why this looks the way it does:
//
//   - OLX sits behind a CloudFront WAF rule that reads the HTTP/2 connection
//     fingerprint, so the client must speak HTTP/1.1. See New.
//   - A removed offer answers 410 on /offers/{id}/. That is the removal signal
//     the site is built on, so it is reported as its own state rather than an error.
package olx

import (
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

const (
	base      = "https://www.olx.ro/api/v1/offers/"
	userAgent = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) " +
		"AppleWebKit/537.36 (KHTML, like Gecko) Chrome/138.0.0.0 Safari/537.36"
)

// ErrGone reports that an offer answered 410 — sold, withdrawn or expired.
var ErrGone = errors.New("offer is gone (HTTP 410)")

// Client talks to the olx.ro offer API. The zero value is not usable; use New.
type Client struct {
	http  *http.Client
	pause time.Duration // spacing between requests, to stay a polite caller
}

// New returns a client that talks to OLX over HTTP/1.1.
//
// That is not a style choice. OLX is behind a CloudFront WAF rule that reads the
// HTTP/2 connection fingerprint — the SETTINGS values, window sizes and frame
// order a client sends when it opens an h2 connection. Go's fingerprint is on its
// list, and so is curl's: over h2 both get 403 on every olx.ro endpoint, API and
// HTML alike, headers notwithstanding. The same requests over HTTP/1.1 get 200,
// from either. Measured 14 Aug 2026:
//
//	curl --http2    403      curl --http1.1    200
//	Go, h2 (ALPN)   403      Go, http/1.1      200
//
// So this needs no browser, no proxy and no TLS impersonation — the TLS layer was
// never the problem. Do not "simplify" it into a shell-out to curl, and do not
// drop the transport below: on the stock transport every request fails.
//
// Pinning NextProtos is what actually forces HTTP/1.1. Clearing TLSNextProto and
// ForceAttemptHTTP2 alone still negotiates h2 over ALPN, and the transport then
// chokes on the h2 frames with `malformed HTTP response "\x00\x00\x12\x04…"`.
func New() *Client {
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.ForceAttemptHTTP2 = false
	tr.TLSNextProto = map[string]func(string, *tls.Conn) http.RoundTripper{}
	tr.TLSClientConfig = &tls.Config{
		NextProtos: []string{"http/1.1"},
		MinVersion: tls.VersionTLS12,
	}
	return &Client{
		http:  &http.Client{Timeout: 30 * time.Second, Transport: tr},
		pause: 400 * time.Millisecond,
	}
}

// Offer is the subset of the API payload this project uses.
type Offer struct {
	ID              int64   `json:"id"`
	URL             string  `json:"url"`
	Title           string  `json:"title"`
	Description     string  `json:"description"`
	Status          string  `json:"status"`
	Business        bool    `json:"business"`
	CreatedTime     string  `json:"created_time"`
	LastRefreshTime string  `json:"last_refresh_time"`
	Params          []Param `json:"params"`
	Location        struct {
		City   struct{ Name string } `json:"city"`
		Region struct{ Name string } `json:"region"`
	} `json:"location"`
}

type Param struct {
	Key   string          `json:"key"`
	Name  string          `json:"name"`
	Type  string          `json:"type"`
	Value json.RawMessage `json:"value"`
}

// Price pulls the price param. ok is false for offers with no price set
// (barter or "price on request" ads).
func (o Offer) Price() (value float64, currency, label string, previous float64, ok bool) {
	for _, p := range o.Params {
		if p.Key != "price" {
			continue
		}
		var v struct {
			Value         float64 `json:"value"`
			Currency      string  `json:"currency"`
			Label         string  `json:"label"`
			PreviousValue float64 `json:"previous_value"`
		}
		if json.Unmarshal(p.Value, &v) != nil || v.Value == 0 {
			return 0, "", "", 0, false
		}
		return v.Value, v.Currency, v.Label, v.PreviousValue, true
	}
	return 0, "", "", 0, false
}

// SelectParam returns the label of a select-type param, e.g. capacitate_memorie_ram.
func (o Offer) SelectParam(key string) string {
	for _, p := range o.Params {
		if p.Key != key {
			continue
		}
		var v struct{ Label string }
		if json.Unmarshal(p.Value, &v) == nil {
			return v.Label
		}
	}
	return ""
}

func (c *Client) get(u string, out any) (int, error) {
	req, err := http.NewRequest(http.MethodGet, u, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "application/json, text/plain, */*")
	req.Header.Set("Accept-Language", "ro-RO,ro;q=0.9,en;q=0.8")

	resp, err := c.http.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	time.Sleep(c.pause)

	if resp.StatusCode == http.StatusGone || resp.StatusCode == http.StatusNotFound {
		return resp.StatusCode, ErrGone
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 200))
		return resp.StatusCode, fmt.Errorf("olx: HTTP %d: %s", resp.StatusCode, body)
	}
	if out == nil {
		return resp.StatusCode, nil
	}
	return resp.StatusCode, json.NewDecoder(resp.Body).Decode(out)
}

// Search returns up to limit offers matching query, newest listings first.
func (c *Client) Search(query string, limit int) ([]Offer, error) {
	var page struct {
		Data     []Offer `json:"data"`
		Metadata struct {
			TotalElements int `json:"total_elements"`
		} `json:"metadata"`
	}
	u := base + "?" + url.Values{
		"offset": {"0"},
		"limit":  {fmt.Sprint(limit)},
		"query":  {query},
	}.Encode()
	if _, err := c.get(u, &page); err != nil {
		return nil, fmt.Errorf("search %q: %w", query, err)
	}
	return page.Data, nil
}

// Alive reports whether an offer still exists. A 410 or 404 means gone, and is
// returned as (false, nil) — that is an answer, not a failure.
func (c *Client) Alive(id int64) (bool, *Offer, error) {
	var wrap struct {
		Data Offer `json:"data"`
	}
	_, err := c.get(fmt.Sprintf("%s%d/", base, id), &wrap)
	if errors.Is(err, ErrGone) {
		return false, nil, nil
	}
	if err != nil {
		return false, nil, err
	}
	return wrap.Data.Status == "active", &wrap.Data, nil
}
