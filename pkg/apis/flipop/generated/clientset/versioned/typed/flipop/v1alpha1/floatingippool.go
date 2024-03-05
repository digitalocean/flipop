// SPDX-License-Identifier: Apache-2.0
//
// Copyright 2024 Digital Ocean, Inc.
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

package v1alpha1

import (
	"context"
	"time"

	scheme "github.com/digitalocean/flipop/pkg/apis/flipop/generated/clientset/versioned/scheme"
	v1alpha1 "github.com/digitalocean/flipop/pkg/apis/flipop/v1alpha1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	types "k8s.io/apimachinery/pkg/types"
	watch "k8s.io/apimachinery/pkg/watch"
	rest "k8s.io/client-go/rest"
)

// FloatingIPPoolsGetter has a method to return a FloatingIPPoolInterface.
// A group's client should implement this interface.
type FloatingIPPoolsGetter interface {
	FloatingIPPools(namespace string) FloatingIPPoolInterface
}

// FloatingIPPoolInterface has methods to work with FloatingIPPool resources.
type FloatingIPPoolInterface interface {
	Create(ctx context.Context, floatingIPPool *v1alpha1.FloatingIPPool, opts v1.CreateOptions) (*v1alpha1.FloatingIPPool, error)
	Update(ctx context.Context, floatingIPPool *v1alpha1.FloatingIPPool, opts v1.UpdateOptions) (*v1alpha1.FloatingIPPool, error)
	UpdateStatus(ctx context.Context, floatingIPPool *v1alpha1.FloatingIPPool, opts v1.UpdateOptions) (*v1alpha1.FloatingIPPool, error)
	Delete(ctx context.Context, name string, opts v1.DeleteOptions) error
	DeleteCollection(ctx context.Context, opts v1.DeleteOptions, listOpts v1.ListOptions) error
	Get(ctx context.Context, name string, opts v1.GetOptions) (*v1alpha1.FloatingIPPool, error)
	List(ctx context.Context, opts v1.ListOptions) (*v1alpha1.FloatingIPPoolList, error)
	Watch(ctx context.Context, opts v1.ListOptions) (watch.Interface, error)
	Patch(ctx context.Context, name string, pt types.PatchType, data []byte, opts v1.PatchOptions, subresources ...string) (result *v1alpha1.FloatingIPPool, err error)
	FloatingIPPoolExpansion
}

// floatingIPPools implements FloatingIPPoolInterface
type floatingIPPools struct {
	client rest.Interface
	ns     string
}

// newFloatingIPPools returns a FloatingIPPools
func newFloatingIPPools(c *FlipopV1alpha1Client, namespace string) *floatingIPPools {
	return &floatingIPPools{
		client: c.RESTClient(),
		ns:     namespace,
	}
}

// Get takes name of the floatingIPPool, and returns the corresponding floatingIPPool object, and an error if there is any.
func (c *floatingIPPools) Get(ctx context.Context, name string, options v1.GetOptions) (result *v1alpha1.FloatingIPPool, err error) {
	result = &v1alpha1.FloatingIPPool{}
	err = c.client.Get().
		Namespace(c.ns).
		Resource("floatingippools").
		Name(name).
		VersionedParams(&options, scheme.ParameterCodec).
		Do(ctx).
		Into(result)
	return
}

// List takes label and field selectors, and returns the list of FloatingIPPools that match those selectors.
func (c *floatingIPPools) List(ctx context.Context, opts v1.ListOptions) (result *v1alpha1.FloatingIPPoolList, err error) {
	var timeout time.Duration
	if opts.TimeoutSeconds != nil {
		timeout = time.Duration(*opts.TimeoutSeconds) * time.Second
	}
	result = &v1alpha1.FloatingIPPoolList{}
	err = c.client.Get().
		Namespace(c.ns).
		Resource("floatingippools").
		VersionedParams(&opts, scheme.ParameterCodec).
		Timeout(timeout).
		Do(ctx).
		Into(result)
	return
}

// Watch returns a watch.Interface that watches the requested floatingIPPools.
func (c *floatingIPPools) Watch(ctx context.Context, opts v1.ListOptions) (watch.Interface, error) {
	var timeout time.Duration
	if opts.TimeoutSeconds != nil {
		timeout = time.Duration(*opts.TimeoutSeconds) * time.Second
	}
	opts.Watch = true
	return c.client.Get().
		Namespace(c.ns).
		Resource("floatingippools").
		VersionedParams(&opts, scheme.ParameterCodec).
		Timeout(timeout).
		Watch(ctx)
}

// Create takes the representation of a floatingIPPool and creates it.  Returns the server's representation of the floatingIPPool, and an error, if there is any.
func (c *floatingIPPools) Create(ctx context.Context, floatingIPPool *v1alpha1.FloatingIPPool, opts v1.CreateOptions) (result *v1alpha1.FloatingIPPool, err error) {
	result = &v1alpha1.FloatingIPPool{}
	err = c.client.Post().
		Namespace(c.ns).
		Resource("floatingippools").
		VersionedParams(&opts, scheme.ParameterCodec).
		Body(floatingIPPool).
		Do(ctx).
		Into(result)
	return
}

// Update takes the representation of a floatingIPPool and updates it. Returns the server's representation of the floatingIPPool, and an error, if there is any.
func (c *floatingIPPools) Update(ctx context.Context, floatingIPPool *v1alpha1.FloatingIPPool, opts v1.UpdateOptions) (result *v1alpha1.FloatingIPPool, err error) {
	result = &v1alpha1.FloatingIPPool{}
	err = c.client.Put().
		Namespace(c.ns).
		Resource("floatingippools").
		Name(floatingIPPool.Name).
		VersionedParams(&opts, scheme.ParameterCodec).
		Body(floatingIPPool).
		Do(ctx).
		Into(result)
	return
}

// UpdateStatus was generated because the type contains a Status member.
// Add a +genclient:noStatus comment above the type to avoid generating UpdateStatus().
func (c *floatingIPPools) UpdateStatus(ctx context.Context, floatingIPPool *v1alpha1.FloatingIPPool, opts v1.UpdateOptions) (result *v1alpha1.FloatingIPPool, err error) {
	result = &v1alpha1.FloatingIPPool{}
	err = c.client.Put().
		Namespace(c.ns).
		Resource("floatingippools").
		Name(floatingIPPool.Name).
		SubResource("status").
		VersionedParams(&opts, scheme.ParameterCodec).
		Body(floatingIPPool).
		Do(ctx).
		Into(result)
	return
}

// Delete takes name of the floatingIPPool and deletes it. Returns an error if one occurs.
func (c *floatingIPPools) Delete(ctx context.Context, name string, opts v1.DeleteOptions) error {
	return c.client.Delete().
		Namespace(c.ns).
		Resource("floatingippools").
		Name(name).
		Body(&opts).
		Do(ctx).
		Error()
}

// DeleteCollection deletes a collection of objects.
func (c *floatingIPPools) DeleteCollection(ctx context.Context, opts v1.DeleteOptions, listOpts v1.ListOptions) error {
	var timeout time.Duration
	if listOpts.TimeoutSeconds != nil {
		timeout = time.Duration(*listOpts.TimeoutSeconds) * time.Second
	}
	return c.client.Delete().
		Namespace(c.ns).
		Resource("floatingippools").
		VersionedParams(&listOpts, scheme.ParameterCodec).
		Timeout(timeout).
		Body(&opts).
		Do(ctx).
		Error()
}

// Patch applies the patch and returns the patched floatingIPPool.
func (c *floatingIPPools) Patch(ctx context.Context, name string, pt types.PatchType, data []byte, opts v1.PatchOptions, subresources ...string) (result *v1alpha1.FloatingIPPool, err error) {
	result = &v1alpha1.FloatingIPPool{}
	err = c.client.Patch(pt).
		Namespace(c.ns).
		Resource("floatingippools").
		Name(name).
		SubResource(subresources...).
		VersionedParams(&opts, scheme.ParameterCodec).
		Body(data).
		Do(ctx).
		Into(result)
	return
}
