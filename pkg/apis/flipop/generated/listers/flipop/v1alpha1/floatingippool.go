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

// Code generated by lister-gen. DO NOT EDIT.

package v1alpha1

import (
	v1alpha1 "github.com/digitalocean/flipop/pkg/apis/flipop/v1alpha1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/tools/cache"
)

// FloatingIPPoolLister helps list FloatingIPPools.
// All objects returned here must be treated as read-only.
type FloatingIPPoolLister interface {
	// List lists all FloatingIPPools in the indexer.
	// Objects returned here must be treated as read-only.
	List(selector labels.Selector) (ret []*v1alpha1.FloatingIPPool, err error)
	// FloatingIPPools returns an object that can list and get FloatingIPPools.
	FloatingIPPools(namespace string) FloatingIPPoolNamespaceLister
	FloatingIPPoolListerExpansion
}

// floatingIPPoolLister implements the FloatingIPPoolLister interface.
type floatingIPPoolLister struct {
	indexer cache.Indexer
}

// NewFloatingIPPoolLister returns a new FloatingIPPoolLister.
func NewFloatingIPPoolLister(indexer cache.Indexer) FloatingIPPoolLister {
	return &floatingIPPoolLister{indexer: indexer}
}

// List lists all FloatingIPPools in the indexer.
func (s *floatingIPPoolLister) List(selector labels.Selector) (ret []*v1alpha1.FloatingIPPool, err error) {
	err = cache.ListAll(s.indexer, selector, func(m interface{}) {
		ret = append(ret, m.(*v1alpha1.FloatingIPPool))
	})
	return ret, err
}

// FloatingIPPools returns an object that can list and get FloatingIPPools.
func (s *floatingIPPoolLister) FloatingIPPools(namespace string) FloatingIPPoolNamespaceLister {
	return floatingIPPoolNamespaceLister{indexer: s.indexer, namespace: namespace}
}

// FloatingIPPoolNamespaceLister helps list and get FloatingIPPools.
// All objects returned here must be treated as read-only.
type FloatingIPPoolNamespaceLister interface {
	// List lists all FloatingIPPools in the indexer for a given namespace.
	// Objects returned here must be treated as read-only.
	List(selector labels.Selector) (ret []*v1alpha1.FloatingIPPool, err error)
	// Get retrieves the FloatingIPPool from the indexer for a given namespace and name.
	// Objects returned here must be treated as read-only.
	Get(name string) (*v1alpha1.FloatingIPPool, error)
	FloatingIPPoolNamespaceListerExpansion
}

// floatingIPPoolNamespaceLister implements the FloatingIPPoolNamespaceLister
// interface.
type floatingIPPoolNamespaceLister struct {
	indexer   cache.Indexer
	namespace string
}

// List lists all FloatingIPPools in the indexer for a given namespace.
func (s floatingIPPoolNamespaceLister) List(selector labels.Selector) (ret []*v1alpha1.FloatingIPPool, err error) {
	err = cache.ListAllByNamespace(s.indexer, s.namespace, selector, func(m interface{}) {
		ret = append(ret, m.(*v1alpha1.FloatingIPPool))
	})
	return ret, err
}

// Get retrieves the FloatingIPPool from the indexer for a given namespace and name.
func (s floatingIPPoolNamespaceLister) Get(name string) (*v1alpha1.FloatingIPPool, error) {
	obj, exists, err := s.indexer.GetByKey(s.namespace + "/" + name)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, errors.NewNotFound(v1alpha1.Resource("floatingippool"), name)
	}
	return obj.(*v1alpha1.FloatingIPPool), nil
}
