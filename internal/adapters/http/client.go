package http

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"qualflare-cli/internal/core/domain"
	"qualflare-cli/internal/core/ports"
	"time"
)

type Client struct {
	client   *http.Client
	config   ports.ConfigProvider
	endpoint string
}

func NewHTTPClient(config ports.ConfigProvider) *Client {
	return &Client{
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
		config:   config,
		endpoint: "http://127.0.0.1:8001/api/v1/collect",
	}
}

func (h *Client) SendReport(ctx context.Context, report *domain.Launch) error {
	jsonData, err := json.Marshal(report)
	if err != nil {
		return fmt.Errorf("failed to marshal report: %w", err)
	}

	fmt.Println("REQ:", string(jsonData))

	req, err := http.NewRequestWithContext(ctx, "POST", h.endpoint, bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "test-reporter-cli/1.0")

	if apiKey := h.config.GetAPIKey(); apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}

	resp, err := h.client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("API request failed with status %d", resp.StatusCode)
	}

	return nil
}
