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

package driftdetection

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestDefaultDriftDetectionConfig(t *testing.T) {
	config := DefaultDriftDetectionConfig()

	assert.Equal(t, 5*time.Minute, config.CheckInterval)
	assert.True(t, config.Enabled)
	assert.Equal(t, 3, config.MaxRetries)
	assert.Equal(t, 30*time.Second, config.RequestTimeout)
}

func TestDriftDetectionResult(t *testing.T) {
	result := DriftDetectionResult{
		AutoIndexerName:      "test-autoindexer",
		AutoIndexerNamespace: "test-namespace",
		ExpectedCount:        10,
		ActualCount:          5,
		DriftDetected:        true,
		Action:               DriftActionTriggerJob,
		Error:                nil,
	}

	assert.Equal(t, "test-autoindexer", result.AutoIndexerName)
	assert.Equal(t, "test-namespace", result.AutoIndexerNamespace)
	assert.Equal(t, int32(10), result.ExpectedCount)
	assert.Equal(t, int32(5), result.ActualCount)
	assert.True(t, result.DriftDetected)
	assert.Equal(t, DriftActionTriggerJob, result.Action)
	assert.Nil(t, result.Error)
}

func TestDriftActionConstants(t *testing.T) {
	assert.Equal(t, DriftAction("TriggerJob"), DriftActionTriggerJob)
	assert.Equal(t, DriftAction("None"), DriftActionNone)
}
