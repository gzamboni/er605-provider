// Package client talks to the TP-Link ER605 (standalone) web API: /cgi-bin/luci/;stok=<token>/<endpoint>.
// Protocol reverse-engineered from the router's own web UI (js/su/data/proxy.js, js/libs/encrypt.js).
package client

import (
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"sync"
	"time"
)

type Client struct {
	base, user, pass string
	http             *http.Client
	mu               sync.Mutex // the router handles one request at a time and has a single login session
	stok             string
}

func New(baseURL, user, pass string, insecure bool) *Client {
	jar, _ := cookiejar.New(nil) // the session cookie must accompany the stok
	return &Client{
		base: strings.TrimRight(baseURL, "/"), user: user, pass: pass,
		http: &http.Client{
			Jar:       jar,
			Timeout:   60 * time.Second,
			Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: insecure}},
		},
	}
}

// SetTimeout bounds each HTTP request (default 60s); the router can be slow to apply a change.
func (c *Client) SetTimeout(d time.Duration) { c.http.Timeout = d }

type envelope struct {
	Result    json.RawMessage `json:"result"`
	ErrorCode json.RawMessage `json:"error_code"`
}

// post sends {"data": <payload json>} form-encoded, as the web UI does.
func (c *Client) post(path string, payload any) (*envelope, error) {
	b, _ := json.Marshal(payload)
	req, err := http.NewRequest("POST", c.base+"/cgi-bin/luci/;stok="+path, strings.NewReader(url.Values{"data": {string(b)}}.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Referer", c.base+"/webpages/index.html") // static files 404 without it; harmless here
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var e envelope
	if err := json.Unmarshal(body, &e); err != nil {
		return nil, fmt.Errorf("router error (HTTP %d): %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	if code := strings.Trim(string(e.ErrorCode), `"`); code != "0" {
		return &e, &APIError{Code: code, Body: string(body)}
	}
	return &e, nil
}

type APIError struct{ Code, Body string }

func (e *APIError) Error() string { return fmt.Sprintf("router error_code %s: %s", e.Code, e.Body) }

// encrypt replicates js/libs/encrypt.js: raw RSA (no padding) of the password, zero-padded on the right to the modulus size.
func encrypt(pw, nHex, eHex string) (string, error) {
	n, ok1 := new(big.Int).SetString(nHex, 16)
	e, ok2 := new(big.Int).SetString(eHex, 16)
	if !ok1 || !ok2 {
		return "", fmt.Errorf("invalid RSA key from router")
	}
	k := (n.BitLen() + 7) / 8
	if len(pw) > k {
		return "", fmt.Errorf("password too long")
	}
	buf := make([]byte, k)
	copy(buf, pw)
	c := new(big.Int).Exp(new(big.Int).SetBytes(buf), e, n).Bytes()
	out := hex.EncodeToString(c)
	if len(out)%2 == 1 {
		out = "0" + out
	}
	return out, nil
}

func (c *Client) login() error {
	e, err := c.post("/login?form=login", map[string]any{"method": "get"})
	if err != nil {
		return err
	}
	var r struct {
		Password []string `json:"password"`
	}
	if err := json.Unmarshal(e.Result, &r); err != nil || len(r.Password) != 2 {
		return fmt.Errorf("unexpected login key response: %s", e.Result)
	}
	pw, err := encrypt(c.pass, r.Password[0], r.Password[1])
	if err != nil {
		return err
	}
	e, err = c.post("/login?form=login", map[string]any{"method": "login", "params": map[string]any{"username": c.user, "password": pw}})
	if err != nil {
		return fmt.Errorf("login failed: %w", err)
	}
	var s struct {
		Stok string `json:"stok"`
	}
	if err := json.Unmarshal(e.Result, &s); err != nil || s.Stok == "" {
		return fmt.Errorf("login returned no token: %s", e.Result)
	}
	c.stok = s.Stok
	return nil
}

// loginRetryDelay is a variable so tests can skip the wait.
var loginRetryDelay = 3 * time.Second

// Call logs in, runs method (get|set|add|delete) on endpoint (e.g. "/admin/dhcps?form=setting"), logs out and
// returns the raw "result". A nil params omits the field. One short session per call: the router allows a single
// admin session and the provider has no teardown hook, so a long-lived session would lock the web UI out.
func (c *Client) Call(endpoint, method string, params any) (json.RawMessage, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	var err error
	for range 3 { // the router occasionally answers a login with a Lua error right after a config change
		if err = c.login(); err == nil {
			break
		}
		time.Sleep(loginRetryDelay)
	}
	if err != nil {
		return nil, err
	}
	defer c.post(c.stok+"/admin/system?form=logout", map[string]any{"method": "set"})
	payload := map[string]any{"method": method}
	if params != nil {
		payload["params"] = params
	}
	e, err := c.post(c.stok+endpoint, payload)
	if err != nil {
		return nil, err
	}
	return e.Result, nil
}

// SetAccount changes the admin username/password (System > Administration > Account) and, on success, switches
// this client to the new credentials so later calls in the same run keep working. Like the web UI, the router
// wants old and new passwords RSA-encrypted with the key from the account form.
func (c *Client) SetAccount(user, pass string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.login(); err != nil {
		return err
	}
	defer c.post(c.stok+"/admin/system?form=logout", map[string]any{"method": "set"})
	e, err := c.post(c.stok+"/admin/administration?form=account", map[string]any{"method": "get"})
	if err != nil {
		return err
	}
	var k struct {
		NewPwd []string `json:"new_pwd"`
	}
	if err := json.Unmarshal(e.Result, &k); err != nil || len(k.NewPwd) != 2 {
		return fmt.Errorf("unexpected account form: %s", e.Result)
	}
	oldPwd, err := encrypt(c.pass, k.NewPwd[0], k.NewPwd[1])
	if err != nil {
		return err
	}
	newPwd, err := encrypt(pass, k.NewPwd[0], k.NewPwd[1])
	if err != nil {
		return err
	}
	if _, err := c.post(c.stok+"/admin/administration?form=account", map[string]any{"method": "set", "params": map[string]any{
		"old_acc": c.user, "old_pwd": oldPwd, "new_acc": user, "new_pwd": newPwd, "cfm_pwd": newPwd,
	}}); err != nil {
		return err
	}
	c.user, c.pass = user, pass
	return nil
}
