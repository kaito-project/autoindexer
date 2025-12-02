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

package v1alpha1

import (
	"context"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestDriftRemediationPolicy_Validation(t *testing.T) {
	testCases := []struct {
		name        string
		policy      *DriftRemediationPolicy
		wantErr     bool
		description string
	}{
		{
			name: "valid Auto strategy",
			policy: &DriftRemediationPolicy{
				Strategy: DriftRemediationStrategyAuto,
			},
			wantErr:     false,
			description: "Auto strategy should be valid",
		},
		{
			name: "valid Manual strategy", 
			policy: &DriftRemediationPolicy{
				Strategy: DriftRemediationStrategyManual,
			},
			wantErr:     false,
			description: "Manual strategy should be valid",
		},
		{
			name: "valid Ignore strategy",
			policy: &DriftRemediationPolicy{
				Strategy: DriftRemediationStrategyIgnore,
			},
			wantErr:     false,
			description: "Ignore strategy should be valid",
		},
		{
			name:        "nil policy should be valid (defaults to Manual)",
			policy:      nil,
			wantErr:     false,
			description: "Nil policy should be valid, defaulting to Manual strategy",
		},
		{
			name: "empty strategy should be invalid",
			policy: &DriftRemediationPolicy{
				Strategy: "",
			},
			wantErr:     true,
			description: "Empty strategy should be invalid",
		},
		{
			name: "invalid strategy should be invalid",
			policy: &DriftRemediationPolicy{
				Strategy: DriftRemediationStrategy("Invalid"),
			},
			wantErr:     true,
			description: "Invalid strategy value should be rejected",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			autoIndexer := &AutoIndexer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test",
					Namespace: "default",
				},
				Spec: AutoIndexerSpec{
					RAGEngine: "test-ragengine",
					IndexName: "test-index",
					DataSource: DataSourceSpec{
						Type: DataSourceTypeGit,
						Git: &GitDataSourceSpec{
							Repository: "https://github.com/example/repo",
							Branch:     "main",
						},
					},
					DriftRemediationPolicy: tc.policy,
				},
			}

			err := autoIndexer.validateCreate()

			if tc.wantErr && err == nil {
				t.Errorf("Expected validation error for %s, but got none", tc.description)
			}
			if !tc.wantErr && err != nil {
				t.Errorf("Expected no validation error for %s, but got: %v", tc.description, err)
			}
		})
	}
}

func TestDriftRemediationPolicy_ValidateFunction(t *testing.T) {
	testCases := []struct {
		name     string
		policy   *DriftRemediationPolicy
		wantErr  bool
		errField string
	}{
		{
			name: "valid Auto strategy",
			policy: &DriftRemediationPolicy{
				Strategy: DriftRemediationStrategyAuto,
			},
			wantErr: false,
		},
		{
			name: "valid Manual strategy",
			policy: &DriftRemediationPolicy{
				Strategy: DriftRemediationStrategyManual,
			},
			wantErr: false,
		},
		{
			name: "valid Ignore strategy",
			policy: &DriftRemediationPolicy{
				Strategy: DriftRemediationStrategyIgnore,
			},
			wantErr: false,
		},
		{
			name:    "nil policy",
			policy:  nil,
			wantErr: false,
		},
		{
			name: "empty strategy",
			policy: &DriftRemediationPolicy{
				Strategy: "",
			},
			wantErr:  true,
			errField: "strategy",
		},
		{
			name: "invalid strategy",
			policy: &DriftRemediationPolicy{
				Strategy: DriftRemediationStrategy("InvalidStrategy"),
			},
			wantErr:  true,
			errField: "strategy",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.policy.validate()

			if tc.wantErr && err == nil {
				t.Errorf("Expected validation error, but got none")
			}
			if !tc.wantErr && err != nil {
				t.Errorf("Expected no validation error, but got: %v", err)
			}
			if tc.wantErr && err != nil && tc.errField != "" {
				// Check that the error is about the expected field
				errStr := err.Error()
				if !contains(errStr, tc.errField) {
					t.Errorf("Expected error to mention field %s, but got: %v", tc.errField, err)
				}
			}
		})
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || (len(s) > len(substr) && 
		(s[:len(substr)] == substr || s[len(s)-len(substr):] == substr || 
		 indexOf(s, substr) >= 0)))
}

func indexOf(s, substr string) int {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}

func TestGitDataSourceSpec_ValidatePatterns(t *testing.T) {
	testCases := []struct {
		name         string
		paths        []string
		excludePaths []string
		wantErr      bool
		description  string
	}{
		{
			name:        "valid simple patterns",
			paths:       []string{"src", "foo/bar", "docs/api"},
			wantErr:     false,
			description: "Simple directory and file patterns should be valid",
		},
		{
			name:        "valid star patterns",
			paths:       []string{"*.go", "*.py", "src/*.js", "docs/*.md"},
			wantErr:     false,
			description: "Single star patterns for extensions and directory files should be valid",
		},
		{
			name:        "valid globstar patterns",
			paths:       []string{"**/foo", "**/src/main", "**/docs/api"},
			wantErr:     false,
			description: "Globstar patterns matching anywhere in tree should be valid",
		},
		{
			name:        "valid globstar extension patterns",
			paths:       []string{"**/*.go", "**/*.py", "**/*.js", "**/*.md"},
			wantErr:     false,
			description: "Globstar with extension patterns should be valid",
		},
		{
			name:        "valid trailing globstar patterns",
			paths:       []string{"src/**", "docs/**", "api/v1/**"},
			wantErr:     false,
			description: "Trailing globstar patterns should be valid",
		},
		{
			name:        "valid middle globstar patterns",
			paths:       []string{"src/**/main", "docs/**/api", "a/**/b"},
			wantErr:     false,
			description: "Middle globstar patterns should be valid",
		},
		{
			name:        "valid single star pattern",
			paths:       []string{"*"},
			wantErr:     false,
			description: "Single star should be valid",
		},
		{
			name: "valid mixed patterns",
			paths: []string{
				"src", "*.go", "foo/bar", "foo/**", "**/main.py",
				"docs/*.md", "api/**/handler", "test/**",
			},
			wantErr:     false,
			description: "Mix of all valid pattern types should work",
		},
		{
			name:        "invalid double slash",
			paths:       []string{"foo//bar"},
			wantErr:     true,
			description: "Double slashes should be invalid",
		},
		{
			name:        "invalid special characters",
			paths:       []string{"foo@bar", "src#docs"},
			wantErr:     true,
			description: "Special characters should be invalid",
		},
		{
			name:        "empty pattern",
			paths:       []string{""},
			wantErr:     true,
			description: "Empty patterns should be invalid",
		},
		{
			name: "valid exclude patterns",
			excludePaths: []string{
				"node_modules/**", "*.log", "test/**",
				"**/temp", "build/*", "**/*.tmp",
			},
			wantErr:     false,
			description: "Valid exclude patterns should work",
		},
		{
			name:         "invalid exclude patterns",
			excludePaths: []string{"foo//bar", "invalid@pattern"},
			wantErr:      true,
			description:  "Invalid exclude patterns should fail validation",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			gitSpec := &GitDataSourceSpec{
				Repository:   "https://github.com/example/repo.git",
				Branch:       "main",
				Paths:        tc.paths,
				ExcludePaths: tc.excludePaths,
			}

			err := gitSpec.validate()
			if tc.wantErr && err == nil {
				t.Errorf("expected error for %s, got nil. Description: %s", tc.name, tc.description)
			}
			if !tc.wantErr && err != nil {
				t.Errorf("unexpected error for %s: %v. Description: %s", tc.name, err, tc.description)
			}
		})
	}
}

func TestAutoIndexer_Validate(t *testing.T) {
	validGit := &GitDataSourceSpec{
		Repository:   "https://github.com/example/repo.git",
		Branch:       "main",
		Paths:        []string{"src", "*.go", "foo/bar", "foo/**"},
		ExcludePaths: []string{"test", "*.md"},
	}
	validStatic := &StaticDataSourceSpec{
		URLs: []string{"https://example.com/file1.txt", "https://example.com/file2.pdf"},
	}
	validSpec := AutoIndexerSpec{
		RAGEngine:  "rag",
		IndexName:  "my-index",
		DataSource: DataSourceSpec{Type: DataSourceTypeGit, Git: validGit},
	}
	valid := &AutoIndexer{
		ObjectMeta: metav1.ObjectMeta{Name: "ai", Namespace: "default"},
		Spec:       validSpec,
	}
	cases := []struct {
		name    string
		mutate  func(a *AutoIndexer)
		wantErr bool
	}{
		{"valid git", nil, false},
		{"missing ragengine name", func(a *AutoIndexer) { a.Spec.RAGEngine = "" }, true},
		{"missing index name", func(a *AutoIndexer) { a.Spec.IndexName = "" }, true},
		{"invalid index name (bad char)", func(a *AutoIndexer) { a.Spec.IndexName = "bad!name" }, true},
		{"invalid index name (starts with dash)", func(a *AutoIndexer) { a.Spec.IndexName = "-badname" }, true},
		{"invalid index name (ends with dash)", func(a *AutoIndexer) { a.Spec.IndexName = "badname-" }, true},
		{"missing datasource type", func(a *AutoIndexer) { a.Spec.DataSource.Type = "" }, true},
		{"missing git block", func(a *AutoIndexer) { a.Spec.DataSource.Git = nil }, true},
		{"missing git repo url", func(a *AutoIndexer) { a.Spec.DataSource.Git.Repository = "" }, true},
		{"missing git branch", func(a *AutoIndexer) { a.Spec.DataSource.Git.Branch = "" }, true},
		{"invalid git path", func(a *AutoIndexer) { a.Spec.DataSource.Git.Paths = []string{"foo//bar"} }, true},
		{"invalid git exclude path", func(a *AutoIndexer) { a.Spec.DataSource.Git.ExcludePaths = []string{"foo//bar"} }, true},
		{"valid static", func(a *AutoIndexer) {
			a.Spec.DataSource.Type = DataSourceTypeStatic
			a.Spec.DataSource.Git = nil
			a.Spec.DataSource.Static = validStatic
		}, false},
		{"missing static endpoints", func(a *AutoIndexer) {
			a.Spec.DataSource.Type = DataSourceTypeStatic
			a.Spec.DataSource.Git = nil
			a.Spec.DataSource.Static = &StaticDataSourceSpec{URLs: nil}
		}, true},
		{"invalid static endpoint url", func(a *AutoIndexer) {
			a.Spec.DataSource.Type = DataSourceTypeStatic
			a.Spec.DataSource.Git = nil
			a.Spec.DataSource.Static = &StaticDataSourceSpec{URLs: []string{"not a url"}}
		}, true},
		{"credentials missing type", func(a *AutoIndexer) {
			a.Spec.Credentials = &CredentialsSpec{Type: ""}
		}, true},
		{"credentials secretref missing", func(a *AutoIndexer) {
			a.Spec.Credentials = &CredentialsSpec{Type: "SecretRef", SecretRef: nil}
		}, true},
		{"credentials secretref missing name", func(a *AutoIndexer) {
			a.Spec.Credentials = &CredentialsSpec{Type: "SecretRef", SecretRef: &SecretKeyRef{Key: "foo"}}
		}, true},
		{"credentials secretref missing key", func(a *AutoIndexer) {
			a.Spec.Credentials = &CredentialsSpec{Type: "SecretRef", SecretRef: &SecretKeyRef{Name: "foo"}}
		}, true},
		{"schedule invalid", func(a *AutoIndexer) {
			s := "bad"
			a.Spec.Schedule = &s
		}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a := valid.DeepCopy()
			if tc.mutate != nil {
				tc.mutate(a)
			}
			err := a.Validate(context.Background())
			if tc.wantErr && err == nil {
				t.Errorf("expected error, got nil")
			}
			if !tc.wantErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}
