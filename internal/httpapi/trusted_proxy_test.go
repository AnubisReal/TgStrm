package httpapi

import (
	"net/http/httptest"
	"testing"
)

func TestRequestSecureTrustsForwardedProtocolOnlyFromDeclaredProxy(t *testing.T) {
	trusted := &API{}
	networks, err := parseTrustedProxyCIDRs([]string{"172.18.0.0/16"})
	if err != nil {
		t.Fatal(err)
	}
	trusted.trustedProxies = networks

	request := httptest.NewRequest("GET", "http://tgstrm.local/", nil)
	request.RemoteAddr = "172.18.0.8:51234"
	request.Header.Set("X-Forwarded-Proto", "https")
	if !trusted.requestSecure(request) {
		t.Fatal("declared proxy should be allowed to assert HTTPS")
	}

	request.RemoteAddr = "10.0.0.7:51234"
	if trusted.requestSecure(request) {
		t.Fatal("untrusted client must not be allowed to assert HTTPS")
	}
}
