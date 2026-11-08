// Package fakerouter is an in-memory stand-in for the ER605 web API, good enough to exercise the client and
// every provider resource without hardware: RSA login, singleton forms, list forms (add/set/delete by index),
// the account form and logout. It mimics the quirks the provider has to handle (empty list reported as {}).
package fakerouter

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
)

type Router struct {
	URL string

	mu       sync.Mutex
	key      *rsa.PrivateKey
	user     string
	pass     string
	sessions map[string]bool
	logouts  int
	lists    map[string][]map[string]any
	singles  map[string]map[string]any
	Account  map[string]string // last account change, passwords decrypted
}

// New starts a fake router accepting user/pass.
func New(t testing.TB, user, pass string) *Router {
	key, err := rsa.GenerateKey(rand.Reader, 1024)
	if err != nil {
		t.Fatal(err)
	}
	r := &Router{key: key, user: user, pass: pass, sessions: map[string]bool{}, lists: map[string][]map[string]any{}, singles: map[string]map[string]any{}}
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)
	r.URL = srv.URL
	return r
}

// SeedList sets the initial rows of a list form.
func (r *Router) SeedList(endpoint string, rows ...map[string]any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.lists[endpoint] = rows
}

// SeedSingle sets the initial value of a singleton form, e.g. "/admin/dhcps?form=setting".
func (r *Router) SeedSingle(endpoint string, v map[string]any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.singles[endpoint] = v
}

// Single returns the current value of a singleton form.
func (r *Router) Single(endpoint string) map[string]any {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.singles[endpoint]
}

// List returns the current rows of a list form.
func (r *Router) List(endpoint string) []map[string]any {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]map[string]any(nil), r.lists[endpoint]...)
}

// Logouts counts closed sessions; Sessions the ones still open.
func (r *Router) Logouts() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.logouts
}

func (r *Router) Sessions() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.sessions)
}

func (r *Router) decrypt(h string) string {
	c, _ := new(big.Int).SetString(h, 16)
	m := new(big.Int).Exp(c, r.key.D, r.key.N).Bytes()
	k := (r.key.N.BitLen() + 7) / 8
	buf := make([]byte, k)
	copy(buf[k-len(m):], m)
	return strings.TrimRight(string(buf), "\x00")
}

func reply(w http.ResponseWriter, code string, result any) {
	b, _ := json.Marshal(map[string]any{"id": 1, "result": result, "error_code": code})
	w.Write(b)
}

func (r *Router) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	r.mu.Lock()
	defer r.mu.Unlock()
	req.ParseForm()
	var body struct {
		Method string         `json:"method"`
		Params map[string]any `json:"params"`
	}
	json.Unmarshal([]byte(req.PostForm.Get("data")), &body)
	rest := strings.TrimPrefix(req.URL.Path, "/cgi-bin/luci/;stok=")
	stok, path, _ := strings.Cut(rest, "/")
	form := req.URL.Query().Get("form")
	endpoint := "/" + path + "?form=" + form
	pub := []string{strings.ToUpper(r.key.N.Text(16)), "010001"}

	switch {
	case path == "login" && body.Method == "get":
		reply(w, "0", map[string]any{"username": "", "password": pub})
	case path == "login" && body.Method == "login":
		if body.Params["username"] != r.user || r.decrypt(body.Params["password"].(string)) != r.pass {
			reply(w, "700", nil)
			return
		}
		r.sessions["tok"] = true
		reply(w, "0", map[string]any{"stok": "tok"})
	case !r.sessions[stok]:
		reply(w, "1014", nil)
	case endpoint == "/admin/system?form=logout":
		delete(r.sessions, stok)
		r.logouts++
		reply(w, "0", nil)
	case endpoint == "/admin/administration?form=account" && body.Method == "get":
		reply(w, "0", map[string]any{"new_pwd": pub})
	case endpoint == "/admin/administration?form=account":
		r.Account = map[string]string{"old_acc": body.Params["old_acc"].(string), "new_acc": body.Params["new_acc"].(string),
			"old_pwd": r.decrypt(body.Params["old_pwd"].(string)), "new_pwd": r.decrypt(body.Params["new_pwd"].(string)), "cfm_pwd": r.decrypt(body.Params["cfm_pwd"].(string))}
		r.user, r.pass = r.Account["new_acc"], r.Account["new_pwd"]
		reply(w, "0", nil)
	default:
		if _, ok := r.singles[endpoint]; ok {
			if body.Method == "set" {
				r.singles[endpoint] = body.Params
			}
			reply(w, "0", r.singles[endpoint])
			return
		}
		r.serveList(w, endpoint, body.Method, body.Params)
	}
}

// serveList treats any other endpoint as a list form, like the router's grids.
func (r *Router) serveList(w http.ResponseWriter, endpoint, method string, p map[string]any) {
	rows := r.lists[endpoint]
	index := func() (int, bool) {
		switch v := p["index"].(type) {
		case float64:
			return int(v), int(v) >= 0 && int(v) < len(rows)
		case string:
			i, err := strconv.Atoi(v)
			return i, err == nil && i >= 0 && i < len(rows)
		}
		return 0, false
	}
	switch method {
	case "add":
		n, _ := p["new"].(map[string]any)
		rows = append(rows, n)
	case "set":
		i, ok := index()
		n, _ := p["new"].(map[string]any)
		if !ok || n == nil {
			reply(w, "1007", nil)
			return
		}
		rows[i] = n
	case "delete":
		i, ok := index()
		if !ok {
			reply(w, "1007", nil)
			return
		}
		rows = append(rows[:i], rows[i+1:]...)
	}
	r.lists[endpoint] = rows
	if len(rows) == 0 {
		reply(w, "0", map[string]any{}) // the router reports an empty list as {}
		return
	}
	reply(w, "0", rows)
}
