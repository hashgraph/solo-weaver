// SPDX-License-Identifier: Apache-2.0

package consensus

import (
	"context"
	"encoding/json"
	"time"

	cn "github.com/hashgraph/solo-weaver/internal/consensus"
	"github.com/joomcode/errorx"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
)

// setExecuteCondition upserts a status condition on the NetworkUpgradeExecute CR.
// The operator reads these conditions to drive phase transitions (it owns every
// phase); the daemon never patches status.phase.
//
// Idempotent: it reads the current conditions, replaces only the entry of the
// given type, and preserves lastTransitionTime when the status is unchanged, so
// re-running is a no-op. Only status.conditions is patched (merge patch), leaving
// the operator-owned status.phase untouched — and conditions have a single writer
// (the daemon), so the read-modify-patch has no competing writer.
func setExecuteCondition(ctx context.Context, client dynamic.Interface, namespace, crName string,
	condType cn.ConditionType, status cn.ConditionStatus, reason cn.ConditionReason, message string) error {
	resource := client.Resource(networkUpgradeExecuteGVR).Namespace(namespace)

	getCtx, cancel := context.WithTimeout(ctx, listTimeout)
	cur, err := resource.Get(getCtx, crName, metav1.GetOptions{})
	cancel()
	if err != nil {
		return errorx.ExternalError.Wrap(err, "get %s to set condition %s", crName, condType)
	}

	conditions, _, _ := unstructured.NestedSlice(cur.Object, "status", "conditions")
	generation, _, _ := unstructured.NestedInt64(cur.Object, "metadata", "generation")
	now := metav1.Now().UTC().Format(time.RFC3339)

	newCond := map[string]interface{}{
		"type":               string(condType),
		"status":             string(status),
		"reason":             string(reason),
		"message":            message,
		"lastTransitionTime": now,
		"observedGeneration": generation,
	}

	replaced := false
	for i, raw := range conditions {
		c, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		if t, _, _ := unstructured.NestedString(c, "type"); t != string(condType) {
			continue
		}
		// Preserve lastTransitionTime when the status has not changed, so a re-run
		// does not churn the timestamp.
		if s, _, _ := unstructured.NestedString(c, "status"); s == string(status) {
			if ltt, _, _ := unstructured.NestedString(c, "lastTransitionTime"); ltt != "" {
				newCond["lastTransitionTime"] = ltt
			}
		}
		conditions[i] = newCond
		replaced = true
		break
	}
	if !replaced {
		conditions = append(conditions, newCond)
	}

	patch, err := json.Marshal(map[string]interface{}{
		"status": map[string]interface{}{"conditions": conditions},
	})
	if err != nil {
		return errorx.InternalError.Wrap(err, "marshal condition patch for %s", crName)
	}

	patchCtx, cancel := context.WithTimeout(ctx, patchTimeout)
	defer cancel()
	if _, err := resource.Patch(patchCtx, crName, types.MergePatchType, patch, metav1.PatchOptions{}, "status"); err != nil {
		return errorx.ExternalError.Wrap(err, "patch %s condition %s", crName, condType)
	}
	return nil
}
