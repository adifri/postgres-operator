/*
 Copyright 2021 - 2022 Crunchy Data Solutions, Inc.
 Licensed under the Apache License, Version 2.0 (the "License");
 you may not use this file except in compliance with the License.
 You may obtain a copy of the License at

 http://www.apache.org/licenses/LICENSE-2.0

 Unless required by applicable law or agreed to in writing, software
 distributed under the License is distributed on an "AS IS" BASIS,
 WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 See the License for the specific language governing permissions and
 limitations under the License.
*/

package postgrescluster

import (
	"context"

	"github.com/pkg/errors"
	"k8s.io/apimachinery/pkg/util/sets"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/adifri/postgres-operator/v5/internal/naming"
	"github.com/adifri/postgres-operator/v5/pkg/apis/postgres-operator.crunchydata.com/v1beta1"
)

// +kubebuilder:rbac:groups=postgres-operator.crunchydata.com,resources=postgresclusters,verbs=patch

// handleDelete sets a finalizer on cluster and performs the finalization of
// cluster when it is being deleted. It returns (nil, nil) when cluster is
// not being deleted. The caller is responsible for returning other values to
// controller-runtime.
func (r *Reconciler) handleDelete(
	ctx context.Context, cluster *v1beta1.PostgresCluster,
) (*reconcile.Result, error) {
	finalizers := sets.NewString(cluster.Finalizers...)

	// An object with Finalizers does not go away when deleted in the Kubernetes
	// API. Instead, it is given a DeletionTimestamp so that controllers can
	// react before it goes away. The object will remain in this state until
	// its Finalizers list is empty. Controllers are expected to remove their
	// finalizer from this list when they have completed their work.
	// - https://docs.k8s.io/tasks/extend-kubernetes/custom-resources/custom-resource-definitions/#finalizers
	// - https://book.kubebuilder.io/reference/using-finalizers.html

	// TODO(cbandy): Foreground deletion also involves a finalizer. The garbage
	// collector deletes dependents *before* their owner.
	// - https://docs.k8s.io/concepts/workloads/controllers/garbage-collection/#foreground-cascading-deletion

	if cluster.DeletionTimestamp.IsZero() {
		if finalizers.Has(naming.Finalizer) {
			// The cluster is not being deleted and the finalizer is set.
			// The caller can do what they like.
			return nil, nil
		}

		// The cluster is not being deleted and needs a finalizer; set it.

		// The Finalizers field is shared by multiple controllers, but the
		// server-side merge strategy does not work on our custom resource due
		// to a bug in Kubernetes. Build a merge-patch that includes the full
		// list of Finalizers plus ResourceVersion to detect conflicts with
		// other potential writers.
		// - https://issue.k8s.io/99730
		before := cluster.DeepCopy()
		// Make another copy so that Patch doesn't write back to cluster.
		intent := before.DeepCopy()
		intent.Finalizers = append(intent.Finalizers, naming.Finalizer)
		err := errors.WithStack(r.patch(ctx, intent,
			client.MergeFromWithOptions(before, client.MergeFromWithOptimisticLock{})))

		// The caller can do what they like or requeue upon error.
		return nil, err
	}

	if !finalizers.Has(naming.Finalizer) {
		// The cluster is being deleted and there is no finalizer.
		// The caller should listen for another event.
		return &reconcile.Result{}, nil
	}

	// The cluster is being deleted and our finalizer is still set; run our
	// finalizer logic.

	if result, err := r.deleteInstances(ctx, cluster); err != nil {
		return nil, err
	} else if result != nil {
		return result, nil
	}

	// Instances are stopped, now cleanup some Patroni stuff.
	if err := r.deletePatroniArtifacts(ctx, cluster); err != nil {
		return nil, err
	}

	// Our finalizer logic is finished; remove our finalizer.
	// The Finalizers field is shared by multiple controllers, but the
	// server-side merge strategy does not work on our custom resource due to a
	// bug in Kubernetes. Build a merge-patch that includes the full list of
	// Finalizers plus ResourceVersion to detect conflicts with other potential
	// writers.
	// - https://issue.k8s.io/99730
	before := cluster.DeepCopy()
	// Make another copy so that Patch doesn't write back to cluster.
	intent := before.DeepCopy()
	intent.Finalizers = finalizers.Delete(naming.Finalizer).List()
	err := errors.WithStack(r.patch(ctx, intent,
		client.MergeFromWithOptions(before, client.MergeFromWithOptimisticLock{})))

	// The caller should wait for further events or requeue upon error.
	return &reconcile.Result{}, err
}
