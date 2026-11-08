package client

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/gzamboni/terraform-provider-er605/internal/fakerouter"
)

// Vector produced by an independent implementation of the router's js/libs/encrypt.js
// (raw RSA, password zero-padded on the right to the modulus size) with the key an ER605 served.
const (
	vecN   = "D1E79FF135D14E342D76185C23024E6DEAD4D6EC2C317A526C811E83538EA4E5ED8E1B0EEE5CE26E3C1B6A5F1FE11FA804F28B7E8821CA90AFA5B2F300DF99FDA27C9D2131E031EA11463C47944C05005EF4C1CE932D7F4A87C7563581D9F27F0C305023FCE94997EC7D790696E784357ED803A610EBB71B12A8BE5936429BFD"
	vecE   = "010001"
	vecOut = "216b8828ac143082f0ef60fd2addeade649efa827776efb34cb873ca1b8c2a10c438fe7209bb55cf8eed6c6523dce8729909809a8c8b33dea6cc66ee7358ff60d06737446c953aeedc70070637a7587085ee9b8ab5ace6b085b54d0f27e8bb1bf745461fbd6a32fc050339b2c5ce0b490530be384a1032cfd5110e5f8af6f3c9"
)

func fast(t *testing.T) {
	loginRetryDelay = 0
	t.Cleanup(func() { loginRetryDelay = 3 * time.Second })
}

func TestEncryptMatchesRouterJS(t *testing.T) {
	got, err := encrypt("password123", vecN, vecE)
	if err != nil || got != vecOut {
		t.Fatalf("encrypt = %q, %v; want %q", got, err, vecOut)
	}
}

func TestEncryptRejectsTooLongPassword(t *testing.T) {
	if _, err := encrypt(strings.Repeat("x", 129), vecN, vecE); err == nil {
		t.Fatal("expected error for password longer than the modulus")
	}
}

func TestCallLogsInUsesSessionAndLogsOut(t *testing.T) {
	fast(t)
	f := fakerouter.New(t, "admin", "secret1")
	f.SeedSingle("/admin/test?form=single", map[string]any{"a": "1"})
	c := New(f.URL, "admin", "secret1", false)
	raw, err := c.Call("/admin/test?form=single", "set", map[string]any{"a": "2"})
	if err != nil || string(raw) != `{"a":"2"}` {
		t.Fatalf("Call = %s, %v", raw, err)
	}
	if f.Logouts() != 1 || f.Sessions() != 0 {
		t.Fatalf("session not closed: logouts=%d open=%d", f.Logouts(), f.Sessions())
	}
}

func TestCallWrongPasswordFailsAfterRetries(t *testing.T) {
	fast(t)
	f := fakerouter.New(t, "admin", "secret1")
	c := New(f.URL, "admin", "wrong", false)
	if _, err := c.Call("/admin/test?form=single", "get", nil); err == nil || !strings.Contains(err.Error(), "login failed") {
		t.Fatalf("want login failure, got %v", err)
	}
}

func TestCallSurfacesRouterErrorCode(t *testing.T) {
	fast(t)
	f := fakerouter.New(t, "admin", "secret1")
	f.SeedSingle("/admin/test?form=single", map[string]any{})
	c := New(f.URL, "admin", "secret1", false)
	// a set on a list form without a valid index is the fake's way to produce error_code 1007
	_, err := c.Call("/admin/test?form=rows", "set", map[string]any{"index": 5, "new": map[string]any{}})
	ae, ok := err.(*APIError)
	if !ok || ae.Code != "1007" {
		t.Fatalf("want APIError 1007, got %v", err)
	}
}

func TestSetAccountEncryptsAndSwitchesCredentials(t *testing.T) {
	fast(t)
	f := fakerouter.New(t, "admin", "secret1")
	f.SeedSingle("/admin/test?form=single", map[string]any{})
	c := New(f.URL, "admin", "secret1", false)
	if err := c.SetAccount("root", "newsecret"); err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"old_acc": "admin", "new_acc": "root", "old_pwd": "secret1", "new_pwd": "newsecret", "cfm_pwd": "newsecret"}
	for k, v := range want {
		if f.Account[k] != v {
			t.Errorf("%s = %q, want %q", k, f.Account[k], v)
		}
	}
	// the client must now authenticate with the new credentials
	if _, err := c.Call("/admin/test?form=single", "get", nil); err != nil {
		t.Fatalf("call after account change: %v", err)
	}
}

func TestPostEncodesPayloadAsDataForm(t *testing.T) {
	var got url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		got = r.PostForm
		w.Write([]byte(`{"id":1,"result":{},"error_code":"0"}`))
	}))
	defer srv.Close()
	c := New(srv.URL, "u", "p", false)
	if _, err := c.post("tok/admin/x?form=y", map[string]any{"method": "get"}); err != nil {
		t.Fatal(err)
	}
	if got.Get("data") != `{"method":"get"}` {
		t.Fatalf("data = %q", got.Get("data"))
	}
}
