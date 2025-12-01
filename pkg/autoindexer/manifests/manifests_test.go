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

package manifests

import (
	"strings"
	"testing"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/kaito-project/autoindexer/api/v1alpha1"
)

func createTestAutoIndexer(name, namespace string, schedule *string) *v1alpha1.AutoIndexer {
	return &v1alpha1.AutoIndexer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: v1alpha1.AutoIndexerSpec{
			RAGEngine: "test-ragengine",
			IndexName: "test-index",
			DataSource: v1alpha1.DataSourceSpec{
				Type: v1alpha1.DataSourceTypeGit,
				Git: &v1alpha1.GitDataSourceSpec{
					Repository: "https://github.com/example/test-repo",
					Branch:     "main",
					Paths:      []string{"docs/"},
				},
			},
			Schedule: schedule,
			Credentials: &v1alpha1.CredentialsSpec{
				Type: v1alpha1.CredentialTypeSecretRef,
				SecretRef: &v1alpha1.SecretKeyRef{
					Name: "github-credentials",
					Key:  "token",
				},
			},
		},
	}
}

func TestGenerateIndexingJobManifest(t *testing.T) {
	autoIndexer := createTestAutoIndexer("test-autoindexer", "default", nil)

	config := GetDefaultJobConfig(autoIndexer, JobTypeOneTime)
	job := GenerateIndexingJobManifest(config)

	// Validate basic job properties
	if job == nil {
		t.Fatal("Generated job is nil")
	}

	if job.Name != config.JobName {
		t.Errorf("Expected job name %s, got %s", config.JobName, job.Name)
	}

	if job.Namespace != autoIndexer.Namespace {
		t.Errorf("Expected job namespace %s, got %s", autoIndexer.Namespace, job.Namespace)
	}

	// Validate labels
	expectedLabels := getJobLabels(autoIndexer)
	for key, expectedValue := range expectedLabels {
		if value, exists := job.Labels[key]; !exists || value != expectedValue {
			t.Errorf("Expected label %s=%s, got %s (exists: %t)", key, expectedValue, value, exists)
		}
	}

	// Validate owner reference
	if len(job.OwnerReferences) != 1 {
		t.Fatalf("Expected 1 owner reference, got %d", len(job.OwnerReferences))
	}

	ownerRef := job.OwnerReferences[0]
	if ownerRef.Name != autoIndexer.Name {
		t.Errorf("Expected owner reference name %s, got %s", autoIndexer.Name, ownerRef.Name)
	}

	// Validate job spec
	if job.Spec.Template.Spec.RestartPolicy != corev1.RestartPolicyOnFailure {
		t.Errorf("Expected restart policy OnFailure, got %s", job.Spec.Template.Spec.RestartPolicy)
	}

	if len(job.Spec.Template.Spec.Containers) != 1 {
		t.Fatalf("Expected 1 container, got %d", len(job.Spec.Template.Spec.Containers))
	}

	container := job.Spec.Template.Spec.Containers[0]
	if container.Name != "autoindexer" {
		t.Errorf("Expected container name 'autoindexer', got %s", container.Name)
	}

	if container.Image != AutoIndexerImage {
		t.Errorf("Expected container image %s, got %s", AutoIndexerImage, container.Image)
	}

	// Validate environment variables
	envVarExists := func(name string) bool {
		for _, env := range container.Env {
			if env.Name == name {
				return true
			}
		}
		return false
	}

	expectedEnvVars := []string{
		EnvAutoIndexerName,
		EnvNamespace,
	}

	for _, envVar := range expectedEnvVars {
		if !envVarExists(envVar) {
			t.Errorf("Expected environment variable %s not found", envVar)
		}
	}

	// Check if ACCESS_SECRET env var is set correctly with SecretRef
	found := false
	for _, env := range container.Env {
		if env.Name == EnvAccessSecret {
			if env.ValueFrom != nil && env.ValueFrom.SecretKeyRef != nil {
				if env.ValueFrom.SecretKeyRef.Name == "github-credentials" && env.ValueFrom.SecretKeyRef.Key == "token" {
					found = true
					break
				}
			}
		}
	}
	if !found {
		t.Error("Expected ACCESS_SECRET environment variable with correct SecretRef not found")
	}
}

func TestGenerateIndexingCronJobManifest(t *testing.T) {
	schedule := "0 2 * * *"
	autoIndexer := createTestAutoIndexer("test-autoindexer", "default", &schedule)

	config := GetDefaultJobConfig(autoIndexer, JobTypeScheduled)
	cronJob := GenerateIndexingCronJobManifest(config)

	// Validate basic cronjob properties
	if cronJob == nil {
		t.Fatal("Generated cronjob is nil")
	}

	if cronJob.Name != config.JobName {
		t.Errorf("Expected cronjob name %s, got %s", config.JobName, cronJob.Name)
	}

	if cronJob.Namespace != autoIndexer.Namespace {
		t.Errorf("Expected cronjob namespace %s, got %s", autoIndexer.Namespace, cronJob.Namespace)
	}

	// Validate schedule
	if cronJob.Spec.Schedule != schedule {
		t.Errorf("Expected schedule %s, got %s", schedule, cronJob.Spec.Schedule)
	}

	// Validate concurrency policy
	if cronJob.Spec.ConcurrencyPolicy != batchv1.ForbidConcurrent {
		t.Errorf("Expected concurrency policy ForbidConcurrent, got %s", cronJob.Spec.ConcurrencyPolicy)
	}

	// Validate job template
	jobTemplate := cronJob.Spec.JobTemplate
	if len(jobTemplate.Spec.Template.Spec.Containers) != 1 {
		t.Fatalf("Expected 1 container in job template, got %d", len(jobTemplate.Spec.Template.Spec.Containers))
	}

	container := jobTemplate.Spec.Template.Spec.Containers[0]
	if container.Name != "autoindexer" {
		t.Errorf("Expected container name 'autoindexer', got %s", container.Name)
	}
}

func TestGenerateIndexingCronJobManifest_NoSchedule(t *testing.T) {
	autoIndexer := createTestAutoIndexer("test-autoindexer", "default", nil)

	config := GetDefaultJobConfig(autoIndexer, JobTypeScheduled)
	cronJob := GenerateIndexingCronJobManifest(config)

	// Should return nil when no schedule is provided
	if cronJob != nil {
		t.Error("Expected nil cronjob when no schedule is provided")
	}
}

func TestGenerateJobName(t *testing.T) {
	autoIndexer := createTestAutoIndexer("test-autoindexer", "default", nil)

	// Test one-time job name
	oneTimeJobName := GenerateJobName(autoIndexer, JobTypeOneTime)
	if !strings.HasPrefix(oneTimeJobName, "test-autoindexer-job-") {
		t.Errorf("One-time job name should start with 'test-autoindexer-job-', got %s", oneTimeJobName)
	}

	// Test scheduled job name
	scheduledJobName := GenerateJobName(autoIndexer, JobTypeScheduled)
	expectedScheduledName := "test-autoindexer-cronjob"
	if scheduledJobName != expectedScheduledName {
		t.Errorf("Expected scheduled job name %s, got %s", expectedScheduledName, scheduledJobName)
	}
}

func TestValidateJobConfig(t *testing.T) {
	autoIndexer := createTestAutoIndexer("test-autoindexer", "default", nil)

	// Valid config
	validConfig := JobConfig{
		AutoIndexer: autoIndexer,
		JobName:     "test-job",
		JobType:     JobTypeOneTime,
	}

	if err := ValidateJobConfig(validConfig); err != nil {
		t.Errorf("Valid config should not produce error, got: %v", err)
	}

	// Invalid config - nil AutoIndexer
	invalidConfig := JobConfig{
		AutoIndexer: nil,
		JobName:     "test-job",
		JobType:     JobTypeOneTime,
	}

	if err := ValidateJobConfig(invalidConfig); err == nil {
		t.Error("Config with nil AutoIndexer should produce error")
	}

	// Invalid config - empty job name
	invalidConfig.AutoIndexer = autoIndexer
	invalidConfig.JobName = ""

	if err := ValidateJobConfig(invalidConfig); err == nil {
		t.Error("Config with empty job name should produce error")
	}

	// Invalid config - invalid job type
	invalidConfig.JobName = "test-job"
	invalidConfig.JobType = "invalid"

	if err := ValidateJobConfig(invalidConfig); err == nil {
		t.Error("Config with invalid job type should produce error")
	}

	// Invalid config - scheduled job without schedule
	schedule := "0 2 * * *"
	autoIndexerWithSchedule := createTestAutoIndexer("test-autoindexer", "default", &schedule)
	autoIndexerWithoutSchedule := createTestAutoIndexer("test-autoindexer", "default", nil)

	invalidConfig.AutoIndexer = autoIndexerWithoutSchedule
	invalidConfig.JobType = JobTypeScheduled

	if err := ValidateJobConfig(invalidConfig); err == nil {
		t.Error("Scheduled job without schedule should produce error")
	}

	// Valid scheduled config
	validScheduledConfig := JobConfig{
		AutoIndexer: autoIndexerWithSchedule,
		JobName:     "test-cronjob",
		JobType:     JobTypeScheduled,
	}

	if err := ValidateJobConfig(validScheduledConfig); err != nil {
		t.Errorf("Valid scheduled config should not produce error, got: %v", err)
	}
}

func TestGetDefaultJobConfig(t *testing.T) {
	autoIndexer := createTestAutoIndexer("test-autoindexer", "default", nil)

	config := GetDefaultJobConfig(autoIndexer, JobTypeOneTime)

	if config.AutoIndexer != autoIndexer {
		t.Error("Default config should use provided AutoIndexer")
	}

	if config.JobType != JobTypeOneTime {
		t.Errorf("Expected job type %s, got %s", JobTypeOneTime, config.JobType)
	}

	if config.Image != AutoIndexerImage {
		t.Errorf("Expected image %s, got %s", AutoIndexerImage, config.Image)
	}

	if config.ImagePullPolicy != corev1.PullAlways {
		t.Errorf("Expected pull policy %s, got %s", corev1.PullAlways, config.ImagePullPolicy)
	}

	if config.JobName == "" {
		t.Error("Default config should generate a job name")
	}
}

func TestGetJobImageConfig(t *testing.T) {
	config := GetJobImageConfig()

	// Should return a valid image config
	if config.RegistryName == "" {
		t.Error("RegistryName should not be empty")
	}

	if config.ImageName == "" {
		t.Error("ImageName should not be empty")
	}

	if config.ImageTag == "" {
		t.Error("ImageTag should not be empty")
	}

	// Test that GetImage() method works
	image := config.GetImage()
	if image == "" {
		t.Error("GetImage() should return a non-empty string")
	}

	// Image should contain registry, name, and tag
	if !strings.Contains(image, config.RegistryName) {
		t.Errorf("Image should contain registry name %s", config.RegistryName)
	}
}

func TestGenerateEnvironmentVariables(t *testing.T) {
	autoIndexer := createTestAutoIndexer("test", "default", nil)

	envVars := generateEnvironmentVariables(autoIndexer)

	// Check that required environment variables are present
	envMap := make(map[string]string)
	var hasAccessSecret bool
	for _, env := range envVars {
		if env.Value != "" {
			envMap[env.Name] = env.Value
		}
		if env.Name == EnvAccessSecret {
			hasAccessSecret = true
		}
	}

	if envMap[EnvNamespace] != autoIndexer.Namespace {
		t.Errorf("Expected namespace %s, got %s", autoIndexer.Namespace, envMap[EnvNamespace])
	}

	if envMap[EnvAutoIndexerName] != autoIndexer.Name {
		t.Errorf("Expected autoindexer name %s, got %s", autoIndexer.Name, envMap[EnvAutoIndexerName])
	}

	// ACCESS_SECRET should be present when credentials are configured
	if !hasAccessSecret {
		t.Error("ACCESS_SECRET should be present when credentials are configured")
	}
}

func TestGenerateRAGEngineEndpoint(t *testing.T) {
	autoIndexer := createTestAutoIndexer("test", "default", nil)

	endpoint := generateRAGEngineEndpoint(autoIndexer)

	// Should generate a proper endpoint URL
	expectedEndpoint := "http://test-ragengine.default.svc.cluster.local:80"
	if endpoint != expectedEndpoint {
		t.Errorf("Expected endpoint %s, got %s", expectedEndpoint, endpoint)
	}
}

func TestGenerateDataSourceConfig(t *testing.T) {
	tests := []struct {
		name       string
		dataSource v1alpha1.DataSourceSpec
		wantErr    bool
	}{
		{
			name: "git data source",
			dataSource: v1alpha1.DataSourceSpec{
				Type: v1alpha1.DataSourceTypeGit,
				Git: &v1alpha1.GitDataSourceSpec{
					Repository: "https://github.com/example/test-repo",
					Branch:     "main",
					Paths:      []string{"docs/"},
				},
			},
			wantErr: false,
		},
		{
			name: "static data source",
			dataSource: v1alpha1.DataSourceSpec{
				Type: v1alpha1.DataSourceTypeStatic,
				Static: &v1alpha1.StaticDataSourceSpec{
					URLs: []string{"https://example.com/file1.txt", "https://example.com/file2.txt"},
				},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config, err := generateDataSourceConfig(tt.dataSource)

			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}

			if config == "" {
				t.Error("Config should not be empty")
			}

			// Config should be valid JSON
			if !strings.Contains(config, "{") {
				t.Error("Config should be JSON format")
			}
		})
	}
}

func TestGetJobLabels(t *testing.T) {
	autoIndexer := createTestAutoIndexer("test", "default", nil)

	labels := getJobLabels(autoIndexer)

	// Check required labels are present
	if labels[LabelAutoIndexerName] != autoIndexer.Name {
		t.Errorf("Expected label %s=%s, got %s", LabelAutoIndexerName, autoIndexer.Name, labels[LabelAutoIndexerName])
	}

	if labels[LabelAutoIndexerNamespace] != autoIndexer.Namespace {
		t.Errorf("Expected label %s=%s, got %s", LabelAutoIndexerNamespace, autoIndexer.Namespace, labels[LabelAutoIndexerNamespace])
	}

	if labels[LabelDataSourceType] != string(autoIndexer.Spec.DataSource.Type) {
		t.Errorf("Expected label %s=%s, got %s", LabelDataSourceType, autoIndexer.Spec.DataSource.Type, labels[LabelDataSourceType])
	}
}

func TestGetResourceRequirements(t *testing.T) {
	tests := []struct {
		name   string
		limits *corev1.ResourceRequirements
	}{
		{
			name:   "nil limits - should use defaults",
			limits: nil,
		},
		{
			name: "custom limits provided - should return as-is",
			limits: &corev1.ResourceRequirements{
				Limits: corev1.ResourceList{
					corev1.ResourceCPU:    mustParseQuantity("1000m"),
					corev1.ResourceMemory: mustParseQuantity("512Mi"),
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getResourceRequirements(tt.limits)

			if tt.limits == nil {
				// Should return defaults
				expectedCPU := mustParseQuantity("100m")
				expectedMemory := mustParseQuantity("256Mi")

				if !result.Requests.Cpu().Equal(expectedCPU) {
					t.Errorf("Expected CPU request %v, got %v", expectedCPU, result.Requests.Cpu())
				}

				if !result.Requests.Memory().Equal(expectedMemory) {
					t.Errorf("Expected memory request %v, got %v", expectedMemory, result.Requests.Memory())
				}
			} else {
				// Should return the provided limits as-is
				if tt.limits.Limits != nil {
					if !result.Limits.Cpu().Equal(tt.limits.Limits[corev1.ResourceCPU]) {
						t.Errorf("Expected CPU limit %v, got %v", tt.limits.Limits[corev1.ResourceCPU], result.Limits.Cpu())
					}
				}
			}
		})
	}
}

func TestGenerateSpecHash(t *testing.T) {
	autoIndexer := createTestAutoIndexer("test", "default", nil)

	hash1 := generateSpecHash(autoIndexer.Spec)
	hash2 := generateSpecHash(autoIndexer.Spec)

	// Same spec should generate same hash
	if hash1 != hash2 {
		t.Error("Same spec should generate same hash")
	}

	// Different spec should generate different hash
	autoIndexer.Spec.IndexName = "different-index"
	hash3 := generateSpecHash(autoIndexer.Spec)
	if hash1 == hash3 {
		t.Error("Different spec should generate different hash")
	}

	// Hash should be reasonable length (truncated to 8 chars)
	if len(hash1) != 8 {
		t.Errorf("Expected hash length 8, got %d", len(hash1))
	}
}

func TestGenerateServiceAccountName(t *testing.T) {
	autoIndexer := createTestAutoIndexer("test-ai", "default", nil)

	name := GenerateServiceAccountName(autoIndexer)

	expectedName := "test-ai-job-sa"
	if name != expectedName {
		t.Errorf("Expected name %s, got %s", expectedName, name)
	}
}

func TestGenerateRoleName(t *testing.T) {
	autoIndexer := createTestAutoIndexer("test-ai", "default", nil)

	name := GenerateRoleName(autoIndexer)

	expectedName := "test-ai-job-access"
	if name != expectedName {
		t.Errorf("Expected name %s, got %s", expectedName, name)
	}
}

func TestGenerateRoleBindingName(t *testing.T) {
	autoIndexer := createTestAutoIndexer("test-ai", "default", nil)

	name := GenerateRoleBindingName(autoIndexer)

	expectedName := "test-ai-job-access-binding"
	if name != expectedName {
		t.Errorf("Expected name %s, got %s", expectedName, name)
	}
}

func TestGenerateServiceAccountManifest(t *testing.T) {
	autoIndexer := createTestAutoIndexer("test", "default", nil)

	sa := GenerateServiceAccountManifest(autoIndexer)

	if sa.Name == "" {
		t.Error("ServiceAccount name should not be empty")
	}

	if sa.Namespace != autoIndexer.Namespace {
		t.Errorf("Expected namespace %s, got %s", autoIndexer.Namespace, sa.Namespace)
	}

	// Check labels
	if sa.Labels[LabelAutoIndexerName] != autoIndexer.Name {
		t.Errorf("Expected label %s=%s", LabelAutoIndexerName, autoIndexer.Name)
	}
}

func TestGenerateRoleManifest(t *testing.T) {
	autoIndexer := createTestAutoIndexer("test", "default", nil)

	role := GenerateRoleManifest(autoIndexer)

	if role.Name == "" {
		t.Error("Role name should not be empty")
	}

	if role.Namespace != autoIndexer.Namespace {
		t.Errorf("Expected namespace %s, got %s", autoIndexer.Namespace, role.Namespace)
	}

	// Check that role has some rules
	if len(role.Rules) == 0 {
		t.Error("Role should have at least one rule")
	}

	// Check labels
	if role.Labels[LabelAutoIndexerName] != autoIndexer.Name {
		t.Errorf("Expected label %s=%s", LabelAutoIndexerName, autoIndexer.Name)
	}
}

func TestAddCredentialsMounts(t *testing.T) {
	autoIndexer := createTestAutoIndexer("test", "default", nil)
	config := GetDefaultJobConfig(autoIndexer, JobTypeOneTime)
	job := GenerateIndexingJobManifest(config)

	// Count volumes and volume mounts before
	volumesBefore := len(job.Spec.Template.Spec.Volumes)
	mountsBefore := len(job.Spec.Template.Spec.Containers[0].VolumeMounts)

	// Add credentials
	addCredentialsMounts(job, autoIndexer.Spec.Credentials)

	// Count volumes and volume mounts after
	volumesAfter := len(job.Spec.Template.Spec.Volumes)
	mountsAfter := len(job.Spec.Template.Spec.Containers[0].VolumeMounts)

	// Should have added volume and mount
	if volumesAfter <= volumesBefore {
		t.Error("Should have added a volume for credentials")
	}

	if mountsAfter <= mountsBefore {
		t.Error("Should have added a volume mount for credentials")
	}
}

// Helper function for resource quantities
func mustParseQuantity(str string) resource.Quantity {
	q, err := resource.ParseQuantity(str)
	if err != nil {
		panic(err)
	}
	return q
}
