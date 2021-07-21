/*
Copyright 2020n The Kubernetes Authors.

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

package source

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kubefake "k8s.io/client-go/kubernetes/fake"

	"sigs.k8s.io/external-dns/endpoint"
	gloov1 "sigs.k8s.io/external-dns/third_party/solo.io/apis/gloo/v1"
	gloofake "sigs.k8s.io/external-dns/third_party/solo.io/clientset/versioned/fake"
)

// This is a compile-time validation that glooSource is a Source.
var _ Source = &glooSource{}

const defaultGlooNamespace = "gloo-system"

// Internal proxy test
var internalProxy = gloov1.Proxy{
	ObjectMeta: metav1.ObjectMeta{
		Name:      "internal",
		Namespace: defaultGlooNamespace,
	},
	Spec: gloov1.ProxySpec{
		Listeners: []gloov1.Listener{
			{
				HTTPListener: gloov1.HTTPListener{
					VirtualHosts: []gloov1.VirtualHost{
						{
							Domains: []string{"a.test", "b.test"},
							Metadata: gloov1.VirtualHostMetadata{
								Source: []gloov1.VirtualHostMetadataSource{
									{
										Kind:      "*v1.Unknown",
										Name:      "my-unknown-svc",
										Namespace: "unknown",
									},
								},
							},
						},
						{
							Domains: []string{"c.test"},
							Metadata: gloov1.VirtualHostMetadata{
								Source: []gloov1.VirtualHostMetadataSource{
									{
										Kind:      "*v1.VirtualService",
										Name:      "my-internal-svc",
										Namespace: "internal",
									},
								},
							},
						},
					},
				},
			},
		},
	},
}

var internalProxySvc = corev1.Service{
	ObjectMeta: metav1.ObjectMeta{
		Name:      internalProxy.Name,
		Namespace: internalProxy.Namespace,
	},
	Spec: corev1.ServiceSpec{
		Type: corev1.ServiceTypeLoadBalancer,
	},
	Status: corev1.ServiceStatus{
		LoadBalancer: corev1.LoadBalancerStatus{
			Ingress: []corev1.LoadBalancerIngress{
				{IP: "203.0.113.1"},
				{IP: "203.0.113.2"},
				{IP: "203.0.113.3"},
			},
		},
	},
}

var internalProxySource = gloov1.VirtualService{
	ObjectMeta: metav1.ObjectMeta{
		Name:      internalProxy.Spec.Listeners[0].HTTPListener.VirtualHosts[1].Metadata.Source[0].Name,
		Namespace: internalProxy.Spec.Listeners[0].HTTPListener.VirtualHosts[1].Metadata.Source[0].Namespace,
		Annotations: map[string]string{
			"external-dns.alpha.kubernetes.io/ttl":                          "42",
			"external-dns.alpha.kubernetes.io/aws-geolocation-country-code": "LU",
			"external-dns.alpha.kubernetes.io/set-identifier":               "identifier",
		},
	},
}

// External proxy test
var externalProxy = gloov1.Proxy{
	ObjectMeta: metav1.ObjectMeta{
		Name:      "external",
		Namespace: defaultGlooNamespace,
	},
	Spec: gloov1.ProxySpec{
		Listeners: []gloov1.Listener{
			{
				HTTPListener: gloov1.HTTPListener{
					VirtualHosts: []gloov1.VirtualHost{
						{
							Domains: []string{"d.test"},
							Metadata: gloov1.VirtualHostMetadata{
								Source: []gloov1.VirtualHostMetadataSource{
									{
										Kind:      "*v1.Unknown",
										Name:      "my-unknown-svc",
										Namespace: "unknown",
									},
								},
							},
						},
						{
							Domains: []string{"e.test"},
							Metadata: gloov1.VirtualHostMetadata{
								Source: []gloov1.VirtualHostMetadataSource{
									{
										Kind:      "*v1.VirtualService",
										Name:      "my-external-svc",
										Namespace: "external",
									},
								},
							},
						},
					},
				},
			},
		},
	},
}

var externalProxySvc = corev1.Service{
	ObjectMeta: metav1.ObjectMeta{
		Name:      externalProxy.Name,
		Namespace: externalProxy.Namespace,
	},
	Spec: corev1.ServiceSpec{
		Type: corev1.ServiceTypeLoadBalancer,
	},
	Status: corev1.ServiceStatus{
		LoadBalancer: corev1.LoadBalancerStatus{
			Ingress: []corev1.LoadBalancerIngress{
				{Hostname: "a.example.org"},
				{Hostname: "b.example.org"},
				{Hostname: "c.example.org"},
			},
		},
	},
}

var externalProxySource = gloov1.VirtualService{
	ObjectMeta: metav1.ObjectMeta{
		Name:      externalProxy.Spec.Listeners[0].HTTPListener.VirtualHosts[1].Metadata.Source[0].Name,
		Namespace: externalProxy.Spec.Listeners[0].HTTPListener.VirtualHosts[1].Metadata.Source[0].Namespace,
		Annotations: map[string]string{
			"external-dns.alpha.kubernetes.io/ttl":                          "24",
			"external-dns.alpha.kubernetes.io/aws-geolocation-country-code": "JP",
			"external-dns.alpha.kubernetes.io/set-identifier":               "identifier-external",
		},
	},
}

func TestGlooSource(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	kubeClient := kubefake.NewSimpleClientset()
	glooClient := gloofake.NewSimpleClientset()

	// Create proxy resources
	_, err := glooClient.GatewayV1().Proxies(internalProxy.Namespace).Create(ctx, &internalProxy, metav1.CreateOptions{})
	assert.NoError(t, err)
	_, err = glooClient.GatewayV1().Proxies(externalProxy.Namespace).Create(ctx, &externalProxy, metav1.CreateOptions{})
	assert.NoError(t, err)

	// Create proxy source
	_, err = glooClient.GatewayV1().VirtualServices(internalProxySource.Namespace).Create(ctx, &internalProxySource, metav1.CreateOptions{})
	assert.NoError(t, err)
	_, err = glooClient.GatewayV1().VirtualServices(externalProxySource.Namespace).Create(ctx, &externalProxySource, metav1.CreateOptions{})
	assert.NoError(t, err)

	// Create proxy service resources
	_, err = kubeClient.CoreV1().Services(internalProxySvc.Namespace).Create(ctx, &internalProxySvc, metav1.CreateOptions{})
	assert.NoError(t, err)
	_, err = kubeClient.CoreV1().Services(externalProxySvc.Namespace).Create(ctx, &externalProxySvc, metav1.CreateOptions{})
	assert.NoError(t, err)

	clients := new(MockClientGenerator)
	clients.On("KubeClient").Return(kubeClient, nil)
	clients.On("GlooClient").Return(glooClient, nil)

	source, err := NewGlooSource(clients, &Config{
		GlooNamespace: defaultGlooNamespace,
	})
	assert.NoError(t, err)
	assert.NotNil(t, source)

	endpoints, err := source.Endpoints(ctx)
	assert.NoError(t, err)
	assert.Len(t, endpoints, 5)
	validateEndpoints(t, endpoints, []*endpoint.Endpoint{
		{
			DNSName:          "a.test",
			Targets:          []string{internalProxySvc.Status.LoadBalancer.Ingress[0].IP, internalProxySvc.Status.LoadBalancer.Ingress[1].IP, internalProxySvc.Status.LoadBalancer.Ingress[2].IP},
			RecordType:       endpoint.RecordTypeA,
			RecordTTL:        0,
			Labels:           endpoint.Labels{},
			ProviderSpecific: endpoint.ProviderSpecific{},
		},
		{
			DNSName:          "b.test",
			Targets:          []string{internalProxySvc.Status.LoadBalancer.Ingress[0].IP, internalProxySvc.Status.LoadBalancer.Ingress[1].IP, internalProxySvc.Status.LoadBalancer.Ingress[2].IP},
			RecordType:       endpoint.RecordTypeA,
			RecordTTL:        0,
			Labels:           endpoint.Labels{},
			ProviderSpecific: endpoint.ProviderSpecific{},
		},
		{
			DNSName:       "c.test",
			Targets:       []string{internalProxySvc.Status.LoadBalancer.Ingress[0].IP, internalProxySvc.Status.LoadBalancer.Ingress[1].IP, internalProxySvc.Status.LoadBalancer.Ingress[2].IP},
			RecordType:    endpoint.RecordTypeA,
			SetIdentifier: "identifier",
			RecordTTL:     42,
			Labels:        endpoint.Labels{},
			ProviderSpecific: endpoint.ProviderSpecific{
				endpoint.ProviderSpecificProperty{
					Name:  "aws/geolocation-country-code",
					Value: "LU",
				},
			},
		},
		{
			DNSName:          "d.test",
			Targets:          []string{externalProxySvc.Status.LoadBalancer.Ingress[0].Hostname, externalProxySvc.Status.LoadBalancer.Ingress[1].Hostname, externalProxySvc.Status.LoadBalancer.Ingress[2].Hostname},
			RecordType:       endpoint.RecordTypeCNAME,
			RecordTTL:        0,
			Labels:           endpoint.Labels{},
			ProviderSpecific: endpoint.ProviderSpecific{},
		},
		{
			DNSName:       "e.test",
			Targets:       []string{externalProxySvc.Status.LoadBalancer.Ingress[0].Hostname, externalProxySvc.Status.LoadBalancer.Ingress[1].Hostname, externalProxySvc.Status.LoadBalancer.Ingress[2].Hostname},
			RecordType:    endpoint.RecordTypeCNAME,
			SetIdentifier: "identifier-external",
			RecordTTL:     24,
			Labels:        endpoint.Labels{},
			ProviderSpecific: endpoint.ProviderSpecific{
				endpoint.ProviderSpecificProperty{
					Name:  "aws/geolocation-country-code",
					Value: "JP",
				},
			},
		},
	})
}
