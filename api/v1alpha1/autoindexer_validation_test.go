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
		{"valid workloadidentity credentials", func(a *AutoIndexer) {
			a.Spec.Credentials = &CredentialsSpec{
				Type: "WorkloadIdentity",
				WorkloadIdentityRef: &WorkloadIdentityRef{
					CloudProvider: "Azure",
					AzureWorkloadIdentityRef: &AzureWorkloadIdentityRef{
						ServiceAccountName: "my-service-account",
						ClientID:           "12345678-1234-1234-1234-123456789abc",
						Scope:              "https://storage.azure.com/.default",
						TenantID:           "87654321-4321-4321-4321-cba987654321",
					},
				},
			}
		}, false},
		{"credentials workloadidentity missing ref", func(a *AutoIndexer) {
			a.Spec.Credentials = &CredentialsSpec{Type: "WorkloadIdentity", WorkloadIdentityRef: nil}
		}, true},
		{"credentials workloadidentity missing cloudprovider", func(a *AutoIndexer) {
			a.Spec.Credentials = &CredentialsSpec{
				Type:                "WorkloadIdentity",
				WorkloadIdentityRef: &WorkloadIdentityRef{},
			}
		}, true},
		{"credentials workloadidentity invalid cloudprovider", func(a *AutoIndexer) {
			a.Spec.Credentials = &CredentialsSpec{
				Type: "WorkloadIdentity",
				WorkloadIdentityRef: &WorkloadIdentityRef{
					CloudProvider: "InvalidProvider",
				},
			}
		}, true},
		{"credentials workloadidentity missing azure ref", func(a *AutoIndexer) {
			a.Spec.Credentials = &CredentialsSpec{
				Type: "WorkloadIdentity",
				WorkloadIdentityRef: &WorkloadIdentityRef{
					CloudProvider:            "Azure",
					AzureWorkloadIdentityRef: nil,
				},
			}
		}, true},
		{"credentials workloadidentity missing tenantId", func(a *AutoIndexer) {
			a.Spec.Credentials = &CredentialsSpec{
				Type: "WorkloadIdentity",
				WorkloadIdentityRef: &WorkloadIdentityRef{
					CloudProvider: "Azure",
					AzureWorkloadIdentityRef: &AzureWorkloadIdentityRef{
						ServiceAccountName: "my-service-account",
						ClientID:           "12345678-1234-1234-1234-123456789abc",
						Scope:              "https://storage.azure.com/.default",
					},
				},
			}
		}, true},
		{"credentials workloadidentity missing scope", func(a *AutoIndexer) {
			a.Spec.Credentials = &CredentialsSpec{
				Type: "WorkloadIdentity",
				WorkloadIdentityRef: &WorkloadIdentityRef{
					CloudProvider: "Azure",
					AzureWorkloadIdentityRef: &AzureWorkloadIdentityRef{
						ServiceAccountName: "my-service-account",
						ClientID:           "12345678-1234-1234-1234-123456789abc",
						TenantID:           "87654321-4321-4321-4321-cba987654321",
					},
				},
			}
		}, true},
		{"credentials workloadidentity missing serviceaccount", func(a *AutoIndexer) {
			a.Spec.Credentials = &CredentialsSpec{
				Type: "WorkloadIdentity",
				WorkloadIdentityRef: &WorkloadIdentityRef{
					CloudProvider: "Azure",
					AzureWorkloadIdentityRef: &AzureWorkloadIdentityRef{
						ClientID: "12345678-1234-1234-1234-123456789abc",
						Scope:    "https://storage.azure.com/.default",
						TenantID: "87654321-4321-4321-4321-cba987654321",
					},
				},
			}
		}, true},
		{"credentials workloadidentity missing clientid", func(a *AutoIndexer) {
			a.Spec.Credentials = &CredentialsSpec{
				Type: "WorkloadIdentity",
				WorkloadIdentityRef: &WorkloadIdentityRef{
					CloudProvider: "Azure",
					AzureWorkloadIdentityRef: &AzureWorkloadIdentityRef{
						ServiceAccountName: "my-service-account",
						Scope:              "https://storage.azure.com/.default",
					},
				},
			}
		}, true},
		{"credentials workloadidentity invalid clientid", func(a *AutoIndexer) {
			a.Spec.Credentials = &CredentialsSpec{
				Type: "WorkloadIdentity",
				WorkloadIdentityRef: &WorkloadIdentityRef{
					CloudProvider: "Azure",
					AzureWorkloadIdentityRef: &AzureWorkloadIdentityRef{
						ServiceAccountName: "my-service-account",
						ClientID:           "invalid-uuid",
						Scope:              "https://storage.azure.com/.default",
					},
				},
			}
		}, true},
		{"credentials workloadidentity invalid tenantid", func(a *AutoIndexer) {
			invalidTenantID := "invalid-tenant-uuid"
			a.Spec.Credentials = &CredentialsSpec{
				Type: "WorkloadIdentity",
				WorkloadIdentityRef: &WorkloadIdentityRef{
					CloudProvider: "Azure",
					AzureWorkloadIdentityRef: &AzureWorkloadIdentityRef{
						ServiceAccountName: "my-service-account",
						ClientID:           "12345678-1234-1234-1234-123456789abc",
						Scope:              "https://storage.azure.com/.default",
						TenantID:           invalidTenantID,
					},
				},
			}
		}, true},
		{"credentials invalid type", func(a *AutoIndexer) {
			a.Spec.Credentials = &CredentialsSpec{Type: "InvalidType"}
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

func TestDatabaseDataSourceSpec_Validation(t *testing.T) {
	testCases := []struct {
		name        string
		autoIndexer *AutoIndexer
		wantErr     bool
		description string
	}{
		{
			name: "valid Database data source with both queries",
			autoIndexer: &AutoIndexer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test",
					Namespace: "default",
				},
				Spec: AutoIndexerSpec{
					RAGEngine: "test-ragengine",
					IndexName: "test-index",
					DataSource: DataSourceSpec{
						Type: DataSourceTypeDatabase,
						Database: &DatabaseDataSourceSpec{
							Language:         "Kusto",
							InitialQuery:     "cluster('https://fake.kusto.windows.net').database('TestDB') | take 10",
							IncrementalQuery: "cluster('https://fake.kusto.windows.net').database('TestDB') | where timestamp > datetime($LAST_INDEXING_TIMESTAMP)",
						},
					},
				},
			},
			wantErr:     false,
			description: "Valid Database data source with proper queries should pass validation",
		},
		{
			name: "Database data source missing initialQuery",
			autoIndexer: &AutoIndexer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test",
					Namespace: "default",
				},
				Spec: AutoIndexerSpec{
					RAGEngine: "test-ragengine",
					IndexName: "test-index",
					DataSource: DataSourceSpec{
						Type: DataSourceTypeDatabase,
						Database: &DatabaseDataSourceSpec{
							Language:         "Kusto",
							IncrementalQuery: "cluster('https://fake.kusto.windows.net').database('TestDB') | where timestamp > datetime($LAST_INDEXING_TIMESTAMP)",
						},
					},
				},
			},
			wantErr:     true,
			description: "Database data source missing initialQuery should fail validation",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.autoIndexer.Validate(context.Background())
			if tc.wantErr && err == nil {
				t.Errorf("expected error for: %s, got nil", tc.description)
			}
			if !tc.wantErr && err != nil {
				t.Errorf("unexpected error for: %s, got %v", tc.description, err)
			}
		})
	}
}

func TestCredentialsSpec_Validation(t *testing.T) {
	testCases := []struct {
		name        string
		credentials *CredentialsSpec
		wantErr     bool
		description string
	}{
		{
			name:        "nil credentials should return error in validate function",
			credentials: nil,
			wantErr:     true,
			description: "Nil credentials should return error in direct validate call (but allowed in main validation)",
		},
		{
			name: "valid SecretRef credentials",
			credentials: &CredentialsSpec{
				Type: CredentialTypeSecretRef,
				SecretRef: &SecretKeyRef{
					Name: "my-secret",
					Key:  "password",
				},
			},
			wantErr:     false,
			description: "Valid SecretRef credentials should pass validation",
		},
		{
			name: "valid WorkloadIdentity credentials",
			credentials: &CredentialsSpec{
				Type: CredentialTypeWorkloadIdentity,
				WorkloadIdentityRef: &WorkloadIdentityRef{
					CloudProvider: CloudProviderAzure,
					AzureWorkloadIdentityRef: &AzureWorkloadIdentityRef{
						ServiceAccountName: "my-service-account",
						ClientID:           "12345678-1234-1234-1234-123456789abc",
						Scope:              "https://storage.azure.com/.default",
						TenantID:           "87654321-4321-4321-4321-cba987654321",
					},
				},
			},
			wantErr:     false,
			description: "Valid WorkloadIdentity credentials should pass validation",
		},
		{
			name: "missing credentials type",
			credentials: &CredentialsSpec{
				Type: "",
			},
			wantErr:     true,
			description: "Missing credentials type should fail validation",
		},
		{
			name: "invalid credentials type",
			credentials: &CredentialsSpec{
				Type: CredentialType("InvalidType"),
			},
			wantErr:     true,
			description: "Invalid credentials type should fail validation",
		},
		{
			name: "SecretRef missing secretRef field",
			credentials: &CredentialsSpec{
				Type:      CredentialTypeSecretRef,
				SecretRef: nil,
			},
			wantErr:     true,
			description: "SecretRef with missing secretRef field should fail validation",
		},
		{
			name: "SecretRef missing name",
			credentials: &CredentialsSpec{
				Type: CredentialTypeSecretRef,
				SecretRef: &SecretKeyRef{
					Key: "password",
				},
			},
			wantErr:     true,
			description: "SecretRef with missing name should fail validation",
		},
		{
			name: "SecretRef missing key",
			credentials: &CredentialsSpec{
				Type: CredentialTypeSecretRef,
				SecretRef: &SecretKeyRef{
					Name: "my-secret",
				},
			},
			wantErr:     true,
			description: "SecretRef with missing key should fail validation",
		},
		{
			name: "WorkloadIdentity missing workloadIdentityRef field",
			credentials: &CredentialsSpec{
				Type:                CredentialTypeWorkloadIdentity,
				WorkloadIdentityRef: nil,
			},
			wantErr:     true,
			description: "WorkloadIdentity with missing workloadIdentityRef field should fail validation",
		},
		{
			name: "WorkloadIdentity missing cloudProvider",
			credentials: &CredentialsSpec{
				Type:                CredentialTypeWorkloadIdentity,
				WorkloadIdentityRef: &WorkloadIdentityRef{},
			},
			wantErr:     true,
			description: "WorkloadIdentity with missing cloudProvider should fail validation",
		},
		{
			name: "WorkloadIdentity invalid cloudProvider",
			credentials: &CredentialsSpec{
				Type: CredentialTypeWorkloadIdentity,
				WorkloadIdentityRef: &WorkloadIdentityRef{
					CloudProvider: CloudProvder("InvalidProvider"),
				},
			},
			wantErr:     true,
			description: "WorkloadIdentity with invalid cloudProvider should fail validation",
		},
		{
			name: "WorkloadIdentity missing azureWorkloadIdentityRef",
			credentials: &CredentialsSpec{
				Type: CredentialTypeWorkloadIdentity,
				WorkloadIdentityRef: &WorkloadIdentityRef{
					CloudProvider:            CloudProviderAzure,
					AzureWorkloadIdentityRef: nil,
				},
			},
			wantErr:     true,
			description: "WorkloadIdentity with missing azureWorkloadIdentityRef should fail validation",
		},
		{
			name: "WorkloadIdentity missing scope",
			credentials: &CredentialsSpec{
				Type: CredentialTypeWorkloadIdentity,
				WorkloadIdentityRef: &WorkloadIdentityRef{
					CloudProvider: CloudProviderAzure,
					AzureWorkloadIdentityRef: &AzureWorkloadIdentityRef{
						ServiceAccountName: "my-service-account",
						ClientID:           "12345678-1234-1234-1234-123456789abc",
					},
				},
			},
			wantErr:     true,
			description: "WorkloadIdentity with missing scope should fail validation",
		},
		{
			name: "WorkloadIdentity missing serviceAccountName",
			credentials: &CredentialsSpec{
				Type: CredentialTypeWorkloadIdentity,
				WorkloadIdentityRef: &WorkloadIdentityRef{
					CloudProvider: CloudProviderAzure,
					AzureWorkloadIdentityRef: &AzureWorkloadIdentityRef{
						ClientID: "12345678-1234-1234-1234-123456789abc",
						Scope:    "https://storage.azure.com/.default",
					},
				},
			},
			wantErr:     true,
			description: "WorkloadIdentity with missing serviceAccountName should fail validation",
		},
		{
			name: "WorkloadIdentity missing clientID",
			credentials: &CredentialsSpec{
				Type: CredentialTypeWorkloadIdentity,
				WorkloadIdentityRef: &WorkloadIdentityRef{
					CloudProvider: CloudProviderAzure,
					AzureWorkloadIdentityRef: &AzureWorkloadIdentityRef{
						ServiceAccountName: "my-service-account",
						Scope:              "https://storage.azure.com/.default",
					},
				},
			},
			wantErr:     true,
			description: "WorkloadIdentity with missing clientID should fail validation",
		},
		{
			name: "WorkloadIdentity invalid clientID format",
			credentials: &CredentialsSpec{
				Type: CredentialTypeWorkloadIdentity,
				WorkloadIdentityRef: &WorkloadIdentityRef{
					CloudProvider: CloudProviderAzure,
					AzureWorkloadIdentityRef: &AzureWorkloadIdentityRef{
						ServiceAccountName: "my-service-account",
						ClientID:           "not-a-valid-uuid",
						Scope:              "https://storage.azure.com/.default",
					},
				},
			},
			wantErr:     true,
			description: "WorkloadIdentity with invalid clientID format should fail validation",
		},
		{
			name: "WorkloadIdentity invalid tenantID format",
			credentials: func() *CredentialsSpec {
				return &CredentialsSpec{
					Type: CredentialTypeWorkloadIdentity,
					WorkloadIdentityRef: &WorkloadIdentityRef{
						CloudProvider: CloudProviderAzure,
						AzureWorkloadIdentityRef: &AzureWorkloadIdentityRef{
							ServiceAccountName: "my-service-account",
							ClientID:           "12345678-1234-1234-1234-123456789abc",
							Scope:              "https://storage.azure.com/.default",
							TenantID:           "not-a-valid-uuid",
						},
					},
				}
			}(),
			wantErr:     true,
			description: "WorkloadIdentity with invalid tenantID format should fail validation",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.credentials.validate()

			if tc.wantErr && err == nil {
				t.Errorf("Expected validation error for %s, but got none. Description: %s", tc.name, tc.description)
			}
			if !tc.wantErr && err != nil {
				t.Errorf("Expected no validation error for %s, but got: %v. Description: %s", tc.name, err, tc.description)
			}
		})
	}
}
