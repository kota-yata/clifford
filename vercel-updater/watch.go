package vercelupdater

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
)

var (
	externalAddressURL    = "https://api4.ipify.org"
	watchInterval         = 5 * time.Minute
	externalAddressClient = &http.Client{Timeout: 10 * time.Second}

	AddressUpdates = make(chan string)
)

func Watch(ctx context.Context) {
	defer close(AddressUpdates)

	ticker := time.NewTicker(watchInterval)
	defer ticker.Stop()

	var previous string
	for {
		address, err := fetchExternalAddress(ctx, externalAddressClient, externalAddressURL)
		if err == nil && address != previous {
			select {
			case AddressUpdates <- address:
				previous = address
			case <-ctx.Done():
				return
			}
		}

		select {
		case <-ticker.C:
		case <-ctx.Done():
			return
		}
	}
}

func fetchExternalAddress(ctx context.Context, client *http.Client, endpoint string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", err
	}

	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("external address service returned %s", resp.Status)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 64))
	if err != nil {
		return "", err
	}

	address := strings.TrimSpace(string(body))
	if net.ParseIP(address) == nil {
		return "", fmt.Errorf("external address service returned an invalid IP address %q", address)
	}

	return address, nil
}
