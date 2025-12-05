package utils

import (
	"os"
	"path/filepath"

	autoindexerv1alpha1 "github.com/kaito-project/autoindexer/api/v1alpha1"
	kaitov1alpha1 "github.com/kaito-project/kaito/api/v1alpha1"

	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/dynamic"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

var (
	scheme         = runtime.NewScheme()
	TestingCluster = NewCluster(scheme)
)

// Cluster object defines the required clients of the test cluster.
type Cluster struct {
	Scheme        *runtime.Scheme
	KubeClient    client.Client
	DynamicClient dynamic.Interface
	RestConfig    *rest.Config
}

func NewCluster(scheme *runtime.Scheme) *Cluster {
	return &Cluster{
		Scheme: scheme,
	}
}

// GetClusterClient returns a Cluster client for the cluster.
func GetClusterClient(cluster *Cluster) error {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(autoindexerv1alpha1.AddToScheme(scheme))
	utilruntime.Must(kaitov1alpha1.AddToScheme(scheme))

	var err error
	kubeconfig := filepath.Join(
		homeDir(), ".kube", "config",
	)
	clientcmd.BuildConfigFromFlags("", kubeconfig)
	cluster.RestConfig, err = clientcmd.BuildConfigFromFlags("", kubeconfig)
	if err != nil {
		return err
	}

	cluster.KubeClient, err = client.New(cluster.RestConfig, client.Options{Scheme: cluster.Scheme})
	if err != nil {
		return err
	}

	cluster.DynamicClient, err = dynamic.NewForConfig(cluster.RestConfig)
	if err != nil {
		return err
	}
	return nil
}

func homeDir() string {
	if h := os.Getenv("HOME"); h != "" {
		return h
	}
	return os.Getenv("USERPROFILE") // Windows fallback
}
