// SPDX-License-Identifier: Apache-2.0

package consensus

import (
	"context"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

// SoakCriterion is the interface every soak criterion must implement.
type SoakCriterion interface {
	// Name returns the unique criterion identifier used in CriterionMet events.
	Name() string
	// Check evaluates the criterion. Returns (true, nil) when the criterion is
	// green. On error, the caller logs a warning and treats the criterion as
	// not-green — a flaky check never triggers decommission.
	Check(ctx context.Context, req SoakStartRequest) (bool, error)
}

// RestartCounter is an optional interface that a SoakCriterion may implement
// to expose the total container restart count observed during the last Check call.
// The MigrationMonitor checks for this interface when building the SoakCheck payload.
type RestartCounter interface {
	TotalRestarts() int
}

// DefaultSoakPeriod is the HIP-specified minimum soak duration.
const DefaultSoakPeriod = 48 * time.Hour

// SoakDuration is green when at least Period has elapsed since the cutover
// timestamp in the activation request. Zero value of Period defaults to
// DefaultSoakPeriod (48h) per HIP spec.
type SoakDuration struct {
	// Period is the minimum time that must elapse since cutover before this
	// criterion is considered met. Defaults to 48h when zero.
	Period time.Duration
}

func (c SoakDuration) Name() string { return "SoakDuration" }

func (c SoakDuration) Check(_ context.Context, req SoakStartRequest) (bool, error) {
	period := c.Period
	if period <= 0 {
		period = DefaultSoakPeriod
	}
	return time.Since(req.CutoverTimestamp) >= period, nil
}

// UploaderBacklogCleared is green when the record stream uploader has no
// backlogged events from the old CN pending upload to mirror nodes.
// Stub — real implementation in a subsequent story.
type UploaderBacklogCleared struct{}

func (UploaderBacklogCleared) Name() string { return "UploaderBacklogCleared" }

func (UploaderBacklogCleared) Check(_ context.Context, _ SoakStartRequest) (bool, error) {
	// TODO: query record stream uploader for backlog status
	return false, nil
}

// NoPodRestarts is green when the CN pod — identified by PodLabelSelector in
// Namespace — was started after the cutover timestamp and has accumulated zero
// container restarts since then, indicating stable operation.
//
// Logic:
//   - List pods matching PodLabelSelector in Namespace.
//   - Consider only pods whose creation timestamp is after req.CutoverTimestamp.
//   - If at least one such pod exists and all of its containers have RestartCount == 0: green.
//   - If no post-cutover pod is found yet (still starting up): not-green (not an error).
//   - If any post-cutover pod has at least one container restart: not-green (not an error).
//
// TotalRestarts returns the sum of all container restart counts across post-cutover
// pods from the last Check call. Use it to populate diagnostic fields in SoakCheck.
type NoPodRestarts struct {
	// KubeconfigPath is the path to the daemon kubeconfig used to contact the K8s API.
	KubeconfigPath string

	// Namespace is the orbit namespace where the CN pod runs.
	Namespace string

	// PodLabelSelector is the label selector used to identify the CN pod managed by
	// this daemon instance. The selector must match exactly the pods that belong to
	// this node's ConsensusCapsule StatefulSet.
	// Example: "operator.solo.hedera.com/orbit=mainnet-00,operator.solo.hedera.com/node-id=0.0.3"
	PodLabelSelector string

	// client is an optional pre-built Kubernetes client. When set (test injection),
	// KubeconfigPath is ignored. Production code leaves this nil.
	client kubernetes.Interface

	// lastRestartCount is the total container restart count from the most recent Check call.
	lastRestartCount int
}

func (c *NoPodRestarts) Name() string { return "NoPodRestarts" }

// TotalRestarts returns the sum of all container restart counts across post-cutover
// pods observed during the last Check call. Zero when no post-cutover pods exist yet.
func (c *NoPodRestarts) TotalRestarts() int { return c.lastRestartCount }

func (c *NoPodRestarts) Check(ctx context.Context, req SoakStartRequest) (bool, error) {
	client := c.client
	if client == nil {
		var err error
		client, err = buildTypedClient(c.KubeconfigPath)
		if err != nil {
			return false, ErrSoakWatcher.Wrap(err, "NoPodRestarts: build k8s client")
		}
	}

	pods, err := client.CoreV1().Pods(c.Namespace).List(ctx, metav1.ListOptions{
		LabelSelector: c.PodLabelSelector,
	})
	if err != nil {
		return false, ErrSoakWatcher.Wrap(err, "NoPodRestarts: list pods")
	}

	// Filter to pods started after the cutover timestamp.
	var postCutoverPods []corev1.Pod
	for _, pod := range pods.Items {
		if pod.CreationTimestamp.After(req.CutoverTimestamp) {
			postCutoverPods = append(postCutoverPods, pod)
		}
	}

	if len(postCutoverPods) == 0 {
		// CN pod not yet started post-cutover — not an error, just not green yet.
		c.lastRestartCount = 0
		return false, nil
	}

	// Tally restarts across all post-cutover pods and containers.
	// Container restarts are an expected non-green state (pod is unstable but
	// the check itself succeeded). Return (false, nil) — not an error.
	total := 0
	for _, pod := range postCutoverPods {
		for _, cs := range pod.Status.ContainerStatuses {
			total += int(cs.RestartCount)
		}
	}
	c.lastRestartCount = total
	return total == 0, nil
}

// buildTypedClient builds a typed Kubernetes client from the kubeconfig at path.
func buildTypedClient(kubeconfigPath string) (kubernetes.Interface, error) {
	restCfg, err := clientcmd.BuildConfigFromFlags("", kubeconfigPath)
	if err != nil {
		return nil, ErrSoakWatcher.Wrap(err, "load kubeconfig %s", kubeconfigPath)
	}
	restCfg.Timeout = 30 * time.Second
	client, err := kubernetes.NewForConfig(restCfg)
	if err != nil {
		return nil, ErrSoakWatcher.Wrap(err, "build typed k8s client")
	}
	return client, nil
}

// ConsensusParticipationNominal is green when the CN is actively participating
// in consensus — signing and submitting transactions within acceptable bounds.
// Stub — real implementation in a subsequent story.
type ConsensusParticipationNominal struct{}

func (ConsensusParticipationNominal) Name() string { return "ConsensusParticipationNominal" }

func (ConsensusParticipationNominal) Check(_ context.Context, _ SoakStartRequest) (bool, error) {
	// TODO: check consensus round participation rate via metrics or REST API
	return false, nil
}
