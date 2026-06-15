// SPDX-License-Identifier: AGPL-3.0-or-later
//
// Package routeros is a minimal client for the MikroTik RouterOS v7+ REST API
// (HTTPS or HTTP, HTTP Basic authentication) exposed by the `www-ssl` / `www`
// services on RouterOS 7.x and later.
//
// The REST API is a clean mapping over the RouterOS menu tree: GET to
// list/print a menu, GET .../<id> to read one item, PUT to add an item (the
// reply echoes the created object including its `.id`), PATCH .../<id> to
// update, DELETE .../<id> to remove. Some menus are singletons (e.g.
// /system/identity, /ip/dns) — GET to read, PATCH the menu path to update,
// no add/delete. This client is generic over that surface (any /rest path).
package routeros

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Client is an HTTP-Basic-authenticated RouterOS REST client. It is stateless
// beyond its credentials (Basic auth carries on every request, so there is no
// session to establish); callers may share one Client across resources (the
// provider does). Safe for concurrent use.
type Client struct {
	base     string // e.g. https://192.168.7.x/rest
	user     string
	password string
	http     *http.Client

	mu sync.Mutex // serializes mutating ops; RouterOS handles config changes one at a time
}

// Config configures a Client.
type Config struct {
	// Host is the router address (host or host:port), no scheme.
	Host string
	// Username / Password are the RouterOS user credentials (HTTP Basic).
	Username string
	Password string
	// Insecure skips TLS verification (RouterOS ships a self-signed cert; true
	// is the norm on a lab/management network).
	Insecure bool
	// Scheme is "https" (default) or "http" (RouterOS `www` service, v7.9+).
	Scheme string
	// Timeout per request (default 30s).
	Timeout time.Duration
}

// NewClient builds a Client. It does not contact the router until the first
// API call.
func NewClient(c Config) *Client {
	if c.Timeout == 0 {
		c.Timeout = 30 * time.Second
	}
	scheme := c.Scheme
	if scheme == "" {
		scheme = "https"
	}
	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: c.Insecure}, //nolint:gosec // self-signed mgmt cert
		MaxIdleConns:    4,
		IdleConnTimeout: 30 * time.Second,
	}
	host := strings.TrimSuffix(strings.TrimPrefix(c.Host, "https://"), "/")
	host = strings.TrimPrefix(host, "http://")
	return &Client{
		base:     fmt.Sprintf("%s://%s/rest", scheme, host),
		user:     c.Username,
		password: c.Password,
		http:     &http.Client{Timeout: c.Timeout, Transport: tr},
	}
}

// APIError is returned when the router responds with a non-2xx status. The body
// of a RouterOS error is a JSON object carrying `error` (code) and `message`.
type APIError struct {
	Method string
	Path   string
	Status int
	Body   string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("routeros %s %s: HTTP %d: %s", e.Method, e.Path, e.Status, e.Body)
}

// NotFound reports whether err is an APIError with a 404 status.
func NotFound(err error) bool {
	var ae *APIError
	if e, ok := err.(*APIError); ok {
		ae = e
	}
	return ae != nil && ae.Status == http.StatusNotFound
}

// do performs one authenticated request under the mutex and returns the
// response body on a 2xx. path is relative to /rest and must start with "/".
// body may be nil.
func (c *Client) do(method, path string, body []byte) ([]byte, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	var rdr io.Reader
	if body != nil {
		rdr = bytes.NewReader(body)
	}
	req, err := http.NewRequest(method, c.base+path, rdr)
	if err != nil {
		return nil, err
	}
	req.SetBasicAuth(c.user, c.password)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("routeros %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode/100 != 2 {
		return nil, &APIError{Method: method, Path: path, Status: resp.StatusCode, Body: string(raw)}
	}
	return raw, nil
}

// Get fetches a resource. path is relative to /rest (must start with "/").
func (c *Client) Get(path string) ([]byte, error) { return c.do(http.MethodGet, path, nil) }

// Put adds a resource to a collection with the given JSON body. RouterOS echoes
// the created object (including its `.id`) on success.
func (c *Client) Put(path string, body []byte) ([]byte, error) {
	return c.do(http.MethodPut, path, body)
}

// Patch updates a collection item (PATCH /rest/<menu>/<id>) with the given JSON body.
func (c *Client) Patch(path string, body []byte) ([]byte, error) {
	return c.do(http.MethodPatch, path, body)
}

// Set updates a settings (singleton) menu via RouterOS's `set` command
// (POST /rest/<menu>/set). A bare PATCH on a settings menu is rejected with
// HTTP 400 "missing or invalid resource identifier" because PATCH addresses a
// collection item by id, which a settings menu does not have.
func (c *Client) Set(menu string, body []byte) ([]byte, error) {
	return c.do(http.MethodPost, menu+"/set", body)
}

// Post executes a command at path with the given JSON body (e.g. /<menu>/print
// with .proplist/.query). Reserved for command-style endpoints.
func (c *Client) Post(path string, body []byte) ([]byte, error) {
	return c.do(http.MethodPost, path, body)
}

// Delete removes a resource.
func (c *Client) Delete(path string) ([]byte, error) { return c.do(http.MethodDelete, path, nil) }

// FindByName returns the `.id` of the item in collection menu whose `name`
// equals name, or "" if none matches. RouterOS identifies many collection items
// (logging actions, address lists, …) by a unique name; this lets the resource
// adopt a pre-existing one (e.g. a built-in logging action) instead of failing
// a duplicate PUT.
func (c *Client) FindByName(menu, name string) (string, error) {
	raw, err := c.Get(menu)
	if err != nil {
		return "", err
	}
	var items []map[string]json.RawMessage
	if err := json.Unmarshal(raw, &items); err != nil {
		return "", fmt.Errorf("decode %s collection: %w", menu, err)
	}
	for _, it := range items {
		var n string
		if json.Unmarshal(it["name"], &n) == nil && n == name {
			var id string
			_ = json.Unmarshal(it[".id"], &id)
			return id, nil
		}
	}
	return "", nil
}
