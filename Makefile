CONTROLLER_GEN ?= $(LOCALBIN)/controller-gen
CONTROLLER_TOOLS_VERSION ?= v0.15.0

.PHONY: localbin
localbin: $(LOCALBIN) ## Create folder for installing local binaries.

LOCALBIN ?= $(shell pwd)/bin
$(LOCALBIN): ## Create folder for installing local binaries.
	mkdir -p $(LOCALBIN)

.PHONY: controller-gen
controller-gen: $(CONTROLLER_GEN) ## Download controller-gen locally if necessary. If wrong version is installed, it will be overwritten.
$(CONTROLLER_GEN): $(LOCALBIN)
	test -s $(LOCALBIN)/controller-gen && $(LOCALBIN)/controller-gen --version | grep -q $(CONTROLLER_TOOLS_VERSION) || \
	GOBIN=$(LOCALBIN) go install sigs.k8s.io/controller-tools/cmd/controller-gen@$(CONTROLLER_TOOLS_VERSION)

.PHONY: generate
generate: controller-gen ## Generate code containing DeepCopy, DeepCopyInto, and DeepCopyObject method implementations.
	$(CONTROLLER_GEN) object:headerFile="hack/boilerplate.go.txt" paths="./..."

.PHONY: manifests
manifests: controller-gen ## Generate WebhookConfiguration, ClusterRole, and CustomResourceDefinition objects.
	$(CONTROLLER_GEN) rbac:roleName=manager-role crd webhook paths="./..." output:crd:artifacts:config=config/crd/bases
	cp config/crd/bases/autoindexer.kaito.sh_autoindexers.yaml charts/kaito/autoindexer/crds/

.PHONY: autoindexer-job-test
autoindexer-job-test: ## Run AutoIndexer job tests with pytest.
	pip install -r jobs/autoindexer/requirements-test.txt
	pip install pytest-cov
	pytest --cov -o log_cli=true -o log_cli_level=INFO jobs/autoindexer/tests

## --------------------------------------
## Docker
## --------------------------------------

REGISTRY ?=

AUTOINDEXER_JOB_IMG_NAME ?= autoindexer-job
AUTOINDEXER_JOB_IMG_TAG ?= latest

AUTOINDEXER_IMAGE_NAME ?= autoindexer
AUTOINDEXER_IMAGE_TAG ?= v0.0.1

BUILDX_BUILDER_NAME ?= img-builder
OUTPUT_TYPE ?= type=registry
QEMU_VERSION ?= 7.2.0-1
BUILDKIT_VERSION ?= v0.18.1
ARCH ?= amd64
BUILD_FLAGS ?=

.PHONY: docker-buildx
docker-buildx: ## Build and push docker image for the manager for cross-platform support.
	@if ! docker buildx ls | grep $(BUILDX_BUILDER_NAME); then \
		docker run --rm --privileged mcr.microsoft.com/mirror/docker/multiarch/qemu-user-static:$(QEMU_VERSION) --reset -p yes; \
		docker buildx create --name $(BUILDX_BUILDER_NAME) --driver-opt image=mcr.microsoft.com/oss/v2/moby/buildkit:$(BUILDKIT_VERSION) --use; \
		docker buildx inspect $(BUILDX_BUILDER_NAME) --bootstrap; \
	fi

.PHONY: docker-build-autoindexer-job
docker-build-autoindexer-job: docker-buildx ## Build Docker image for Auto Indexer job.
	docker buildx build \
        --platform="linux/$(ARCH)" \
        --output=$(OUTPUT_TYPE) \
        --file ./docker/autoindexer/job/Dockerfile \
        --pull \
		$(BUILD_FLAGS) \
        --tag $(REGISTRY)/$(AUTOINDEXER_JOB_IMG_NAME):$(AUTOINDEXER_JOB_IMG_TAG) .

.PHONY: docker-build-autoindexer
docker-build-autoindexer: docker-buildx ## Build Docker image for Auto Indexer.
	docker buildx build \
		--file ./docker/autoindexer/Dockerfile \
		--output=$(OUTPUT_TYPE) \
		--platform="linux/$(ARCH)" \
		--pull \
		$(BUILD_FLAGS) \
		--tag $(REGISTRY)/$(AUTOINDEXER_IMAGE_NAME):latest .


## --------------------------------------
## Installation
## --------------------------------------

HELM_INSTALL_EXTRA_ARGS ?=

KAITO_AUTOINDEXER_NAMESPACE ?= kaito-autoindexer

AZURE_CLUSTER_NAME ?= kaito-dogfood
AZURE_RESOURCE_GROUP ?= kaito-dogfood

.PHONY: az-patch-install-autoindexer-helm-e2e
az-patch-install-autoindexer-helm-e2e: ## Install Kaito AutoIndexer Helm chart for e2e tests and set Azure client env vars and settings in Helm values.
	az aks get-credentials --name $(AZURE_CLUSTER_NAME) --resource-group $(AZURE_RESOURCE_GROUP)

	yq -i '(.image.repository)                                              = "$(REGISTRY)/$(AUTOINDEXER_IMAGE_NAME)"' ./charts/kaito/autoindexer/values.yaml
	yq -i '(.image.tag)                                                     = "latest"'                                ./charts/kaito/autoindexer/values.yaml
	yq -i '(.clusterName)                                                   = "$(AZURE_CLUSTER_NAME)"'                 ./charts/kaito/autoindexer/values.yaml
	yq -i '(.presetAutoIndexerRegistryName)                                 = "$(REGISTRY)"'                           ./charts/kaito/autoindexer/values.yaml
	yq -i '(.presetAutoIndexerImageName)                                    = "$(AUTOINDEXER_JOB_IMG_NAME)"'           ./charts/kaito/autoindexer/values.yaml
	yq -i '(.presetAutoIndexerImageTag)                                     = "$(AUTOINDEXER_JOB_IMG_TAG)"'            ./charts/kaito/autoindexer/values.yaml

	helm install kaito-autoindexer ./charts/kaito/autoindexer --namespace $(KAITO_AUTOINDEXER_NAMESPACE) --create-namespace $(HELM_INSTALL_EXTRA_ARGS)

.PHONY: az-uninstall-autoindexer-helm
az-uninstall-autoindexer-helm: ## Uninstall Kaito AutoIndexer Helm chart from Azure AKS cluster.
	az aks get-credentials --name $(AZURE_CLUSTER_NAME) --resource-group $(AZURE_RESOURCE_GROUP)
	helm uninstall kaito-autoindexer --namespace $(KAITO_AUTOINDEXER_NAMESPACE)