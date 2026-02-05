package actions

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"slices"
	"strings"
	"time"

	kaitov1alpha1 "github.com/kaito-project/kaito/api/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/tools/portforward"
	"k8s.io/client-go/transport/spdy"

	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kaito-project/autoindexer/test/e2e/manifests"
	ragclient "github.com/kaito-project/autoindexer/test/e2e/suite/ragengine/client"
	"github.com/kaito-project/autoindexer/test/e2e/suite/types"
)

func CreateRAGEngine(ragEngineName, ragEngineNamespace string) types.Action {
	return types.Action{
		Name: "Create RAG Engine",
		RunFunc: func(ctx context.Context, logger *slog.Logger, testContext *types.TestContext) error {
			logger.Info("Creating RAG Engine")
			ragManifext := manifests.CreateRAGEngineManifest(ragEngineName, ragEngineNamespace,
				manifests.DefaultCPUInstanceType, manifests.DefaultTestEmbeddingModelID,
				manifests.CreateRAGEngineInferenceServiceSpec(manifests.DefaultTestURL, manifests.DefaultTestContextWindowSize),
			)

			err := testContext.Cluster.KubeClient.Create(ctx, ragManifext)
			if client.IgnoreAlreadyExists(err) != nil {
				return fmt.Errorf("failed to create RAGEngine: %w", err)
			}

			err = wait.PollUntilContextTimeout(ctx, 10*time.Second, 15*time.Minute, true, func(ctx context.Context) (bool, error) {
				logger.Info("Waiting for RAG Engine deployment to become ready", "ragEngine", ragEngineName)
				var dep appsv1.Deployment
				if err := testContext.Cluster.KubeClient.Get(ctx, client.ObjectKey{
					Name:      ragEngineName,
					Namespace: ragEngineNamespace,
				}, &dep); err != nil {
					return false, client.IgnoreNotFound(err)
				}

				desired := int32(1)
				if dep.Spec.Replicas != nil {
					desired = *dep.Spec.Replicas
				}

				if dep.Status.ReadyReplicas == desired &&
					dep.Status.UpdatedReplicas == desired &&
					dep.Status.AvailableReplicas == desired {
					logger.Info("RAG Engine deployment is ready", "ragEngine", ragEngineName)
					return true, nil
				}

				logger.Info("RAG Engine deployment is not ready yet", "ragEngine", ragEngineName)
				return false, nil
			})
			if err != nil {
				return fmt.Errorf("RAGEngine deployment did not become ready in time: %w", err)
			}

			testContext.RAGClient = ragclient.NewRAGEngineClient(fmt.Sprintf("http://%s.%s.svc.cluster.local:8080", ragEngineName, ragEngineNamespace))

			return nil
		},
		CleanupFunc: func(ctx context.Context, logger *slog.Logger, testContext *types.TestContext) error {
			logger.Info("Deleting RAG Engine", "ragEngine", ragEngineName)
			var rag kaitov1alpha1.RAGEngine
			err := testContext.Cluster.KubeClient.Get(ctx, client.ObjectKey{
				Name:      ragEngineName,
				Namespace: ragEngineNamespace,
			}, &rag)

			if err != nil {
				if client.IgnoreNotFound(err) != nil {
					return fmt.Errorf("failed to get RAGEngine: %w", err)
				}
				logger.Info("RAG Engine not found, nothing to delete", "ragEngine", ragEngineName)
				return nil
			}

			err = testContext.Cluster.KubeClient.Delete(ctx, &rag)
			if err != nil {
				return fmt.Errorf("failed to delete RAGEngine: %w", err)
			}
			return nil
		},
	}
}

func ValidateIndexExistsInRAGEngine(ragEngineName, ragEngineNamespace, indexName string) types.Action {
	return types.Action{
		Name: "Validate Index Exists in RAG Engine",
		RunFunc: func(ctx context.Context, logger *slog.Logger, testContext *types.TestContext) error {
			logger.Info("Validating index exists in RAG Engine", "index", indexName)
			indexes, err := testContext.RAGClient.ListIndexes()
			if err != nil {
				return fmt.Errorf("failed to list indexes from rag engine: %w", err)
			}

			logger.Info("Indexes found in rag engine", "indexes", indexes)
			if slices.Contains(indexes, indexName) {
				logger.Info("Index found in rag engine", "index", indexName)
				return nil
			}

			return fmt.Errorf("index %s not found in rag engine", indexName)
		},
		CleanupFunc: func(ctx context.Context, logger *slog.Logger, testContext *types.TestContext) error {
			// No cleanup needed for validation action
			return nil
		},
	}
}

func StartPortForward(ragEngineName, ragEngineNamespace string, localPort int) types.Action {
	return types.Action{
		Name: "Start Port Forward to RAG Engine",
		RunFunc: func(ctx context.Context, logger *slog.Logger, testContext *types.TestContext) error {
			logger.Info("Setting up port forwarding to RAG Engine", "ragEngine", ragEngineName)
			pf, err := createPortForwarder(context.Background(), testContext, ragEngineNamespace, ragEngineName, localPort, 5000)
			if err != nil {
				return fmt.Errorf("failed to create port forwarder: %w", err)
			}
			testContext.PortForwarder = pf
			go func() {
				logger.Info("Starting port forwarding to RAG Engine", "ragEngine", ragEngineName)
				if err := testContext.PortForwarder.ForwardPorts(); err != nil {
					logger.Error("port forwarding to rag engine failed", "error", err)
				}
				logger.Info("Port forwarding to RAG Engine ended", "ragEngine", ragEngineName)
			}()

			time.Sleep(5 * time.Second)
			logger.Info("Updating RAG client base URL to use port forwarding", "ragEngine", ragEngineName)
			testContext.RAGClient.BaseURL = fmt.Sprintf("http://localhost:%d", localPort)

			return nil
		},
		CleanupFunc: func(ctx context.Context, logger *slog.Logger, testContext *types.TestContext) error {
			logger.Info("Stopping port forwarding to RAG Engine", "ragEngine", ragEngineName)
			if testContext.PortForwarder != nil {
				logger.Info("port fowarder is not nil, trying to safe close", "ragEngine", ragEngineName)
				safeClosePortForwarder(testContext.PortForwarder, logger)
			}
			return nil
		},
	}
}

func safeClosePortForwarder(pf *portforward.PortForwarder, logger *slog.Logger) {
	defer func() {
		if r := recover(); r != nil {
			logger.Warn("panic while closing port forwarder", "recover", r)
		}
	}()
	pf.Close()
}

func createPortForwarder(ctx context.Context, testContext *types.TestContext, namespace, podName string, localPort, podPort int) (*portforward.PortForwarder, error) {
	podList := &corev1.PodList{}
	if err := testContext.Cluster.KubeClient.List(ctx, podList,
		client.InNamespace(namespace),
		client.MatchingLabels(map[string]string{"kaito.sh/ragengine": podName}),
	); err != nil {
		return nil, err
	}

	if len(podList.Items) == 0 {
		return nil, fmt.Errorf("no pods match selector for service %s/%s", namespace, podName)
	}

	// pick first running pod
	fullPodName := ""
	for _, p := range podList.Items {
		if p.Status.Phase == corev1.PodRunning {
			fullPodName = p.Name
			break
		}
	}

	if fullPodName == "" {
		return nil, fmt.Errorf("no running pods found for service %s/%s", namespace, podName)
	}

	transport, upgrader, err := spdy.RoundTripperFor(testContext.Cluster.RestConfig)
	if err != nil {
		return nil, err
	}

	path := fmt.Sprintf("/api/v1/namespaces/%s/pods/%s/portforward", namespace, fullPodName)
	hostIP := strings.TrimPrefix(testContext.Cluster.RestConfig.Host, "https://")
	serverURL := &url.URL{Scheme: "https", Host: hostIP, Path: path}

	dialer := spdy.NewDialer(upgrader, &http.Client{Transport: transport}, http.MethodPost, serverURL)

	ports := []string{fmt.Sprintf("%d:%d", localPort, podPort)}
	out := os.Stdout
	errOut := os.Stderr

	return portforward.New(dialer, ports, make(chan struct{}), make(chan struct{}), out, errOut)
}
