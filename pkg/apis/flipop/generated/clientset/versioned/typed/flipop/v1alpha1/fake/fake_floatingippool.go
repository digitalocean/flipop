/*
MIT License

Copyright (c) 2020 Digital Ocean, Inc.

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.

*/

// Code generated by client-gen. DO NOT EDIT.

package fake

import (
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
func (c *FakeFloatingIPPools) Get(name string, options v1.GetOptions) (result *v1alpha1.FloatingIPPool, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewGetAction(floatingippoolsResource, c.ns, name), &v1alpha1.FloatingIPPool{})

	if obj == nil {
		return nil, err
	}
	return obj.(*v1alpha1.FloatingIPPool), err
}

// List takes label and field selectors, and returns the list of FloatingIPPools that match those selectors.
func (c *FakeFloatingIPPools) List(opts v1.ListOptions) (result *v1alpha1.FloatingIPPoolList, err error) {
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
func (c *FakeFloatingIPPools) Watch(opts v1.ListOptions) (watch.Interface, error) {
	return c.Fake.
		InvokesWatch(testing.NewWatchAction(floatingippoolsResource, c.ns, opts))

}

// Create takes the representation of a floatingIPPool and creates it.  Returns the server's representation of the floatingIPPool, and an error, if there is any.
func (c *FakeFloatingIPPools) Create(floatingIPPool *v1alpha1.FloatingIPPool) (result *v1alpha1.FloatingIPPool, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewCreateAction(floatingippoolsResource, c.ns, floatingIPPool), &v1alpha1.FloatingIPPool{})

	if obj == nil {
		return nil, err
	}
	return obj.(*v1alpha1.FloatingIPPool), err
}

// Update takes the representation of a floatingIPPool and updates it. Returns the server's representation of the floatingIPPool, and an error, if there is any.
func (c *FakeFloatingIPPools) Update(floatingIPPool *v1alpha1.FloatingIPPool) (result *v1alpha1.FloatingIPPool, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewUpdateAction(floatingippoolsResource, c.ns, floatingIPPool), &v1alpha1.FloatingIPPool{})

	if obj == nil {
		return nil, err
	}
	return obj.(*v1alpha1.FloatingIPPool), err
}

// UpdateStatus was generated because the type contains a Status member.
// Add a +genclient:noStatus comment above the type to avoid generating UpdateStatus().
func (c *FakeFloatingIPPools) UpdateStatus(floatingIPPool *v1alpha1.FloatingIPPool) (*v1alpha1.FloatingIPPool, error) {
	obj, err := c.Fake.
		Invokes(testing.NewUpdateSubresourceAction(floatingippoolsResource, "status", c.ns, floatingIPPool), &v1alpha1.FloatingIPPool{})

	if obj == nil {
		return nil, err
	}
	return obj.(*v1alpha1.FloatingIPPool), err
}

// Delete takes name of the floatingIPPool and deletes it. Returns an error if one occurs.
func (c *FakeFloatingIPPools) Delete(name string, options *v1.DeleteOptions) error {
	_, err := c.Fake.
		Invokes(testing.NewDeleteAction(floatingippoolsResource, c.ns, name), &v1alpha1.FloatingIPPool{})

	return err
}

// DeleteCollection deletes a collection of objects.
func (c *FakeFloatingIPPools) DeleteCollection(options *v1.DeleteOptions, listOptions v1.ListOptions) error {
	action := testing.NewDeleteCollectionAction(floatingippoolsResource, c.ns, listOptions)

	_, err := c.Fake.Invokes(action, &v1alpha1.FloatingIPPoolList{})
	return err
}

// Patch applies the patch and returns the patched floatingIPPool.
func (c *FakeFloatingIPPools) Patch(name string, pt types.PatchType, data []byte, subresources ...string) (result *v1alpha1.FloatingIPPool, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewPatchSubresourceAction(floatingippoolsResource, c.ns, name, pt, data, subresources...), &v1alpha1.FloatingIPPool{})

	if obj == nil {
		return nil, err
	}
	return obj.(*v1alpha1.FloatingIPPool), err
}