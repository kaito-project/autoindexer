// Copyright (c) KAITO authors.
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package driftdetection

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

// RAGEngineClientImpl implements RAGEngineClient for KAITO RAG engines
type RAGEngineClientImpl struct {
	httpClient *http.Client
	maxRetries int
}

// NewRAGEngineClient creates a new RAG engine client
func NewRAGEngineClient(timeout time.Duration, maxRetries int) RAGEngineClient {
	return &RAGEngineClientImpl{
		httpClient: &http.Client{
			Timeout: timeout,
		},
		maxRetries: maxRetries,
	}
}

// DocumentListResponse represents the response from the RAG engine list documents API
type DocumentListResponse struct {
	TotalItems int `json:"total_items"`
	Documents  []struct {
		ID       string            `json:"id"`
		Metadata map[string]string `json:"metadata"`
	} `json:"documents"`
}

// GetDocumentCount gets the number of documents in the given index filtered by autoindexer
func (r *RAGEngineClientImpl) GetDocumentCount(ragEngineEndpoint, indexName, autoindexerName string) (int32, error) {
	var lastErr error

	for attempt := 0; attempt <= r.maxRetries; attempt++ {
		count, err := r.getDocumentCountAttempt(ragEngineEndpoint, indexName, autoindexerName)
		if err == nil {
			return count, nil
		}
		lastErr = err

		if attempt < r.maxRetries {
			// Exponential backoff
			time.Sleep(time.Duration(1<<attempt) * time.Second)
		}
	}

	return 0, fmt.Errorf("failed to get document count after %d attempts: %w", r.maxRetries+1, lastErr)
}

func (r *RAGEngineClientImpl) getDocumentCountAttempt(ragEngineEndpoint, indexName, autoindexerName string) (int32, error) {
	// Construct the URL for listing documents
	baseURL := fmt.Sprintf("%s/indexes/%s/documents", ragEngineEndpoint, url.QueryEscape(indexName))

	// Add query parameters
	params := url.Values{}
	params.Add("limit", "1") // We only need the count, not the actual documents
	params.Add("offset", "0")
	// Add metadata filter for autoindexer
	params.Add("metadata_filter", fmt.Sprintf(`{"autoindexer": "%s"}`, autoindexerName))

	requestURL := fmt.Sprintf("%s?%s", baseURL, params.Encode())

	// Make the HTTP request
	resp, err := r.httpClient.Get(requestURL)
	if err != nil {
		return 0, fmt.Errorf("failed to make HTTP request to RAG engine: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return 0, fmt.Errorf("RAG engine returned non-200 status: %d, body: %s", resp.StatusCode, string(body))
	}

	// Read and parse the response
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, fmt.Errorf("failed to read response body: %w", err)
	}

	var response DocumentListResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return 0, fmt.Errorf("failed to parse response JSON: %w", err)
	}

	return int32(response.TotalItems), nil
}

// generateRAGEngineEndpoint creates the RAGEngine service endpoint URL from AutoIndexer
func generateRAGEngineEndpoint(ragEngineName, namespace string) string {
	// Assume RAGEngine service follows the naming convention: <ragengine-name>.<namespace>.svc.cluster.local:80
	return fmt.Sprintf("http://%s.%s.svc.cluster.local:80", ragEngineName, namespace)
}
