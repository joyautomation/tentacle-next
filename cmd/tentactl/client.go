package main

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"time"
)

type client struct {
	baseURL    string
	httpClient *http.Client
}

func newClient(serverURL string) *client {
	return &client{
		baseURL: serverURL + "/api/v1",
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

func (c *client) get(path string) ([]byte, error) {
	resp, err := c.httpClient.Get(c.baseURL + path)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("server error (%d): %s", resp.StatusCode, body)
	}
	return body, nil
}

func (c *client) postYAML(path string, data []byte, source string) ([]byte, error) {
	req, err := http.NewRequest("POST", c.baseURL+path, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-yaml")
	if source != "" {
		req.Header.Set("X-Config-Source", source)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("server error (%d): %s", resp.StatusCode, body)
	}
	return body, nil
}
