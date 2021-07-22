/*
Copyright 2021 The Kubernetes Authors.

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

	kongfake "github.com/kong/kubernetes-ingress-controller/pkg/client/configuration/clientset/versioned/fake"
	kong_v1b1 "github.com/kong/kubernetes-ingress-controller/railgun/apis/configuration/v1beta1"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kubefake "k8s.io/client-go/kubernetes/fake"
	"sigs.k8s.io/external-dns/endpoint"
)

// This is a compile-time validation that glooSource is a Source.
var _ Source = &kongTCPIngressSource{}

const defaultKongNamespace = "kong"

func TestKongTCPIngressEndpoints(t *testing.T) {
	t.Parallel()

	for _, ti := range []struct {
		title    string
		tcpProxy *kong_v1b1.TCPIngress
		expected []*endpoint.Endpoint
	}{
		{
			title: "TCPIngress with hostname annotation",
			tcpProxy: &kong_v1b1.TCPIngress{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "tcp-ingress-annotation",
					Namespace: defaultKongNamespace,
					Annotations: map[string]string{
						"external-dns.alpha.kubernetes.io/hostname": "a.example.com",
						"kubernetes.io/ingress.class":               "kong",
					},
				},
				Spec: kong_v1b1.TCPIngressSpec{
					Rules: []kong_v1b1.IngressRule{
						{
							Port: 30000,
						},
						{
							Port: 30001,
						},
					},
				},
				Status: kong_v1b1.TCPIngressStatus{
					LoadBalancer: corev1.LoadBalancerStatus{
						Ingress: []corev1.LoadBalancerIngress{
							{
								Hostname: "a691234567a314e71861a4303f06a3bd-1291189659.us-east-1.elb.amazonaws.com",
							},
						},
					},
				},
			},
			expected: []*endpoint.Endpoint{
				{
					DNSName:    "a.example.com",
					Targets:    []string{"a691234567a314e71861a4303f06a3bd-1291189659.us-east-1.elb.amazonaws.com"},
					RecordType: endpoint.RecordTypeCNAME,
					RecordTTL:  0,
					Labels: endpoint.Labels{
						"resource": "tcpingress/kong/tcp-ingress-annotation",
					},
					ProviderSpecific: endpoint.ProviderSpecific{},
				},
			},
		},
		{
			title: "TCPIngress using SNI",
			tcpProxy: &kong_v1b1.TCPIngress{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "tcp-ingress-sni",
					Namespace: defaultKongNamespace,
					Annotations: map[string]string{
						"kubernetes.io/ingress.class": "kong",
					},
				},
				Spec: kong_v1b1.TCPIngressSpec{
					Rules: []kong_v1b1.IngressRule{
						{
							Port: 30002,
							Host: "b.example.com",
						},
						{
							Port: 30003,
							Host: "c.example.com",
						},
					},
				},
				Status: kong_v1b1.TCPIngressStatus{
					LoadBalancer: corev1.LoadBalancerStatus{
						Ingress: []corev1.LoadBalancerIngress{
							{
								Hostname: "a123456769a314e71861a4303f06a3bd-1291189659.us-east-1.elb.amazonaws.com",
							},
						},
					},
				},
			},
			expected: []*endpoint.Endpoint{
				{
					DNSName:    "b.example.com",
					Targets:    []string{"a123456769a314e71861a4303f06a3bd-1291189659.us-east-1.elb.amazonaws.com"},
					RecordType: endpoint.RecordTypeCNAME,
					RecordTTL:  0,
					Labels: endpoint.Labels{
						"resource": "tcpingress/kong/tcp-ingress-sni",
					},
					ProviderSpecific: endpoint.ProviderSpecific{},
				},
				{
					DNSName:    "c.example.com",
					Targets:    []string{"a123456769a314e71861a4303f06a3bd-1291189659.us-east-1.elb.amazonaws.com"},
					RecordType: endpoint.RecordTypeCNAME,
					Labels: endpoint.Labels{
						"resource": "tcpingress/kong/tcp-ingress-sni",
					},
					ProviderSpecific: endpoint.ProviderSpecific{},
				},
			},
		},
		{
			title: "TCPIngress with hostname annotation and using SNI",
			tcpProxy: &kong_v1b1.TCPIngress{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "tcp-ingress-both",
					Namespace: defaultKongNamespace,
					Annotations: map[string]string{
						"external-dns.alpha.kubernetes.io/hostname": "d.example.com",
						"kubernetes.io/ingress.class":               "kong",
					},
				},
				Spec: kong_v1b1.TCPIngressSpec{
					Rules: []kong_v1b1.IngressRule{
						{
							Port: 30004,
							Host: "e.example.com",
						},
						{
							Port: 30005,
							Host: "f.example.com",
						},
					},
				},
				Status: kong_v1b1.TCPIngressStatus{
					LoadBalancer: corev1.LoadBalancerStatus{
						Ingress: []corev1.LoadBalancerIngress{
							{
								Hostname: "a12e71861a4303f063456769a314a3bd-1291189659.us-east-1.elb.amazonaws.com",
							},
						},
					},
				},
			},
			expected: []*endpoint.Endpoint{
				{
					DNSName:    "d.example.com",
					Targets:    []string{"a12e71861a4303f063456769a314a3bd-1291189659.us-east-1.elb.amazonaws.com"},
					RecordType: endpoint.RecordTypeCNAME,
					RecordTTL:  0,
					Labels: endpoint.Labels{
						"resource": "tcpingress/kong/tcp-ingress-both",
					},
					ProviderSpecific: endpoint.ProviderSpecific{},
				},
				{
					DNSName:    "e.example.com",
					Targets:    []string{"a12e71861a4303f063456769a314a3bd-1291189659.us-east-1.elb.amazonaws.com"},
					RecordType: endpoint.RecordTypeCNAME,
					RecordTTL:  0,
					Labels: endpoint.Labels{
						"resource": "tcpingress/kong/tcp-ingress-both",
					},
					ProviderSpecific: endpoint.ProviderSpecific{},
				},
				{
					DNSName:    "f.example.com",
					Targets:    []string{"a12e71861a4303f063456769a314a3bd-1291189659.us-east-1.elb.amazonaws.com"},
					RecordType: endpoint.RecordTypeCNAME,
					RecordTTL:  0,
					Labels: endpoint.Labels{
						"resource": "tcpingress/kong/tcp-ingress-both",
					},
					ProviderSpecific: endpoint.ProviderSpecific{},
				},
			},
		},
	} {
		t.Run(ti.title, func(t *testing.T) {
			t.Parallel()

			ctx := context.Background()
			kubeClient := kubefake.NewSimpleClientset()
			kongClient := kongfake.NewSimpleClientset()

			_, err := kongClient.ConfigurationV1beta1().TCPIngresses(ti.tcpProxy.Namespace).Create(ctx, ti.tcpProxy, metav1.CreateOptions{})
			require.NoError(t, err)

			clients := new(MockClientGenerator)
			clients.On("KubeClient").Return(kubeClient, nil)
			clients.On("KongClient").Return(kongClient, nil)

			source, err := NewKongTCPIngressSource(clients, &Config{
				Namespace:        defaultKongNamespace,
				AnnotationFilter: "kubernetes.io/ingress.class=kong",
			})
			require.NoError(t, err)
			require.NotNil(t, source)

			endpoints, err := source.Endpoints(ctx)
			require.NoError(t, err)
			validateEndpoints(t, endpoints, ti.expected)
		})
	}
}
