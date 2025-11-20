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

// ConditionType is a valid value for Condition.Type.
type ConditionType string

const (
	// ConditionTypeResourceStatus is the state when the AutoIndexer has been created.
	ConditionTypeResourceStatus = ConditionType("ResourceReady")

	// AutoIndexerConditionTypeSucceeded is the state when AutoIndexer has succeeded.
	AutoIndexerConditionTypeSucceeded ConditionType = ConditionType("AutoIndexerSucceeded")

	// AutoIndexerConditionTypeScheduled is the state when AutoIndexer has been scheduled.
	AutoIndexerConditionTypeScheduled ConditionType = ConditionType("AutoIndexerScheduled")

	// AutoIndexerConditionTypeIndexing is the state when AutoIndexer is indexing.
	AutoIndexerConditionTypeIndexing ConditionType = ConditionType("AutoIndexerIndexing")

	// AutoIndexerConditionTypeError is the state when AutoIndexer has failed.
	AutoIndexerConditionTypeError ConditionType = ConditionType("AutoIndexerError")

	// AutoIndexerConditionTypeDriftDetected is the state when drift has been detected.
	AutoIndexerConditionTypeDriftDetected ConditionType = ConditionType("DriftDetected")

	// AutoIndexerConditionTypeDriftRemediation is the state when drift has been remediated.
	AutoIndexerConditionTypeDriftRemediation ConditionType = ConditionType("DriftRemediation")
)
