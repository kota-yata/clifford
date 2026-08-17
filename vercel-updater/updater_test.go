package vercelupdater

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync"
	"testing"
)

func TestUpdateAppliesAddressToEveryDomain(t *testing.T) {
	var (
		mu      sync.Mutex
		patched = make(map[string]string)
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
			t.Errorf("Authorization = %q", got)
		}
		if got := r.URL.Query().Get("teamId"); got != "team_123" {
			t.Errorf("teamId = %q", got)
		}

		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v5/domains/kota-yata.com/records":
			fmt.Fprint(w, `{
				"records": [
					{"id":"rec_www","name":"www","type":"A","value":"203.0.113.1"},
					{"id":"rec_blog","name":"blog","type":"A","value":"203.0.113.2"},
					{"id":"rec_txt","name":"www","type":"TXT","value":"ignored"}
				],
				"pagination":{"count":3,"next":null,"prev":null}
			}`)
		case r.Method == http.MethodPatch:
			var body struct {
				Value string `json:"value"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Errorf("decode PATCH body: %v", err)
			}
			mu.Lock()
			patched[r.URL.Path] = body.Value
			mu.Unlock()
			fmt.Fprint(w, `{}`)
		default:
			http.Error(w, "unexpected request", http.StatusNotFound)
		}
	}))
	defer server.Close()
	configureUpdaterForTest(t, server)
	t.Setenv("VERCEL_API_TOKEN", "test-token")
	t.Setenv("VERCEL_TOKEN", "")
	t.Setenv("VERCEL_TEAM_ID", "team_123")

	updates := make(chan string, 1)
	updates <- "203.0.113.10"
	close(updates)
	AddressUpdates = updates

	if err := Update(context.Background()); err != nil {
		t.Fatalf("Update() error = %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	want := map[string]string{
		"/v1/domains/records/rec_www":  "203.0.113.10",
		"/v1/domains/records/rec_blog": "203.0.113.10",
	}
	if !maps.Equal(patched, want) {
		t.Fatalf("patched records = %#v, want %#v", patched, want)
	}
}

func TestUpdateSkipsRecordAlreadyAtAddress(t *testing.T) {
	var methods []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		methods = append(methods, r.Method)
		fmt.Fprint(w, `{
			"records": [
				{"id":"rec_www","name":"www","type":"A","value":"203.0.113.10"},
				{"id":"rec_blog","name":"blog","type":"A","value":"203.0.113.10"}
			],
			"pagination":{"count":2,"next":null,"prev":null}
		}`)
	}))
	defer server.Close()
	configureUpdaterForTest(t, server)

	if err := updateDNSRecords(context.Background(), "test-token", "", "203.0.113.10"); err != nil {
		t.Fatalf("updateDNSRecords() error = %v", err)
	}
	if !slices.Equal(methods, []string{http.MethodGet}) {
		t.Fatalf("request methods = %v, want only GET", methods)
	}
}

func TestUpdateRequiresToken(t *testing.T) {
	t.Setenv("VERCEL_API_TOKEN", "")
	t.Setenv("VERCEL_TOKEN", "")

	if err := Update(context.Background()); err == nil {
		t.Fatal("Update() succeeded without a Vercel API token")
	}
}

func configureUpdaterForTest(t *testing.T, server *httptest.Server) {
	t.Helper()

	previousBaseURL := vercelAPIBaseURL
	previousClient := vercelAPIClient
	previousUpdates := AddressUpdates
	vercelAPIBaseURL = server.URL
	vercelAPIClient = server.Client()
	t.Cleanup(func() {
		vercelAPIBaseURL = previousBaseURL
		vercelAPIClient = previousClient
		AddressUpdates = previousUpdates
	})
}
