/*
Copyright 2020 The Kubernetes Authors.

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
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"sigs.k8s.io/external-dns/endpoint"
	projcontour_v1 "sigs.k8s.io/external-dns/third_party/projectcontour.io/apis/projectcontour/v1"
)

// This is a compile-time validation that httpProxySource is a Source.
var _ Source = &httpProxySource{}

type HTTPProxySuite struct {
	suite.Suite
	source    Source
	httpProxy *projcontour_v1.HTTPProxy
}

func (suite *HTTPProxySuite) SetupTest() {
	ctx := context.Background()
	_, contourClient, clients := fakeContourClients()

	suite.httpProxy = (fakeHTTPProxy{
		name:      "foo-httpproxy-with-targets",
		namespace: "default",
		host:      "example.com",
	}).HTTPProxy()
	_, err := contourClient.ProjectcontourV1().HTTPProxies(suite.httpProxy.Namespace).Create(ctx, suite.httpProxy, metav1.CreateOptions{})
	suite.NoError(err, "should succeed")

	suite.source, err = NewContourHTTPProxySource(clients, &Config{
		Namespace:    "default",
		FQDNTemplate: "{{.Name}}",
	})
	suite.NoError(err, "should initialize httpproxy source")
}

func (suite *HTTPProxySuite) TestResourceLabelIsSet() {
	endpoints, _ := suite.source.Endpoints(context.Background())
	for _, ep := range endpoints {
		suite.Equal("httpproxy/default/foo-httpproxy-with-targets", ep.Labels[endpoint.ResourceLabelKey], "should set correct resource label")
	}
}

func TestHTTPProxy(t *testing.T) {
	t.Parallel()

	suite.Run(t, new(HTTPProxySuite))
	t.Run("endpointsFromHTTPProxy", testEndpointsFromHTTPProxy)
	t.Run("Endpoints", testHTTPProxyEndpoints)
}

func TestNewContourHTTPProxySource(t *testing.T) {
	t.Parallel()

	for _, ti := range []struct {
		title                    string
		annotationFilter         string
		fqdnTemplate             string
		combineFQDNAndAnnotation bool
		expectError              bool
	}{
		{
			title:        "invalid template",
			expectError:  true,
			fqdnTemplate: "{{.Name",
		},
		{
			title:       "valid empty template",
			expectError: false,
		},
		{
			title:        "valid template",
			expectError:  false,
			fqdnTemplate: "{{.Name}}-{{.Namespace}}.ext-dns.test.com",
		},
		{
			title:        "valid template",
			expectError:  false,
			fqdnTemplate: "{{.Name}}-{{.Namespace}}.ext-dns.test.com, {{.Name}}-{{.Namespace}}.ext-dna.test.com",
		},
		{
			title:                    "valid template",
			expectError:              false,
			fqdnTemplate:             "{{.Name}}-{{.Namespace}}.ext-dns.test.com, {{.Name}}-{{.Namespace}}.ext-dna.test.com",
			combineFQDNAndAnnotation: true,
		},
		{
			title:            "non-empty annotation filter label",
			expectError:      false,
			annotationFilter: "contour.heptio.com/ingress.class=contour",
		},
	} {
		t.Run(ti.title, func(t *testing.T) {
			t.Parallel()

			_, _, clients := fakeContourClients()
			_, err := NewContourHTTPProxySource(clients, &Config{
				AnnotationFilter:         ti.annotationFilter,
				FQDNTemplate:             ti.fqdnTemplate,
				CombineFQDNAndAnnotation: ti.combineFQDNAndAnnotation,
			})
			if ti.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func testEndpointsFromHTTPProxy(t *testing.T) {
	t.Parallel()

	for _, ti := range []struct {
		title     string
		httpProxy fakeHTTPProxy
		expected  []*endpoint.Endpoint
	}{
		{
			title: "one rule.host one lb.hostname",
			httpProxy: fakeHTTPProxy{
				name:      "proxy",
				namespace: "httpproxy-test",
				host:      "foo.bar", // Kubernetes requires removal of trailing dot
				loadBalancer: fakeLoadBalancerService{
					hostnames: []string{"lb.com"}, // Kubernetes omits the trailing dot
				},
			},
			expected: []*endpoint.Endpoint{
				{
					DNSName: "foo.bar",
					Targets: endpoint.Targets{"lb.com"},
				},
			},
		},
		{
			title: "one rule.host one lb.IP",
			httpProxy: fakeHTTPProxy{
				name:      "proxy",
				namespace: "httpproxy-test",
				host:      "foo.bar",
				loadBalancer: fakeLoadBalancerService{
					ips: []string{"8.8.8.8"},
				},
			},
			expected: []*endpoint.Endpoint{
				{
					DNSName: "foo.bar",
					Targets: endpoint.Targets{"8.8.8.8"},
				},
			},
		},
		{
			title: "one rule.host two lb.IP and two lb.Hostname",
			httpProxy: fakeHTTPProxy{
				name:      "proxy",
				namespace: "httpproxy-test",
				host:      "foo.bar",
				loadBalancer: fakeLoadBalancerService{
					ips:       []string{"8.8.8.8", "127.0.0.1"},
					hostnames: []string{"elb.com", "alb.com"},
				},
			},
			expected: []*endpoint.Endpoint{
				{
					DNSName: "foo.bar",
					Targets: endpoint.Targets{"8.8.8.8", "127.0.0.1"},
				},
				{
					DNSName: "foo.bar",
					Targets: endpoint.Targets{"elb.com", "alb.com"},
				},
			},
		},
		{
			title: "no rule.host",
			httpProxy: fakeHTTPProxy{
				name:      "proxy",
				namespace: "httpproxy-test",
			},
			expected: []*endpoint.Endpoint{},
		},
		{
			title: "one rule.host invalid httpproxy",
			httpProxy: fakeHTTPProxy{
				name:      "proxy",
				namespace: "httpproxy-test",
				host:      "foo.bar",
				invalid:   true,
			},
			expected: []*endpoint.Endpoint{},
		},
		{
			title: "no targets",
			httpProxy: fakeHTTPProxy{
				name:      "proxy",
				namespace: "httpproxy-test",
			},
			expected: []*endpoint.Endpoint{},
		},
		{
			title: "delegate httpproxy",
			httpProxy: fakeHTTPProxy{
				name:      "proxy",
				namespace: "httpproxy-test",
				delegate:  true,
			},
			expected: []*endpoint.Endpoint{},
		},
	} {
		t.Run(ti.title, func(t *testing.T) {
			t.Parallel()

			ctx := context.Background()
			_, contourClient, clients := fakeContourClients()

			hp := ti.httpProxy.HTTPProxy()
			_, err := contourClient.ProjectcontourV1().HTTPProxies(hp.Namespace).Create(ctx, hp, metav1.CreateOptions{})
			require.NoError(t, err)

			src, err := NewContourHTTPProxySource(clients, &Config{
				Namespace:    "default",
				FQDNTemplate: "{{.Name}}",
			})
			require.NoError(t, err)

			endpoints, err := src.Endpoints(ctx)
			require.NoError(t, err)
			validateEndpoints(t, endpoints, ti.expected)
		})
	}
}

func testHTTPProxyEndpoints(t *testing.T) {
	t.Parallel()

	namespace := "testing"
	for _, ti := range []struct {
		title                    string
		targetNamespace          string
		annotationFilter         string
		loadBalancer             fakeLoadBalancerService
		httpProxyItems           []fakeHTTPProxy
		expected                 []*endpoint.Endpoint
		expectError              bool
		fqdnTemplate             string
		combineFQDNAndAnnotation bool
		ignoreHostnameAnnotation bool
	}{
		{
			title:           "no httpproxy",
			targetNamespace: "",
		},
		{
			title:           "two simple httpproxys",
			targetNamespace: "",
			loadBalancer: fakeLoadBalancerService{
				ips:       []string{"8.8.8.8"},
				hostnames: []string{"lb.com"},
			},
			httpProxyItems: []fakeHTTPProxy{
				{
					name:      "fake1",
					namespace: namespace,
					host:      "example.org",
				},
				{
					name:      "fake2",
					namespace: namespace,
					host:      "new.org",
				},
			},
			expected: []*endpoint.Endpoint{
				{
					DNSName: "example.org",
					Targets: endpoint.Targets{"8.8.8.8"},
				},
				{
					DNSName: "example.org",
					Targets: endpoint.Targets{"lb.com"},
				},
				{
					DNSName: "new.org",
					Targets: endpoint.Targets{"8.8.8.8"},
				},
				{
					DNSName: "new.org",
					Targets: endpoint.Targets{"lb.com"},
				},
			},
		},
		{
			title:           "two simple httpproxys on different namespaces",
			targetNamespace: "",
			loadBalancer: fakeLoadBalancerService{
				ips:       []string{"8.8.8.8"},
				hostnames: []string{"lb.com"},
			},
			httpProxyItems: []fakeHTTPProxy{
				{
					name:      "fake1",
					namespace: "testing1",
					host:      "example.org",
				},
				{
					name:      "fake2",
					namespace: "testing2",
					host:      "new.org",
				},
			},
			expected: []*endpoint.Endpoint{
				{
					DNSName: "example.org",
					Targets: endpoint.Targets{"8.8.8.8"},
				},
				{
					DNSName: "example.org",
					Targets: endpoint.Targets{"lb.com"},
				},
				{
					DNSName: "new.org",
					Targets: endpoint.Targets{"8.8.8.8"},
				},
				{
					DNSName: "new.org",
					Targets: endpoint.Targets{"lb.com"},
				},
			},
		},
		{
			title:           "two simple httpproxys on different namespaces and a target namespace",
			targetNamespace: "testing1",
			loadBalancer: fakeLoadBalancerService{
				ips:       []string{"8.8.8.8"},
				hostnames: []string{"lb.com"},
			},
			httpProxyItems: []fakeHTTPProxy{
				{
					name:      "fake1",
					namespace: "testing1",
					host:      "example.org",
				},
				{
					name:      "fake2",
					namespace: "testing2",
					host:      "new.org",
				},
			},
			expected: []*endpoint.Endpoint{
				{
					DNSName: "example.org",
					Targets: endpoint.Targets{"8.8.8.8"},
				},
				{
					DNSName: "example.org",
					Targets: endpoint.Targets{"lb.com"},
				},
			},
		},
		{
			title:            "valid matching annotation filter expression",
			targetNamespace:  "",
			annotationFilter: "contour.heptio.com/ingress.class in (alb, contour)",
			loadBalancer: fakeLoadBalancerService{
				ips: []string{"8.8.8.8"},
			},
			httpProxyItems: []fakeHTTPProxy{
				{
					name:      "fake1",
					namespace: namespace,
					annotations: map[string]string{
						"contour.heptio.com/ingress.class": "contour",
					},
					host: "example.org",
				},
			},
			expected: []*endpoint.Endpoint{
				{
					DNSName: "example.org",
					Targets: endpoint.Targets{"8.8.8.8"},
				},
			},
		},
		{
			title:            "valid non-matching annotation filter expression",
			targetNamespace:  "",
			annotationFilter: "contour.heptio.com/ingress.class in (alb, contour)",
			loadBalancer: fakeLoadBalancerService{
				ips: []string{"8.8.8.8"},
			},
			httpProxyItems: []fakeHTTPProxy{
				{
					name:      "fake1",
					namespace: namespace,
					annotations: map[string]string{
						"contour.heptio.com/ingress.class": "tectonic",
					},
					host: "example.org",
				},
			},
			expected: []*endpoint.Endpoint{},
		},
		{
			title:            "invalid annotation filter expression",
			targetNamespace:  "",
			annotationFilter: "contour.heptio.com/ingress.name in (a b)",
			loadBalancer: fakeLoadBalancerService{
				ips: []string{"8.8.8.8"},
			},
			httpProxyItems: []fakeHTTPProxy{
				{
					name:      "fake1",
					namespace: namespace,
					annotations: map[string]string{
						"contour.heptio.com/ingress.class": "alb",
					},
					host: "example.org",
				},
			},
			expected:    []*endpoint.Endpoint{},
			expectError: true,
		},
		{
			title:            "valid matching annotation filter label",
			targetNamespace:  "",
			annotationFilter: "contour.heptio.com/ingress.class=contour",
			loadBalancer: fakeLoadBalancerService{
				ips: []string{"8.8.8.8"},
			},
			httpProxyItems: []fakeHTTPProxy{
				{
					name:      "fake1",
					namespace: namespace,
					annotations: map[string]string{
						"contour.heptio.com/ingress.class": "contour",
					},
					host: "example.org",
				},
			},
			expected: []*endpoint.Endpoint{
				{
					DNSName: "example.org",
					Targets: endpoint.Targets{"8.8.8.8"},
				},
			},
		},
		{
			title:            "valid non-matching annotation filter label",
			targetNamespace:  "",
			annotationFilter: "contour.heptio.com/ingress.class=contour",
			loadBalancer: fakeLoadBalancerService{
				ips: []string{"8.8.8.8"},
			},
			httpProxyItems: []fakeHTTPProxy{
				{
					name:      "fake1",
					namespace: namespace,
					annotations: map[string]string{
						"contour.heptio.com/ingress.class": "alb",
					},
					host: "example.org",
				},
			},
			expected: []*endpoint.Endpoint{},
		},
		{
			title:           "our controller type is dns-controller",
			targetNamespace: "",
			loadBalancer: fakeLoadBalancerService{
				ips: []string{"8.8.8.8"},
			},
			httpProxyItems: []fakeHTTPProxy{
				{
					name:      "fake1",
					namespace: namespace,
					annotations: map[string]string{
						controllerAnnotationKey: controllerAnnotationValue,
					},
					host: "example.org",
				},
			},
			expected: []*endpoint.Endpoint{
				{
					DNSName: "example.org",
					Targets: endpoint.Targets{"8.8.8.8"},
				},
			},
		},
		{
			title:           "different controller types are ignored",
			targetNamespace: "",
			loadBalancer: fakeLoadBalancerService{
				ips: []string{"8.8.8.8"},
			},
			httpProxyItems: []fakeHTTPProxy{
				{
					name:      "fake1",
					namespace: namespace,
					annotations: map[string]string{
						controllerAnnotationKey: "some-other-tool",
					},
					host: "example.org",
				},
			},
			expected: []*endpoint.Endpoint{},
		},
		{
			title:           "template for httpproxy if host is missing",
			targetNamespace: "",
			loadBalancer: fakeLoadBalancerService{
				ips:       []string{"8.8.8.8"},
				hostnames: []string{"elb.com"},
			},
			httpProxyItems: []fakeHTTPProxy{
				{
					name:      "fake1",
					namespace: namespace,
					annotations: map[string]string{
						controllerAnnotationKey: controllerAnnotationValue,
					},
					host: "",
				},
			},
			expected: []*endpoint.Endpoint{
				{
					DNSName: "fake1.ext-dns.test.com",
					Targets: endpoint.Targets{"8.8.8.8"},
				},
				{
					DNSName: "fake1.ext-dns.test.com",
					Targets: endpoint.Targets{"elb.com"},
				},
			},
			fqdnTemplate: "{{.Name}}.ext-dns.test.com",
		},
		{
			title:           "another controller annotation skipped even with template",
			targetNamespace: "",
			loadBalancer: fakeLoadBalancerService{
				ips: []string{"8.8.8.8"},
			},
			httpProxyItems: []fakeHTTPProxy{
				{
					name:      "fake1",
					namespace: namespace,
					annotations: map[string]string{
						controllerAnnotationKey: "other-controller",
					},
					host: "",
				},
			},
			expected:     []*endpoint.Endpoint{},
			fqdnTemplate: "{{.Name}}.ext-dns.test.com",
		},
		{
			title:           "multiple FQDN template hostnames",
			targetNamespace: "",
			loadBalancer: fakeLoadBalancerService{
				ips: []string{"8.8.8.8"},
			},
			httpProxyItems: []fakeHTTPProxy{
				{
					name:        "fake1",
					namespace:   namespace,
					annotations: map[string]string{},
					host:        "",
				},
			},
			expected: []*endpoint.Endpoint{
				{
					DNSName:    "fake1.ext-dns.test.com",
					Targets:    endpoint.Targets{"8.8.8.8"},
					RecordType: endpoint.RecordTypeA,
				},
				{
					DNSName:    "fake1.ext-dna.test.com",
					Targets:    endpoint.Targets{"8.8.8.8"},
					RecordType: endpoint.RecordTypeA,
				},
			},
			fqdnTemplate: "{{.Name}}.ext-dns.test.com, {{.Name}}.ext-dna.test.com",
		},
		{
			title:           "multiple FQDN template hostnames",
			targetNamespace: "",
			loadBalancer: fakeLoadBalancerService{
				ips: []string{"8.8.8.8"},
			},
			httpProxyItems: []fakeHTTPProxy{
				{
					name:        "fake1",
					namespace:   namespace,
					annotations: map[string]string{},
					host:        "",
				},
				{
					name:      "fake2",
					namespace: namespace,
					annotations: map[string]string{
						targetAnnotationKey: "httpproxy-target.com",
					},
					host: "example.org",
				},
			},
			expected: []*endpoint.Endpoint{
				{
					DNSName:    "fake1.ext-dns.test.com",
					Targets:    endpoint.Targets{"8.8.8.8"},
					RecordType: endpoint.RecordTypeA,
				},
				{
					DNSName:    "fake1.ext-dna.test.com",
					Targets:    endpoint.Targets{"8.8.8.8"},
					RecordType: endpoint.RecordTypeA,
				},
				{
					DNSName:    "example.org",
					Targets:    endpoint.Targets{"httpproxy-target.com"},
					RecordType: endpoint.RecordTypeCNAME,
				},
				{
					DNSName:    "fake2.ext-dns.test.com",
					Targets:    endpoint.Targets{"httpproxy-target.com"},
					RecordType: endpoint.RecordTypeCNAME,
				},
				{
					DNSName:    "fake2.ext-dna.test.com",
					Targets:    endpoint.Targets{"httpproxy-target.com"},
					RecordType: endpoint.RecordTypeCNAME,
				},
			},
			fqdnTemplate:             "{{.Name}}.ext-dns.test.com, {{.Name}}.ext-dna.test.com",
			combineFQDNAndAnnotation: true,
		},
		{
			title:           "httpproxy rules with annotation",
			targetNamespace: "",
			loadBalancer: fakeLoadBalancerService{
				ips: []string{"8.8.8.8"},
			},
			httpProxyItems: []fakeHTTPProxy{
				{
					name:      "fake1",
					namespace: namespace,
					annotations: map[string]string{
						targetAnnotationKey: "httpproxy-target.com",
					},
					host: "example.org",
				},
				{
					name:      "fake2",
					namespace: namespace,
					annotations: map[string]string{
						targetAnnotationKey: "httpproxy-target.com",
					},
					host: "example2.org",
				},
				{
					name:      "fake3",
					namespace: namespace,
					annotations: map[string]string{
						targetAnnotationKey: "1.2.3.4",
					},
					host: "example3.org",
				},
			},
			expected: []*endpoint.Endpoint{
				{
					DNSName:    "example.org",
					Targets:    endpoint.Targets{"httpproxy-target.com"},
					RecordType: endpoint.RecordTypeCNAME,
				},
				{
					DNSName:    "example2.org",
					Targets:    endpoint.Targets{"httpproxy-target.com"},
					RecordType: endpoint.RecordTypeCNAME,
				},
				{
					DNSName:    "example3.org",
					Targets:    endpoint.Targets{"1.2.3.4"},
					RecordType: endpoint.RecordTypeA,
				},
			},
		},
		{
			title:           "httpproxy rules with hostname annotation",
			targetNamespace: "",
			loadBalancer: fakeLoadBalancerService{
				ips: []string{"1.2.3.4"},
			},
			httpProxyItems: []fakeHTTPProxy{
				{
					name:      "fake1",
					namespace: namespace,
					annotations: map[string]string{
						hostnameAnnotationKey: "dns-through-hostname.com",
					},
					host: "example.org",
				},
			},
			expected: []*endpoint.Endpoint{
				{
					DNSName:    "example.org",
					Targets:    endpoint.Targets{"1.2.3.4"},
					RecordType: endpoint.RecordTypeA,
				},
				{
					DNSName:    "dns-through-hostname.com",
					Targets:    endpoint.Targets{"1.2.3.4"},
					RecordType: endpoint.RecordTypeA,
				},
			},
		},
		{
			title:           "httpproxy rules with hostname annotation having multiple hostnames",
			targetNamespace: "",
			loadBalancer: fakeLoadBalancerService{
				ips: []string{"1.2.3.4"},
			},
			httpProxyItems: []fakeHTTPProxy{
				{
					name:      "fake1",
					namespace: namespace,
					annotations: map[string]string{
						hostnameAnnotationKey: "dns-through-hostname.com, another-dns-through-hostname.com",
					},
					host: "example.org",
				},
			},
			expected: []*endpoint.Endpoint{
				{
					DNSName:    "example.org",
					Targets:    endpoint.Targets{"1.2.3.4"},
					RecordType: endpoint.RecordTypeA,
				},
				{
					DNSName:    "dns-through-hostname.com",
					Targets:    endpoint.Targets{"1.2.3.4"},
					RecordType: endpoint.RecordTypeA,
				},
				{
					DNSName:    "another-dns-through-hostname.com",
					Targets:    endpoint.Targets{"1.2.3.4"},
					RecordType: endpoint.RecordTypeA,
				},
			},
		},
		{
			title:           "httpproxy rules with hostname and target annotation",
			targetNamespace: "",
			loadBalancer: fakeLoadBalancerService{
				ips: []string{},
			},
			httpProxyItems: []fakeHTTPProxy{
				{
					name:      "fake1",
					namespace: namespace,
					annotations: map[string]string{
						hostnameAnnotationKey: "dns-through-hostname.com",
						targetAnnotationKey:   "httpproxy-target.com",
					},
					host: "example.org",
				},
			},
			expected: []*endpoint.Endpoint{
				{
					DNSName:    "example.org",
					Targets:    endpoint.Targets{"httpproxy-target.com"},
					RecordType: endpoint.RecordTypeCNAME,
				},
				{
					DNSName:    "dns-through-hostname.com",
					Targets:    endpoint.Targets{"httpproxy-target.com"},
					RecordType: endpoint.RecordTypeCNAME,
				},
			},
		},
		{
			title:           "httpproxy rules with annotation and custom TTL",
			targetNamespace: "",
			loadBalancer: fakeLoadBalancerService{
				ips: []string{"8.8.8.8"},
			},
			httpProxyItems: []fakeHTTPProxy{
				{
					name:      "fake1",
					namespace: namespace,
					annotations: map[string]string{
						targetAnnotationKey: "httpproxy-target.com",
						ttlAnnotationKey:    "6",
					},
					host: "example.org",
				},
				{
					name:      "fake2",
					namespace: namespace,
					annotations: map[string]string{
						targetAnnotationKey: "httpproxy-target.com",
						ttlAnnotationKey:    "1",
					},
					host: "example2.org",
				},
				{
					name:      "fake3",
					namespace: namespace,
					annotations: map[string]string{
						targetAnnotationKey: "httpproxy-target.com",
						ttlAnnotationKey:    "10s",
					},
					host: "example3.org",
				},
			},
			expected: []*endpoint.Endpoint{
				{
					DNSName:   "example.org",
					Targets:   endpoint.Targets{"httpproxy-target.com"},
					RecordTTL: endpoint.TTL(6),
				},
				{
					DNSName:   "example2.org",
					Targets:   endpoint.Targets{"httpproxy-target.com"},
					RecordTTL: endpoint.TTL(1),
				},
				{
					DNSName:   "example3.org",
					Targets:   endpoint.Targets{"httpproxy-target.com"},
					RecordTTL: endpoint.TTL(10),
				},
			},
		},
		{
			title:           "template for httpproxy with annotation",
			targetNamespace: "",
			loadBalancer: fakeLoadBalancerService{
				ips:       []string{},
				hostnames: []string{},
			},
			httpProxyItems: []fakeHTTPProxy{
				{
					name:      "fake1",
					namespace: namespace,
					annotations: map[string]string{
						targetAnnotationKey: "httpproxy-target.com",
					},
					host: "",
				},
				{
					name:      "fake2",
					namespace: namespace,
					annotations: map[string]string{
						targetAnnotationKey: "httpproxy-target.com",
					},
					host: "",
				},
				{
					name:      "fake3",
					namespace: namespace,
					annotations: map[string]string{
						targetAnnotationKey: "1.2.3.4",
					},
					host: "",
				},
			},
			expected: []*endpoint.Endpoint{
				{
					DNSName:    "fake1.ext-dns.test.com",
					Targets:    endpoint.Targets{"httpproxy-target.com"},
					RecordType: endpoint.RecordTypeCNAME,
				},
				{
					DNSName:    "fake2.ext-dns.test.com",
					Targets:    endpoint.Targets{"httpproxy-target.com"},
					RecordType: endpoint.RecordTypeCNAME,
				},
				{
					DNSName:    "fake3.ext-dns.test.com",
					Targets:    endpoint.Targets{"1.2.3.4"},
					RecordType: endpoint.RecordTypeA,
				},
			},
			fqdnTemplate: "{{.Name}}.ext-dns.test.com",
		},
		{
			title:           "httpproxy with empty annotation",
			targetNamespace: "",
			loadBalancer: fakeLoadBalancerService{
				ips:       []string{},
				hostnames: []string{},
			},
			httpProxyItems: []fakeHTTPProxy{
				{
					name:      "fake1",
					namespace: namespace,
					annotations: map[string]string{
						targetAnnotationKey: "",
					},
					host: "",
				},
			},
			expected:     []*endpoint.Endpoint{},
			fqdnTemplate: "{{.Name}}.ext-dns.test.com",
		},
		{
			title:           "ignore hostname annotations",
			targetNamespace: "",
			loadBalancer: fakeLoadBalancerService{
				ips:       []string{"8.8.8.8"},
				hostnames: []string{"lb.com"},
			},
			httpProxyItems: []fakeHTTPProxy{
				{
					name:      "fake1",
					namespace: namespace,
					annotations: map[string]string{
						hostnameAnnotationKey: "ignore.me",
					},
					host: "example.org",
				},
				{
					name:      "fake2",
					namespace: namespace,
					annotations: map[string]string{
						hostnameAnnotationKey: "ignore.me.too",
					},
					host: "new.org",
				},
			},
			expected: []*endpoint.Endpoint{
				{
					DNSName: "example.org",
					Targets: endpoint.Targets{"8.8.8.8"},
				},
				{
					DNSName: "example.org",
					Targets: endpoint.Targets{"lb.com"},
				},
				{
					DNSName: "new.org",
					Targets: endpoint.Targets{"8.8.8.8"},
				},
				{
					DNSName: "new.org",
					Targets: endpoint.Targets{"lb.com"},
				},
			},
			ignoreHostnameAnnotation: true,
		},
	} {
		t.Run(ti.title, func(t *testing.T) {
			t.Parallel()

			ctx := context.Background()
			_, contourClient, clients := fakeContourClients()

			for _, item := range ti.httpProxyItems {
				item.loadBalancer = ti.loadBalancer
				hp := item.HTTPProxy()
				_, err := contourClient.ProjectcontourV1().HTTPProxies(hp.Namespace).Create(ctx, hp, metav1.CreateOptions{})
				require.NoError(t, err)
			}

			src, err := NewContourHTTPProxySource(clients, &Config{
				Namespace:                ti.targetNamespace,
				AnnotationFilter:         ti.annotationFilter,
				FQDNTemplate:             ti.fqdnTemplate,
				CombineFQDNAndAnnotation: ti.combineFQDNAndAnnotation,
				IgnoreHostnameAnnotation: ti.ignoreHostnameAnnotation,
			})
			if ti.expectError {
				require.Error(t, err)
			}
			require.NoError(t, err)

			res, err := src.Endpoints(ctx)
			assert.NoError(t, err)
			validateEndpoints(t, res, ti.expected)
		})
	}
}

type fakeHTTPProxy struct {
	namespace   string
	name        string
	annotations map[string]string

	host         string
	invalid      bool
	delegate     bool
	loadBalancer fakeLoadBalancerService
}

func (ir fakeHTTPProxy) HTTPProxy() *projcontour_v1.HTTPProxy {
	status := "valid"
	if ir.invalid {
		status = "invalid"
	}

	var spec projcontour_v1.HTTPProxySpec
	if !ir.delegate {
		spec.VirtualHost = &projcontour_v1.VirtualHost{
			Fqdn: ir.host,
		}
	}

	lb := v1.LoadBalancerStatus{
		Ingress: []v1.LoadBalancerIngress{},
	}
	for _, ip := range ir.loadBalancer.ips {
		lb.Ingress = append(lb.Ingress, v1.LoadBalancerIngress{
			IP: ip,
		})
	}
	for _, hostname := range ir.loadBalancer.hostnames {
		lb.Ingress = append(lb.Ingress, v1.LoadBalancerIngress{
			Hostname: hostname,
		})
	}

	return &projcontour_v1.HTTPProxy{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:   ir.namespace,
			Name:        ir.name,
			Annotations: ir.annotations,
		},
		Spec: spec,
		Status: projcontour_v1.Status{
			CurrentStatus: status,
			LoadBalancer:  lb,
		},
	}
}
