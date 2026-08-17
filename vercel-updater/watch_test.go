package vercelupdater

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

func TestWatchNotifiesOnlyWhenExternalAddressChanges(t *testing.T) {
	responses := []string{"203.0.113.1", "203.0.113.1", "203.0.113.2"}
	var (
		mu       sync.Mutex
		requests int
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		index := requests
		requests++
		mu.Unlock()

		if index >= len(responses) {
			index = len(responses) - 1
		}
		fmt.Fprint(w, responses[index])
	}))
	defer server.Close()
	updates := configureWatchForTest(t, server.Client(), server.URL, 5*time.Millisecond)

	ctx, cancel := context.WithCancel(context.Background())
	go Watch(ctx)

	assertUpdate(t, updates, "203.0.113.1")
	assertUpdate(t, updates, "203.0.113.2")

	cancel()
	select {
	case _, ok := <-updates:
		if ok {
			t.Fatal("updates channel remained open after cancellation")
		}
	case <-time.After(time.Second):
		t.Fatal("updates channel was not closed after cancellation")
	}
}

func TestWatchCancellationInterruptsRequest(t *testing.T) {
	requestStarted := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(requestStarted)
		<-r.Context().Done()
	}))
	defer server.Close()
	updates := configureWatchForTest(t, server.Client(), server.URL, time.Hour)

	ctx, cancel := context.WithCancel(context.Background())
	go Watch(ctx)

	<-requestStarted
	cancel()

	select {
	case _, ok := <-updates:
		if ok {
			t.Fatal("updates channel remained open after cancellation")
		}
	case <-time.After(time.Second):
		t.Fatal("updates channel was not closed after cancellation")
	}
}

func TestFetchExternalAddressRejectsInvalidResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, "not an IP address")
	}))
	defer server.Close()

	if _, err := fetchExternalAddress(context.Background(), server.Client(), server.URL); err == nil {
		t.Fatal("fetchExternalAddress accepted an invalid address")
	}
}

func configureWatchForTest(t *testing.T, client *http.Client, endpoint string, interval time.Duration) <-chan string {
	t.Helper()

	previousClient := externalAddressClient
	previousURL := externalAddressURL
	previousInterval := watchInterval
	previousUpdates := AddressUpdates
	externalAddressClient = client
	externalAddressURL = endpoint
	watchInterval = interval
	AddressUpdates = make(chan string)
	t.Cleanup(func() {
		externalAddressClient = previousClient
		externalAddressURL = previousURL
		watchInterval = previousInterval
		AddressUpdates = previousUpdates
	})

	return AddressUpdates
}

func assertUpdate(t *testing.T, updates <-chan string, want string) {
	t.Helper()

	select {
	case got, ok := <-updates:
		if !ok {
			t.Fatalf("updates channel closed before receiving %q", want)
		}
		if got != want {
			t.Fatalf("received %q, want %q", got, want)
		}
	case <-time.After(time.Second):
		t.Fatalf("timed out waiting for %q", want)
	}
}
