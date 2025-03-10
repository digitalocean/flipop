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

// FakeNodeDNSRecordSets implements NodeDNSRecordSetInterface
type FakeNodeDNSRecordSets struct {
	Fake *FakeFlipopV1alpha1
	ns   string
}

var nodednsrecordsetsResource = schema.GroupVersionResource{Group: "flipop.digitalocean.com", Version: "v1alpha1", Resource: "nodednsrecordsets"}

var nodednsrecordsetsKind = schema.GroupVersionKind{Group: "flipop.digitalocean.com", Version: "v1alpha1", Kind: "NodeDNSRecordSet"}

// Get takes name of the nodeDNSRecordSet, and returns the corresponding nodeDNSRecordSet object, and an error if there is any.
func (c *FakeNodeDNSRecordSets) Get(ctx context.Context, name string, options v1.GetOptions) (result *v1alpha1.NodeDNSRecordSet, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewGetAction(nodednsrecordsetsResource, c.ns, name), &v1alpha1.NodeDNSRecordSet{})

	if obj == nil {
		return nil, err
	}
	return obj.(*v1alpha1.NodeDNSRecordSet), err
}

// List takes label and field selectors, and returns the list of NodeDNSRecordSets that match those selectors.
func (c *FakeNodeDNSRecordSets) List(ctx context.Context, opts v1.ListOptions) (result *v1alpha1.NodeDNSRecordSetList, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewListAction(nodednsrecordsetsResource, nodednsrecordsetsKind, c.ns, opts), &v1alpha1.NodeDNSRecordSetList{})

	if obj == nil {
		return nil, err
	}

	label, _, _ := testing.ExtractFromListOptions(opts)
	if label == nil {
		label = labels.Everything()
	}
	list := &v1alpha1.NodeDNSRecordSetList{ListMeta: obj.(*v1alpha1.NodeDNSRecordSetList).ListMeta}
	for _, item := range obj.(*v1alpha1.NodeDNSRecordSetList).Items {
		if label.Matches(labels.Set(item.Labels)) {
			list.Items = append(list.Items, item)
		}
	}
	return list, err
}

// Watch returns a watch.Interface that watches the requested nodeDNSRecordSets.
func (c *FakeNodeDNSRecordSets) Watch(ctx context.Context, opts v1.ListOptions) (watch.Interface, error) {
	return c.Fake.
		InvokesWatch(testing.NewWatchAction(nodednsrecordsetsResource, c.ns, opts))

}

// Create takes the representation of a nodeDNSRecordSet and creates it.  Returns the server's representation of the nodeDNSRecordSet, and an error, if there is any.
func (c *FakeNodeDNSRecordSets) Create(ctx context.Context, nodeDNSRecordSet *v1alpha1.NodeDNSRecordSet, opts v1.CreateOptions) (result *v1alpha1.NodeDNSRecordSet, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewCreateAction(nodednsrecordsetsResource, c.ns, nodeDNSRecordSet), &v1alpha1.NodeDNSRecordSet{})

	if obj == nil {
		return nil, err
	}
	return obj.(*v1alpha1.NodeDNSRecordSet), err
}

// Update takes the representation of a nodeDNSRecordSet and updates it. Returns the server's representation of the nodeDNSRecordSet, and an error, if there is any.
func (c *FakeNodeDNSRecordSets) Update(ctx context.Context, nodeDNSRecordSet *v1alpha1.NodeDNSRecordSet, opts v1.UpdateOptions) (result *v1alpha1.NodeDNSRecordSet, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewUpdateAction(nodednsrecordsetsResource, c.ns, nodeDNSRecordSet), &v1alpha1.NodeDNSRecordSet{})

	if obj == nil {
		return nil, err
	}
	return obj.(*v1alpha1.NodeDNSRecordSet), err
}

// UpdateStatus was generated because the type contains a Status member.
// Add a +genclient:noStatus comment above the type to avoid generating UpdateStatus().
func (c *FakeNodeDNSRecordSets) UpdateStatus(ctx context.Context, nodeDNSRecordSet *v1alpha1.NodeDNSRecordSet, opts v1.UpdateOptions) (*v1alpha1.NodeDNSRecordSet, error) {
	obj, err := c.Fake.
		Invokes(testing.NewUpdateSubresourceAction(nodednsrecordsetsResource, "status", c.ns, nodeDNSRecordSet), &v1alpha1.NodeDNSRecordSet{})

	if obj == nil {
		return nil, err
	}
	return obj.(*v1alpha1.NodeDNSRecordSet), err
}

// Delete takes name of the nodeDNSRecordSet and deletes it. Returns an error if one occurs.
func (c *FakeNodeDNSRecordSets) Delete(ctx context.Context, name string, opts v1.DeleteOptions) error {
	_, err := c.Fake.
		Invokes(testing.NewDeleteAction(nodednsrecordsetsResource, c.ns, name), &v1alpha1.NodeDNSRecordSet{})

	return err
}

// DeleteCollection deletes a collection of objects.
func (c *FakeNodeDNSRecordSets) DeleteCollection(ctx context.Context, opts v1.DeleteOptions, listOpts v1.ListOptions) error {
	action := testing.NewDeleteCollectionAction(nodednsrecordsetsResource, c.ns, listOpts)

	_, err := c.Fake.Invokes(action, &v1alpha1.NodeDNSRecordSetList{})
	return err
}

// Patch applies the patch and returns the patched nodeDNSRecordSet.
func (c *FakeNodeDNSRecordSets) Patch(ctx context.Context, name string, pt types.PatchType, data []byte, opts v1.PatchOptions, subresources ...string) (result *v1alpha1.NodeDNSRecordSet, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewPatchSubresourceAction(nodednsrecordsetsResource, c.ns, name, pt, data, subresources...), &v1alpha1.NodeDNSRecordSet{})

	if obj == nil {
		return nil, err
	}
	return obj.(*v1alpha1.NodeDNSRecordSet), err
}
