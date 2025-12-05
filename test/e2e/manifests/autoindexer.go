package manifests

import (
	autoindexerv1alpha1 "github.com/kaito-project/autoindexer/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func CreateAutoIndexerManifest(name, namespace, ragEngine, indexName string, schedule *string,
	dataSource autoindexerv1alpha1.DataSourceSpec,
	credentials *autoindexerv1alpha1.CredentialsSpec,
	driftRemediationPolicy *autoindexerv1alpha1.DriftRemediationPolicy,
) *autoindexerv1alpha1.AutoIndexer {
	return &autoindexerv1alpha1.AutoIndexer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: autoindexerv1alpha1.AutoIndexerSpec{
			RAGEngine:              ragEngine,
			IndexName:              indexName,
			Schedule:               schedule,
			DataSource:             dataSource,
			Credentials:            credentials,
			DriftRemediationPolicy: driftRemediationPolicy,
		},
	}
}

func CreateGitDataSourceSpec(repoURL, branch string, commit *string, includePaths, excludePaths []string) autoindexerv1alpha1.DataSourceSpec {
	return autoindexerv1alpha1.DataSourceSpec{
		Type: autoindexerv1alpha1.DataSourceTypeGit,
		Git: &autoindexerv1alpha1.GitDataSourceSpec{
			Repository:   repoURL,
			Branch:       branch,
			Commit:       commit,
			Paths:        includePaths,
			ExcludePaths: excludePaths,
		},
	}
}

func CreateStaticDataSourceSpec(urls []string) autoindexerv1alpha1.DataSourceSpec {
	return autoindexerv1alpha1.DataSourceSpec{
		Type: autoindexerv1alpha1.DataSourceTypeStatic,
		Static: &autoindexerv1alpha1.StaticDataSourceSpec{
			URLs: urls,
		},
	}
}

func CreateCredentialsSpec(secretName, secretKeyRef string) *autoindexerv1alpha1.CredentialsSpec {
	return &autoindexerv1alpha1.CredentialsSpec{
		Type: autoindexerv1alpha1.CredentialTypeSecretRef,
		SecretRef: &autoindexerv1alpha1.SecretKeyRef{
			Name: secretName,
			Key:  secretKeyRef,
		},
	}
}

func CreateDriftRemediationPolicy(strategy autoindexerv1alpha1.DriftRemediationStrategy) *autoindexerv1alpha1.DriftRemediationPolicy {
	return &autoindexerv1alpha1.DriftRemediationPolicy{
		Strategy: strategy,
	}
}
