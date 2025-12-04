package manifests

import (
	kaitov1alpha1 "github.com/kaito-project/kaito/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	DefaultTestEmbeddingModelID  = "BAAI/bge-small-en-v1.5"
	DefaultTestContextWindowSize = 16000
	DefaultTestURL               = "http://test-inference.kaito-system.svc.cluster.local/v1/chat/completions"
	DefaultCPUInstanceType       = "Standard_D8_v4"
)

func CreateRAGEngineManifest(name, namespace, instanceType, embeddingModelID string, inferenceSpec *kaitov1alpha1.InferenceServiceSpec) *kaitov1alpha1.RAGEngine {
	return &kaitov1alpha1.RAGEngine{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: &kaitov1alpha1.RAGEngineSpec{
			Compute: &kaitov1alpha1.ResourceSpec{
				InstanceType:  instanceType,
				LabelSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"apps": name}},
			},
			Embedding: &kaitov1alpha1.EmbeddingSpec{
				Local: &kaitov1alpha1.LocalEmbeddingSpec{
					ModelID: embeddingModelID,
				},
			},
			InferenceService: inferenceSpec,
		},
	}
}

func CreateRAGEngineInferenceServiceSpec(url string, contextWindowSize int) *kaitov1alpha1.InferenceServiceSpec {
	return &kaitov1alpha1.InferenceServiceSpec{
		URL:               url,
		ContextWindowSize: contextWindowSize,
	}
}
