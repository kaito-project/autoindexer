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

package controllers

import (
	"context"

	"k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	autoindexerv1alpha1 "github.com/kaito-project/autoindexer/api/v1alpha1"
)

// setAutoIndexerCondition sets a condition on the AutoIndexer status
func (r *AutoIndexerReconciler) setAutoIndexerCondition(autoIndexerObj *autoindexerv1alpha1.AutoIndexer, conditionType autoindexerv1alpha1.ConditionType, status metav1.ConditionStatus, reason, message string) {
	condition := metav1.Condition{
		Type:               string(conditionType),
		Status:             status,
		Reason:             reason,
		Message:            message,
		LastTransitionTime: metav1.Now(),
		ObservedGeneration: autoIndexerObj.Generation,
	}

	// Find and update existing condition or append new one
	for i, existingCondition := range autoIndexerObj.Status.Conditions {
		if existingCondition.Type == condition.Type {
			// Only update if the status changed to avoid unnecessary updates
			if existingCondition.Status != condition.Status {
				autoIndexerObj.Status.Conditions[i] = condition
			}
			return
		}
	}

	// Add new condition
	autoIndexerObj.Status.Conditions = append(autoIndexerObj.Status.Conditions, condition)
}

func (r *AutoIndexerReconciler) updateAnnotations(ctx context.Context, autoIndexerObj, existingAutoIndexerObj *autoindexerv1alpha1.AutoIndexer) error {
	if !equality.Semantic.DeepEqual(existingAutoIndexerObj.Annotations, autoIndexerObj.Annotations) {
		// Spec changed, patch the whole object
		if patchErr := r.Patch(ctx, autoIndexerObj, client.MergeFrom(existingAutoIndexerObj)); patchErr != nil {
			return patchErr
		}
	}
	return nil
}

func (r *AutoIndexerReconciler) setSuspendedState(ctx context.Context, autoIndexerObj *autoindexerv1alpha1.AutoIndexer, suspend bool) error {
	existingAutoIndexerObj := autoIndexerObj.DeepCopy()
	autoIndexerObj.Spec.Suspend = &suspend

	if !equality.Semantic.DeepEqual(existingAutoIndexerObj.Spec, autoIndexerObj.Spec) {
		// Spec changed, patch the whole object
		if patchErr := r.Patch(ctx, autoIndexerObj, client.MergeFrom(existingAutoIndexerObj)); patchErr != nil {
			return patchErr
		}
	}
	return nil
}

func (r *AutoIndexerReconciler) patchAndReturn(ctx context.Context, obj, original *autoindexerv1alpha1.AutoIndexer, result ctrl.Result, err error) (ctrl.Result, error) {
	if !equality.Semantic.DeepEqual(original.Status, obj.Status) {
		// Only status changed, patch the status subresource
		if patchErr := r.Status().Patch(ctx, obj, client.MergeFrom(original)); patchErr != nil {
			return ctrl.Result{}, patchErr
		}
	}
	return result, err
}

// getAutoIndexerCondition gets a condition by type from the AutoIndexer status
func (r *AutoIndexerReconciler) getAutoIndexerCondition(autoIndexerObj *autoindexerv1alpha1.AutoIndexer, conditionType autoindexerv1alpha1.ConditionType) *metav1.Condition {
	for _, condition := range autoIndexerObj.Status.Conditions {
		if condition.Type == string(conditionType) {
			return &condition
		}
	}
	return nil
}

// recordAutoIndexerEvent records an event for the AutoIndexer
func (r *AutoIndexerReconciler) recordAutoIndexerEvent(autoIndexerObj *autoindexerv1alpha1.AutoIndexer, eventType, reason, message string) {
	if r.Recorder != nil {
		r.Recorder.Event(autoIndexerObj, eventType, reason, message)
	}
}
