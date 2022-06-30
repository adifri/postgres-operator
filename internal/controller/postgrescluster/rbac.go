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
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"

	"github.com/crunchydata/postgres-operator/v5/internal/initialize"
	"github.com/crunchydata/postgres-operator/v5/internal/naming"
	"github.com/crunchydata/postgres-operator/v5/internal/patroni"
	"github.com/crunchydata/postgres-operator/v5/pkg/apis/postgres-operator.crunchydata.com/v1beta1"
)

// reconcileRBACResources creates Roles, RoleBindings, and ServiceAccounts for
// cluster. The returned instanceServiceAccount has all the authorization needed
// by an instance Pod.
func (r *Reconciler) reconcileRBACResources(
	ctx context.Context, cluster *v1beta1.PostgresCluster,
) (
	instanceServiceAccount *corev1.ServiceAccount, err error,
) {
	return r.reconcileInstanceRBAC(ctx, cluster)
}

// +kubebuilder:rbac:groups="",resources=serviceaccounts,verbs=create;patch
// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=roles,verbs=create;patch
// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=rolebindings,verbs=create;patch

// reconcileInstanceRBAC writes the Role, RoleBinding, and ServiceAccount for
// all instances of cluster.
func (r *Reconciler) reconcileInstanceRBAC(
	ctx context.Context, cluster *v1beta1.PostgresCluster,
) (*corev1.ServiceAccount, error) {
	account := &corev1.ServiceAccount{ObjectMeta: naming.ClusterInstanceRBAC(cluster)}
	account.SetGroupVersionKind(corev1.SchemeGroupVersion.WithKind("ServiceAccount"))

	binding := &rbacv1.RoleBinding{ObjectMeta: naming.ClusterInstanceRBAC(cluster)}
	binding.SetGroupVersionKind(rbacv1.SchemeGroupVersion.WithKind("RoleBinding"))

	role := &rbacv1.Role{ObjectMeta: naming.ClusterInstanceRBAC(cluster)}
	role.SetGroupVersionKind(rbacv1.SchemeGroupVersion.WithKind("Role"))

	err := errors.WithStack(r.setControllerReference(cluster, account))
	if err == nil {
		err = errors.WithStack(r.setControllerReference(cluster, binding))
	}
	if err == nil {
		err = errors.WithStack(r.setControllerReference(cluster, role))
	}

	account.Annotations = naming.Merge(cluster.Spec.Metadata.GetAnnotationsOrNil())
	account.Labels = naming.Merge(cluster.Spec.Metadata.GetLabelsOrNil(),
		map[string]string{
			naming.LabelCluster: cluster.Name,
		})
	binding.Annotations = naming.Merge(cluster.Spec.Metadata.GetAnnotationsOrNil())
	binding.Labels = naming.Merge(cluster.Spec.Metadata.GetLabelsOrNil(),
		map[string]string{
			naming.LabelCluster: cluster.Name,
		})
	role.Annotations = naming.Merge(cluster.Spec.Metadata.GetAnnotationsOrNil())
	role.Labels = naming.Merge(cluster.Spec.Metadata.GetLabelsOrNil(),
		map[string]string{
			naming.LabelCluster: cluster.Name,
		})

	account.AutomountServiceAccountToken = initialize.Bool(true)
	binding.RoleRef = rbacv1.RoleRef{
		APIGroup: rbacv1.SchemeGroupVersion.Group,
		Kind:     role.Kind,
		Name:     role.Name,
	}
	binding.Subjects = []rbacv1.Subject{{
		Kind: account.Kind,
		Name: account.Name,
	}}
	role.Rules = patroni.Permissions(cluster)

	if err == nil {
		err = errors.WithStack(r.apply(ctx, account))
	}
	if err == nil {
		err = errors.WithStack(r.apply(ctx, role))
	}
	if err == nil {
		err = errors.WithStack(r.apply(ctx, binding))
	}

	return account, err
}
