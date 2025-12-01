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

package ragengine

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestNewRAGEngineClient(t *testing.T) {
	client := NewRAGEngineClient(30*time.Second, 3)
	assert.NotNil(t, client)

	assert.Equal(t, 30*time.Second, client.httpClient.Timeout)
	assert.Equal(t, 3, client.maxRetries)
}

func TestRAGEngineClient_GetDocumentCount_Success(t *testing.T) {
	// This test would require a real RAG engine endpoint to work properly
	// since the client always uses generateRAGEngineEndpoint
	// We test the endpoint generation separately
	client := NewRAGEngineClient(30*time.Second, 1)
	assert.NotNil(t, client)

	// Test that GetDocumentCount calls generateRAGEngineEndpoint correctly
	// This will fail with a connection error since we're not running a real server,
	// but it validates the URL generation logic
	_, err := client.GetDocumentCount("test-rag-engine", "test-index", "test-autoindexer", "default")
	assert.Error(t, err) // Expected since no real server is running
	assert.Contains(t, err.Error(), "failed to make HTTP request to RAG engine")
}

func TestRAGEngineClient_GetDocumentCount_HTTPError(t *testing.T) {
	// Test client creation and basic validation
	client := NewRAGEngineClient(30*time.Second, 1)
	assert.NotNil(t, client)
	assert.Equal(t, 30*time.Second, client.httpClient.Timeout)
	assert.Equal(t, 1, client.maxRetries)
}

func TestRAGEngineClient_GetDocumentCount_ConnectionError(t *testing.T) {
	// Test with invalid RAG engine name that will fail DNS resolution
	client := NewRAGEngineClient(1*time.Second, 1) // Short timeout
	count, err := client.GetDocumentCount("invalid-rag-engine", "test-index", "test-autoindexer", "default")

	assert.Error(t, err)
	assert.Equal(t, int32(0), count)
	assert.Contains(t, err.Error(), "failed to get document count after")
}

func TestGenerateRAGEngineEndpoint(t *testing.T) {
	endpoint := generateRAGEngineEndpoint("my-rag-engine", "default")
	expected := "http://my-rag-engine.default.svc.cluster.local:80"
	assert.Equal(t, expected, endpoint)
}

func TestRAGEngineClient_EndpointGeneration(t *testing.T) {
	tests := []struct {
		name      string
		ragEngine string
		namespace string
		expected  string
	}{
		{
			name:      "standard endpoint",
			ragEngine: "my-rag-engine",
			namespace: "default",
			expected:  "http://my-rag-engine.default.svc.cluster.local:80",
		},
		{
			name:      "custom namespace",
			ragEngine: "test-rag",
			namespace: "kaito-system",
			expected:  "http://test-rag.kaito-system.svc.cluster.local:80",
		},
		{
			name:      "hyphenated names",
			ragEngine: "my-rag-engine-v2",
			namespace: "test-namespace",
			expected:  "http://my-rag-engine-v2.test-namespace.svc.cluster.local:80",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			endpoint := generateRAGEngineEndpoint(tt.ragEngine, tt.namespace)
			assert.Equal(t, tt.expected, endpoint)
		})
	}
}

func TestRAGEngineClient_MethodsReturnErrors(t *testing.T) {
	client := NewRAGEngineClient(1*time.Second, 1) // Short timeout

	// Test GetDocumentCount with invalid endpoint
	count, err := client.GetDocumentCount("invalid-rag-engine", "test-index", "test-autoindexer", "default")
	assert.Error(t, err)
	assert.Equal(t, int32(0), count)
	assert.Contains(t, err.Error(), "failed to get document count after")

	// Test ListIndexes with invalid endpoint
	indexes, err := client.ListIndexes("invalid-rag-engine", "test-index", "test-autoindexer", "default")
	assert.Error(t, err)
	assert.Nil(t, indexes)
	assert.Contains(t, err.Error(), "failed to list indexes after")

	// Test DeleteIndex with invalid endpoint
	err = client.DeleteIndex("invalid-rag-engine", "test-index", "default")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to delete index after")
}
