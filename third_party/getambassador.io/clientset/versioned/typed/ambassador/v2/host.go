// Copyright Datawire.  All rights reserved
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
// Code generated by client-gen. DO NOT EDIT.

package v2

import (
	"context"
	"time"

	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	types "k8s.io/apimachinery/pkg/types"
	watch "k8s.io/apimachinery/pkg/watch"
	rest "k8s.io/client-go/rest"
	v2 "sigs.k8s.io/external-dns/third_party/getambassador.io/apis/ambassador/v2"
	scheme "sigs.k8s.io/external-dns/third_party/getambassador.io/clientset/versioned/scheme"
)

// HostsGetter has a method to return a HostInterface.
// A group's client should implement this interface.
type HostsGetter interface {
	Hosts(namespace string) HostInterface
}

// HostInterface has methods to work with Host resources.
type HostInterface interface {
	Create(ctx context.Context, host *v2.Host, opts v1.CreateOptions) (*v2.Host, error)
	Update(ctx context.Context, host *v2.Host, opts v1.UpdateOptions) (*v2.Host, error)
	UpdateStatus(ctx context.Context, host *v2.Host, opts v1.UpdateOptions) (*v2.Host, error)
	Delete(ctx context.Context, name string, opts v1.DeleteOptions) error
	DeleteCollection(ctx context.Context, opts v1.DeleteOptions, listOpts v1.ListOptions) error
	Get(ctx context.Context, name string, opts v1.GetOptions) (*v2.Host, error)
	List(ctx context.Context, opts v1.ListOptions) (*v2.HostList, error)
	Watch(ctx context.Context, opts v1.ListOptions) (watch.Interface, error)
	Patch(ctx context.Context, name string, pt types.PatchType, data []byte, opts v1.PatchOptions, subresources ...string) (result *v2.Host, err error)
	HostExpansion
}

// hosts implements HostInterface
type hosts struct {
	client rest.Interface
	ns     string
}

// newHosts returns a Hosts
func newHosts(c *GetambassadorV2Client, namespace string) *hosts {
	return &hosts{
		client: c.RESTClient(),
		ns:     namespace,
	}
}

// Get takes name of the host, and returns the corresponding host object, and an error if there is any.
func (c *hosts) Get(ctx context.Context, name string, options v1.GetOptions) (result *v2.Host, err error) {
	result = &v2.Host{}
	err = c.client.Get().
		Namespace(c.ns).
		Resource("hosts").
		Name(name).
		VersionedParams(&options, scheme.ParameterCodec).
		Do(ctx).
		Into(result)
	return
}

// List takes label and field selectors, and returns the list of Hosts that match those selectors.
func (c *hosts) List(ctx context.Context, opts v1.ListOptions) (result *v2.HostList, err error) {
	var timeout time.Duration
	if opts.TimeoutSeconds != nil {
		timeout = time.Duration(*opts.TimeoutSeconds) * time.Second
	}
	result = &v2.HostList{}
	err = c.client.Get().
		Namespace(c.ns).
		Resource("hosts").
		VersionedParams(&opts, scheme.ParameterCodec).
		Timeout(timeout).
		Do(ctx).
		Into(result)
	return
}

// Watch returns a watch.Interface that watches the requested hosts.
func (c *hosts) Watch(ctx context.Context, opts v1.ListOptions) (watch.Interface, error) {
	var timeout time.Duration
	if opts.TimeoutSeconds != nil {
		timeout = time.Duration(*opts.TimeoutSeconds) * time.Second
	}
	opts.Watch = true
	return c.client.Get().
		Namespace(c.ns).
		Resource("hosts").
		VersionedParams(&opts, scheme.ParameterCodec).
		Timeout(timeout).
		Watch(ctx)
}

// Create takes the representation of a host and creates it.  Returns the server's representation of the host, and an error, if there is any.
func (c *hosts) Create(ctx context.Context, host *v2.Host, opts v1.CreateOptions) (result *v2.Host, err error) {
	result = &v2.Host{}
	err = c.client.Post().
		Namespace(c.ns).
		Resource("hosts").
		VersionedParams(&opts, scheme.ParameterCodec).
		Body(host).
		Do(ctx).
		Into(result)
	return
}

// Update takes the representation of a host and updates it. Returns the server's representation of the host, and an error, if there is any.
func (c *hosts) Update(ctx context.Context, host *v2.Host, opts v1.UpdateOptions) (result *v2.Host, err error) {
	result = &v2.Host{}
	err = c.client.Put().
		Namespace(c.ns).
		Resource("hosts").
		Name(host.Name).
		VersionedParams(&opts, scheme.ParameterCodec).
		Body(host).
		Do(ctx).
		Into(result)
	return
}

// UpdateStatus was generated because the type contains a Status member.
// Add a +genclient:noStatus comment above the type to avoid generating UpdateStatus().
func (c *hosts) UpdateStatus(ctx context.Context, host *v2.Host, opts v1.UpdateOptions) (result *v2.Host, err error) {
	result = &v2.Host{}
	err = c.client.Put().
		Namespace(c.ns).
		Resource("hosts").
		Name(host.Name).
		SubResource("status").
		VersionedParams(&opts, scheme.ParameterCodec).
		Body(host).
		Do(ctx).
		Into(result)
	return
}

// Delete takes name of the host and deletes it. Returns an error if one occurs.
func (c *hosts) Delete(ctx context.Context, name string, opts v1.DeleteOptions) error {
	return c.client.Delete().
		Namespace(c.ns).
		Resource("hosts").
		Name(name).
		Body(&opts).
		Do(ctx).
		Error()
}

// DeleteCollection deletes a collection of objects.
func (c *hosts) DeleteCollection(ctx context.Context, opts v1.DeleteOptions, listOpts v1.ListOptions) error {
	var timeout time.Duration
	if listOpts.TimeoutSeconds != nil {
		timeout = time.Duration(*listOpts.TimeoutSeconds) * time.Second
	}
	return c.client.Delete().
		Namespace(c.ns).
		Resource("hosts").
		VersionedParams(&listOpts, scheme.ParameterCodec).
		Timeout(timeout).
		Body(&opts).
		Do(ctx).
		Error()
}

// Patch applies the patch and returns the patched host.
func (c *hosts) Patch(ctx context.Context, name string, pt types.PatchType, data []byte, opts v1.PatchOptions, subresources ...string) (result *v2.Host, err error) {
	result = &v2.Host{}
	err = c.client.Patch(pt).
		Namespace(c.ns).
		Resource("hosts").
		Name(name).
		SubResource(subresources...).
		VersionedParams(&opts, scheme.ParameterCodec).
		Body(data).
		Do(ctx).
		Into(result)
	return
}
