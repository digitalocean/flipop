// SPDX-License-Identifier: Apache-2.0
//
// Copyright 2025 Digital Ocean, Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//    http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Code generated by client-gen. DO NOT EDIT.

package fake

import (
	"context"

	v1alpha1 "github.com/digitalocean/flipop/pkg/apis/flipop/v1alpha1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	labels "k8s.io/apimachinery/pkg/labels"
	schema "k8s.io/apimachinery/pkg/runtime/schema"
	types "k8s.io/apimachinery/pkg/types"
	watch "k8s.io/apimachinery/pkg/watch"
	testing "k8s.io/client-go/testing"
)

// FakeFloatingIPPools implements FloatingIPPoolInterface
type FakeFloatingIPPools struct {
	Fake *FakeFlipopV1alpha1
	ns   string
}

var floatingippoolsResource = schema.GroupVersionResource{Group: "flipop.digitalocean.com", Version: "v1alpha1", Resource: "floatingippools"}

var floatingippoolsKind = schema.GroupVersionKind{Group: "flipop.digitalocean.com", Version: "v1alpha1", Kind: "FloatingIPPool"}

// Get takes name of the floatingIPPool, and returns the corresponding floatingIPPool object, and an error if there is any.
func (c *FakeFloatingIPPools) Get(ctx context.Context, name string, options v1.GetOptions) (result *v1alpha1.FloatingIPPool, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewGetAction(floatingippoolsResource, c.ns, name), &v1alpha1.FloatingIPPool{})

	if obj == nil {
		return nil, err
	}
	return obj.(*v1alpha1.FloatingIPPool), err
}

// List takes label and field selectors, and returns the list of FloatingIPPools that match those selectors.
func (c *FakeFloatingIPPools) List(ctx context.Context, opts v1.ListOptions) (result *v1alpha1.FloatingIPPoolList, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewListAction(floatingippoolsResource, floatingippoolsKind, c.ns, opts), &v1alpha1.FloatingIPPoolList{})

	if obj == nil {
		return nil, err
	}

	label, _, _ := testing.ExtractFromListOptions(opts)
	if label == nil {
		label = labels.Everything()
	}
	list := &v1alpha1.FloatingIPPoolList{ListMeta: obj.(*v1alpha1.FloatingIPPoolList).ListMeta}
	for _, item := range obj.(*v1alpha1.FloatingIPPoolList).Items {
		if label.Matches(labels.Set(item.Labels)) {
			list.Items = append(list.Items, item)
		}
	}
	return list, err
}

// Watch returns a watch.Interface that watches the requested floatingIPPools.
func (c *FakeFloatingIPPools) Watch(ctx context.Context, opts v1.ListOptions) (watch.Interface, error) {
	return c.Fake.
		InvokesWatch(testing.NewWatchAction(floatingippoolsResource, c.ns, opts))

}

// Create takes the representation of a floatingIPPool and creates it.  Returns the server's representation of the floatingIPPool, and an error, if there is any.
func (c *FakeFloatingIPPools) Create(ctx context.Context, floatingIPPool *v1alpha1.FloatingIPPool, opts v1.CreateOptions) (result *v1alpha1.FloatingIPPool, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewCreateAction(floatingippoolsResource, c.ns, floatingIPPool), &v1alpha1.FloatingIPPool{})

	if obj == nil {
		return nil, err
	}
	return obj.(*v1alpha1.FloatingIPPool), err
}

// Update takes the representation of a floatingIPPool and updates it. Returns the server's representation of the floatingIPPool, and an error, if there is any.
func (c *FakeFloatingIPPools) Update(ctx context.Context, floatingIPPool *v1alpha1.FloatingIPPool, opts v1.UpdateOptions) (result *v1alpha1.FloatingIPPool, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewUpdateAction(floatingippoolsResource, c.ns, floatingIPPool), &v1alpha1.FloatingIPPool{})

	if obj == nil {
		return nil, err
	}
	return obj.(*v1alpha1.FloatingIPPool), err
}

// UpdateStatus was generated because the type contains a Status member.
// Add a +genclient:noStatus comment above the type to avoid generating UpdateStatus().
func (c *FakeFloatingIPPools) UpdateStatus(ctx context.Context, floatingIPPool *v1alpha1.FloatingIPPool, opts v1.UpdateOptions) (*v1alpha1.FloatingIPPool, error) {
	obj, err := c.Fake.
		Invokes(testing.NewUpdateSubresourceAction(floatingippoolsResource, "status", c.ns, floatingIPPool), &v1alpha1.FloatingIPPool{})

	if obj == nil {
		return nil, err
	}
	return obj.(*v1alpha1.FloatingIPPool), err
}

// Delete takes name of the floatingIPPool and deletes it. Returns an error if one occurs.
func (c *FakeFloatingIPPools) Delete(ctx context.Context, name string, opts v1.DeleteOptions) error {
	_, err := c.Fake.
		Invokes(testing.NewDeleteAction(floatingippoolsResource, c.ns, name), &v1alpha1.FloatingIPPool{})

	return err
}

// DeleteCollection deletes a collection of objects.
func (c *FakeFloatingIPPools) DeleteCollection(ctx context.Context, opts v1.DeleteOptions, listOpts v1.ListOptions) error {
	action := testing.NewDeleteCollectionAction(floatingippoolsResource, c.ns, listOpts)

	_, err := c.Fake.Invokes(action, &v1alpha1.FloatingIPPoolList{})
	return err
}

// Patch applies the patch and returns the patched floatingIPPool.
func (c *FakeFloatingIPPools) Patch(ctx context.Context, name string, pt types.PatchType, data []byte, opts v1.PatchOptions, subresources ...string) (result *v1alpha1.FloatingIPPool, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewPatchSubresourceAction(floatingippoolsResource, c.ns, name, pt, data, subresources...), &v1alpha1.FloatingIPPool{})

	if obj == nil {
		return nil, err
	}
	return obj.(*v1alpha1.FloatingIPPool), err
}
