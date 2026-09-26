package webui

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHandlerKeepsMissingAssetAsNotFound(t *testing.T) {
	handler := Handler(http.NotFoundHandler())
	request := httptest.NewRequest(http.MethodGet, "/assets/not-present.js", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusNotFound)
	}
}
