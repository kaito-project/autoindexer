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
		DataSource: DataSourceSpec{Type: DataSourceTypeGitHub, Git: validGit},
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
