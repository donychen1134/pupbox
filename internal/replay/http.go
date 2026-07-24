package replay

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

const maxResponseBytes = 32 << 20

func normalizeBaseURL(value string) (*url.URL, error) {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil {
		return nil, fmt.Errorf("parse server URL: %w", err)
	}
	if (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return nil, errors.New("server URL must use http or https")
	}
	if parsed.User != nil {
		return nil, errors.New("server URL must not contain credentials")
	}
	parsed.RawQuery = ""
	parsed.Fragment = ""
	parsed.Path = strings.TrimRight(parsed.Path, "/")
	return parsed, nil
}

func endpointURL(base *url.URL, path string) string {
	copy := *base
	copy.Path = strings.TrimRight(base.Path, "/") + path
	copy.RawPath = ""
	copy.RawQuery = ""
	copy.Fragment = ""
	return copy.String()
}

func addAuth(req *http.Request, token string) {
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
}

func readResponse(resp *http.Response) ([]byte, error) {
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxResponseBytes {
		return nil, errors.New("response exceeds 32 MiB")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		message := strings.TrimSpace(string(data))
		if len(message) > 300 {
			message = message[:300]
		}
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, message)
	}
	return data, nil
}

func getJSON(ctx context.Context, client *http.Client, target, token string, output any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return err
	}
	addAuth(req, token)
	req.Header.Set("Accept", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	data, err := readResponse(resp)
	if err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	if err := decoder.Decode(output); err != nil {
		return fmt.Errorf("decode JSON response: %w", err)
	}
	return nil
}
