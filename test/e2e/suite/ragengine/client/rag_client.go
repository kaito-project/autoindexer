package client

import (
	"encoding/json"
	"fmt"
	"net/http"
)

type RAGEngineClient struct {
	BaseURL    string
	httpClient *http.Client
}

func NewRAGEngineClient(baseURL string) *RAGEngineClient {
	return &RAGEngineClient{
		BaseURL:    baseURL,
		httpClient: &http.Client{},
	}
}

func (c *RAGEngineClient) ListIndexes() ([]string, error) {
	endpoint := c.BaseURL + "/indexes"
	resp, err := c.httpClient.Get(endpoint)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("list indexes failed: %s", resp.Status)
	}
	var indexes []string
	if err := json.NewDecoder(resp.Body).Decode(&indexes); err != nil {
		return nil, err
	}
	return indexes, nil
}

