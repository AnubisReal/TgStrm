package httpapi

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestLibraryRequestRejectsAmbiguousJSON(t *testing.T) {
	for _, body := range []string{`{"revision":1}{"revision":2}`, `{"revision":1} garbage`, `{"revision":1,"unexpected":true}`, strings.Repeat(" ", 64<<10) + `{"revision":1}`} {
		var value struct {
			Revision int `json:"revision"`
		}
		response := httptest.NewRecorder()
		request := httptest.NewRequest("POST", "/", strings.NewReader(body))
		if decodeLibraryRequest(response, request, &value) {
			t.Fatalf("accepted invalid body: %.60s", body)
		}
		if response.Code != 400 {
			t.Fatalf("status: %d", response.Code)
		}
	}
	var value struct {
		Revision int `json:"revision"`
	}
	if !decodeLibraryRequest(httptest.NewRecorder(), httptest.NewRequest("POST", "/", strings.NewReader("{\"revision\":1}\n")), &value) || value.Revision != 1 {
		t.Fatal("valid request rejected")
	}
}
