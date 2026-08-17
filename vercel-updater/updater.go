package vercelupdater

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

const dnsZone = "kota-yata.com"

var (
	domains = []string{
		"www.kota-yata.com",
		"blog.kota-yata.com",
	}
	vercelAPIBaseURL = "https://api.vercel.com"
	vercelAPIClient  = &http.Client{Timeout: 10 * time.Second}
)

type dnsRecord struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Type  string `json:"type"`
	Value string `json:"value"`
}

type dnsRecordsResponse struct {
	Records    []dnsRecord `json:"records"`
	Pagination struct {
		Next *int64 `json:"next"`
	} `json:"pagination"`
}

func Update(ctx context.Context) error {
	if err := godotenv.Load(); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("load .env: %w", err)
	}

	token := os.Getenv("VERCEL_API_TOKEN")
	if token == "" {
		token = os.Getenv("VERCEL_TOKEN")
	}
	if token == "" {
		return errors.New("VERCEL_API_TOKEN is not set")
	}
	teamID := os.Getenv("VERCEL_TEAM_ID")

	for {
		select {
		case address, ok := <-AddressUpdates: // from watch.go
			if !ok {
				return nil
			}
			if err := updateDNSRecords(ctx, token, teamID, address); err != nil {
				return err
			}
		case <-ctx.Done():
			return nil
		}
	}
}

func updateDNSRecords(ctx context.Context, token, teamID, address string) error {
	ip := net.ParseIP(address)
	if ip == nil || ip.To4() == nil {
		return fmt.Errorf("cannot set A records to non-IPv4 address %q", address)
	}

	records, err := listDNSRecords(ctx, token, teamID)
	if err != nil {
		return err
	}

	recordsByName := make(map[string][]dnsRecord)
	for _, record := range records {
		if record.Type == "A" {
			recordsByName[record.Name] = append(recordsByName[record.Name], record)
		}
	}

	var updateErrors []error
	for _, domain := range domains {
		name, err := dnsRecordName(domain)
		if err != nil {
			updateErrors = append(updateErrors, err)
			continue
		}

		matching := recordsByName[name]
		if len(matching) == 0 {
			updateErrors = append(updateErrors, fmt.Errorf("A record for %s was not found", domain))
			continue
		}

		for _, record := range matching {
			if record.Value == address {
				continue
			}
			if err := patchDNSRecord(ctx, token, teamID, record.ID, address); err != nil {
				updateErrors = append(updateErrors, fmt.Errorf("update %s: %w", domain, err))
			}
		}
	}

	return errors.Join(updateErrors...)
}

func listDNSRecords(ctx context.Context, token, teamID string) ([]dnsRecord, error) {
	var (
		records []dnsRecord
		until   *int64
	)

	for {
		query := url.Values{"limit": {"100"}}
		if teamID != "" {
			query.Set("teamId", teamID)
		}
		if until != nil {
			query.Set("until", fmt.Sprint(*until))
		}

		var page dnsRecordsResponse
		if err := doVercelRequest(
			ctx,
			http.MethodGet,
			"/v5/domains/"+url.PathEscape(dnsZone)+"/records",
			query,
			token,
			nil,
			&page,
		); err != nil {
			return nil, fmt.Errorf("list DNS records: %w", err)
		}
		records = append(records, page.Records...)

		if page.Pagination.Next == nil {
			return records, nil
		}
		if until != nil && *page.Pagination.Next == *until {
			return nil, errors.New("list DNS records: pagination did not advance")
		}
		until = page.Pagination.Next
	}
}

func patchDNSRecord(ctx context.Context, token, teamID, recordID, address string) error {
	query := make(url.Values)
	if teamID != "" {
		query.Set("teamId", teamID)
	}

	body := struct {
		Value string `json:"value"`
	}{Value: address}

	if err := doVercelRequest(
		ctx,
		http.MethodPatch,
		"/v1/domains/records/"+url.PathEscape(recordID),
		query,
		token,
		body,
		nil,
	); err != nil {
		return err
	}
	return nil
}

func doVercelRequest(
	ctx context.Context,
	method, endpoint string,
	query url.Values,
	token string,
	body, result any,
) error {
	requestURL, err := url.Parse(strings.TrimRight(vercelAPIBaseURL, "/") + endpoint)
	if err != nil {
		return err
	}
	requestURL.RawQuery = query.Encode()

	var requestBody io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return err
		}
		requestBody = bytes.NewReader(encoded)
	}

	req, err := http.NewRequestWithContext(ctx, method, requestURL.String(), requestBody)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := vercelAPIClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		message, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
		return fmt.Errorf("Vercel API returned %s: %s", resp.Status, strings.TrimSpace(string(message)))
	}
	if result == nil {
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(result); err != nil {
		return fmt.Errorf("decode Vercel API response: %w", err)
	}
	return nil
}

func dnsRecordName(domain string) (string, error) {
	domain = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(domain)), ".")
	if domain == dnsZone {
		return "@", nil
	}

	suffix := "." + dnsZone
	if !strings.HasSuffix(domain, suffix) {
		return "", fmt.Errorf("domain %q is outside DNS zone %s", domain, dnsZone)
	}
	return strings.TrimSuffix(domain, suffix), nil
}
