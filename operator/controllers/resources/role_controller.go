/*
Copyright 2022 Gravitational, Inc.

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

package resources

import (
	"context"
	"fmt"

	"github.com/gravitational/teleport/api/types"
	resourcesv5 "github.com/gravitational/teleport/operator/apis/resources/v5"
	"github.com/gravitational/teleport/operator/sidecar"
	"github.com/gravitational/trace"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	ctrl "sigs.k8s.io/controller-runtime"
	kclient "sigs.k8s.io/controller-runtime/pkg/client"
)

const TeleportRoleKind = "TeleportRole"

var TeleportRoleGVK = schema.GroupVersionKind{
	Group:   resourcesv5.GroupVersion.Group,
	Version: resourcesv5.GroupVersion.Version,
	Kind:    TeleportRoleKind,
}

// RoleReconciler reconciles a TeleportRole object
type RoleReconciler struct {
	kclient.Client
	Scheme                 *runtime.Scheme
	TeleportClientAccessor sidecar.ClientAccessor
}

//+kubebuilder:rbac:groups=resources.teleport.dev,resources=roles,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=resources.teleport.dev,resources=roles/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=resources.teleport.dev,resources=roles/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the TeleportRole object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.11.0/pkg/reconcile
func (r *RoleReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	// The TeleportRole OpenAPI spec does not validate typing of Label fields like `node_labels`.
	// This means we can receive invalid data, by default it won't be unmarshalled properly and will crash the operator.
	// To handle this more gracefully we unmarshall first in an unstructured object.
	// The unstructured object will be converted later to a typed one, in r.UpsertExternal.
	// See `/operator/crdgen/schemagen.go` and https://github.com/gravitational/teleport/issues/15204 for context.
	obj := getUnstructuredObjectFromGVK(TeleportRoleGVK)
	return ResourceBaseReconciler{
		Client:         r.Client,
		DeleteExternal: r.Delete,
		UpsertExternal: r.Upsert,
	}.Do(ctx, req, obj)
}

// SetupWithManager sets up the controller with the Manager.
func (r *RoleReconciler) SetupWithManager(mgr ctrl.Manager) error {
	// The TeleportRole OpenAPI spec does not validate typing of Label fields like `node_labels`.
	// This means we can receive invalid data, by default it won't be unmarshalled properly and will crash the operator
	// To handle this more gracefully we unmarshall first in an unstructured object.
	// The unstructured object will be converted later to a typed one, in r.UpsertExternal.
	// See `/operator/crdgen/schemagen.go` and https://github.com/gravitational/teleport/issues/15204 for context
	obj := getUnstructuredObjectFromGVK(TeleportRoleGVK)
	return ctrl.NewControllerManagedBy(mgr).
		For(obj).
		Complete(r)
}

func (r *RoleReconciler) Delete(ctx context.Context, obj kclient.Object) error {
	teleportClient, err := r.TeleportClientAccessor(ctx)
	if err != nil {
		return trace.Wrap(err)
	}
	return teleportClient.DeleteRole(ctx, obj.GetName())
}

func (r *RoleReconciler) Upsert(ctx context.Context, obj kclient.Object) error {
	// We receive an unstructured object. We convert it to a typed TeleportRole object and gracefully handle errors.
	u, ok := obj.(*unstructured.Unstructured)
	if !ok {
		return fmt.Errorf("failed to convert Object into resource object: %T", obj)
	}
	k8sResource := &resourcesv5.TeleportRole{}

	// If an error happens we want to put it in status.conditions before returning.
	err := runtime.DefaultUnstructuredConverter.FromUnstructuredWithValidation(
		u.Object,
		k8sResource, true, /* returnUnknownFields */
	)
	newStructureCondition := getStructureConditionFromError(err)
	meta.SetStatusCondition(&k8sResource.Status.Conditions, newStructureCondition)
	if err != nil {
		// We update the status conditions on exit and aggregate the eventual error with the original one.
		return trace.NewAggregate(
			trace.WrapWithMessage(
				err,
				fmt.Sprintf("failed to convert unstructured Object into resource object: %T", k8sResource)),
			trace.Wrap(r.Status().Update(ctx, k8sResource)),
		)
	}

	// Converting the Kubernetes resource into a Teleport one, checking potential ownership issues.
	teleportResource := k8sResource.ToTeleport()
	teleportClient, err := r.TeleportClientAccessor(ctx)
	if err != nil {
		return trace.NewAggregate(
			trace.Wrap(err),
			trace.Wrap(r.Status().Update(ctx, k8sResource)),
		)
	}

	existingResource, err := teleportClient.GetRole(ctx, teleportResource.GetName())
	if err != nil && !trace.IsNotFound(err) {
		return trace.NewAggregate(
			trace.Wrap(err),
			trace.Wrap(r.Status().Update(ctx, k8sResource)),
		)
	}

	// If an error happens we want to put it in status.conditions before returning.
	newOwnershipCondition, err := checkOwnership(existingResource)
	meta.SetStatusCondition(&k8sResource.Status.Conditions, newOwnershipCondition)
	if err != nil {
		return trace.NewAggregate(
			trace.Wrap(err),
			trace.Wrap(r.Status().Update(ctx, k8sResource)),
		)
	}

	r.addTeleportResourceOrigin(teleportResource)

	// If an error happens we want to put it in status.conditions before returning.
	err = teleportClient.UpsertRole(ctx, teleportResource)
	newReconciliationCondition := getReconciliationConditionFromError(err)
	meta.SetStatusCondition(&k8sResource.Status.Conditions, newReconciliationCondition)
	return trace.NewAggregate(
		trace.Wrap(err),
		trace.Wrap(r.Status().Update(ctx, k8sResource)),
	)
}

func (r *RoleReconciler) addTeleportResourceOrigin(resource types.Role) {
	metadata := resource.GetMetadata()
	if metadata.Labels == nil {
		metadata.Labels = make(map[string]string)
	}
	metadata.Labels[types.OriginLabel] = types.OriginKubernetes
	resource.SetMetadata(metadata)
}

func getUnstructuredObjectFromGVK(gvk schema.GroupVersionKind) *unstructured.Unstructured {
	obj := unstructured.Unstructured{}
	obj.SetGroupVersionKind(gvk)
	return &obj
}
