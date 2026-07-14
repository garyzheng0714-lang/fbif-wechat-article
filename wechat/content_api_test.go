package wechat

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestContentEndpointRegistryIncludesEveryReadInterface(t *testing.T) {
	endpoints := AllContentEndpoints()
	if len(endpoints) != 11 {
		t.Fatalf("endpoint count = %d, want 11", len(endpoints))
	}
	seen := make(map[string]bool)
	for _, endpoint := range endpoints {
		if seen[endpoint.Name] || endpoint.Path == "" || endpoint.Documentation == "" {
			t.Fatalf("invalid endpoint: %+v", endpoint)
		}
		seen[endpoint.Name] = true
	}
}

func TestContentClientAcceptsBinaryMaterialResponse(t *testing.T) {
	binary := []byte{0x89, 'P', 'N', 'G', 0x00, 0x01}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(binary)
	}))
	defer server.Close()
	client := &ContentClient{
		BaseURL:      server.URL,
		HTTPClient:   server.Client(),
		GetToken:     func() (string, error) { return "token", nil },
		RefreshToken: func() (string, error) { return "token", nil },
	}
	endpoint, _ := ContentEndpointByName("material_get_material")
	response, err := client.Call(context.Background(), endpoint, map[string]string{"media_id": "x"})
	if err != nil {
		t.Fatal(err)
	}
	if string(response.Body) != string(binary) {
		t.Fatalf("binary response changed: %v", response.Body)
	}
}
