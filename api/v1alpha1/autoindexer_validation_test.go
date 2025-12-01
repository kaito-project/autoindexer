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
