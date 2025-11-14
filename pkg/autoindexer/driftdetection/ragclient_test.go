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
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewRAGEngineClient(t *testing.T) {
	client := NewRAGEngineClient(30*time.Second, 3)
	assert.NotNil(t, client)
	
	impl, ok := client.(*RAGEngineClientImpl)
	require.True(t, ok)
	assert.Equal(t, 30*time.Second, impl.httpClient.Timeout)
	assert.Equal(t, 3, impl.maxRetries)
}

func TestRAGEngineClient_GetDocumentCount_Success(t *testing.T) {
	// Mock response
	response := DocumentListResponse{
		TotalItems: 42,
		Documents: []struct {
			ID       string            `json:"id"`
			Metadata map[string]string `json:"metadata"`
		}{},
	}
	
	// Create test server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/index/test-index/documents", r.URL.Path)
		assert.Equal(t, "1", r.URL.Query().Get("limit"))
		assert.Equal(t, "0", r.URL.Query().Get("offset"))
		assert.Contains(t, r.URL.Query().Get("metadata_filter"), `"autoindexer": "test-autoindexer"`)
		
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()
	
	client := NewRAGEngineClient(30*time.Second, 1)
	count, err := client.GetDocumentCount(server.URL, "test-index", "test-autoindexer")
	
	require.NoError(t, err)
	assert.Equal(t, int32(42), count)
}

func TestRAGEngineClient_GetDocumentCount_HTTPError(t *testing.T) {
	// Create test server that returns 500
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("Internal Server Error"))
	}))
	defer server.Close()
	
	client := NewRAGEngineClient(30*time.Second, 1)
	count, err := client.GetDocumentCount(server.URL, "test-index", "test-autoindexer")
	
	assert.Error(t, err)
	assert.Equal(t, int32(0), count)
	assert.Contains(t, err.Error(), "RAG engine returned non-200 status: 500")
}

func TestRAGEngineClient_GetDocumentCount_InvalidJSON(t *testing.T) {
	// Create test server that returns invalid JSON
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte("invalid json"))
	}))
	defer server.Close()
	
	client := NewRAGEngineClient(30*time.Second, 1)
	count, err := client.GetDocumentCount(server.URL, "test-index", "test-autoindexer")
	
	assert.Error(t, err)
	assert.Equal(t, int32(0), count)
	assert.Contains(t, err.Error(), "failed to parse response JSON")
}

func TestGenerateRAGEngineEndpoint(t *testing.T) {
	endpoint := generateRAGEngineEndpoint("my-rag-engine", "default")
	expected := "http://my-rag-engine.default.svc.cluster.local:80"
	assert.Equal(t, expected, endpoint)
}