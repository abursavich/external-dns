/*
Copyright 2017 The Kubernetes Authors.

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
	kubefake "k8s.io/client-go/kubernetes/fake"

	"sigs.k8s.io/external-dns/endpoint"
	contour_v1b1 "sigs.k8s.io/external-dns/third_party/projectcontour.io/apis/contour/v1beta1"
	projcontour "sigs.k8s.io/external-dns/third_party/projectcontour.io/apis/projectcontour/v1"
	contourfake "sigs.k8s.io/external-dns/third_party/projectcontour.io/clientset/versioned/fake"
)

// This is a compile-time validation that ingressRouteSource is a Source.
var _ Source = &ingressRouteSource{}

type IngressRouteSuite struct {
	suite.Suite
	source       Source
	loadBalancer *v1.Service
	ingressRoute *contour_v1b1.IngressRoute
}

func (suite *IngressRouteSuite) SetupTest() {
	ctx := context.Background()
	kubeClient, contourClient, clients := fakeContourClients()

	suite.loadBalancer = (fakeLoadBalancerService{
		ips:       []string{"8.8.8.8"},
		hostnames: []string{"v1"},
		namespace: "heptio-contour/contour",
		name:      "contour",
	}).Service()

	_, err := kubeClient.CoreV1().Services(suite.loadBalancer.Namespace).Create(ctx, suite.loadBalancer, metav1.CreateOptions{})
	suite.NoError(err, "should succeed")

	suite.ingressRoute = (fakeIngressRoute{
		name:      "foo-ingressroute-with-targets",
		namespace: "default",
		host:      "example.com",
	}).IngressRoute()
	_, err = contourClient.ContourV1beta1().IngressRoutes(suite.ingressRoute.Namespace).Create(ctx, suite.ingressRoute, metav1.CreateOptions{})
	suite.NoError(err, "should succeed")

	suite.source, err = NewContourIngressRouteSource(clients, &Config{
		ContourLoadBalancerService: "heptio-contour/contour",
		Namespace:                  "default",
		FQDNTemplate:               "{{.Name}}",
	})
	suite.NoError(err, "should initialize ingressroute source")
}

func (suite *IngressRouteSuite) TestResourceLabelIsSet() {
	endpoints, _ := suite.source.Endpoints(context.Background())
	for _, ep := range endpoints {
		suite.Equal("ingressroute/default/foo-ingressroute-with-targets", ep.Labels[endpoint.ResourceLabelKey], "should set correct resource label")
	}
}

func TestIngressRoute(t *testing.T) {
	t.Parallel()

	suite.Run(t, new(IngressRouteSuite))
	t.Run("endpointsFromIngressRoute", testEndpointsFromIngressRoute)
	t.Run("Endpoints", testIngressRouteEndpoints)
}

func TestNewContourIngressRouteSource(t *testing.T) {
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
			_, err := NewContourIngressRouteSource(clients, &Config{
				ContourLoadBalancerService: "heptio-contour/contour",
				AnnotationFilter:           ti.annotationFilter,
				FQDNTemplate:               ti.fqdnTemplate,
				CombineFQDNAndAnnotation:   ti.combineFQDNAndAnnotation,
			})
			if ti.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func testEndpointsFromIngressRoute(t *testing.T) {
	for _, ti := range []struct {
		title        string
		loadBalancer fakeLoadBalancerService
		ingressRoute fakeIngressRoute
		expected     []*endpoint.Endpoint
	}{
		{
			title: "one rule.host one lb.hostname",
			loadBalancer: fakeLoadBalancerService{
				hostnames: []string{"lb.com"}, // Kubernetes omits the trailing dot
			},
			ingressRoute: fakeIngressRoute{
				host: "foo.bar", // Kubernetes requires removal of trailing dot
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
			loadBalancer: fakeLoadBalancerService{
				ips: []string{"8.8.8.8"},
			},
			ingressRoute: fakeIngressRoute{
				host: "foo.bar",
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
			loadBalancer: fakeLoadBalancerService{
				ips:       []string{"8.8.8.8", "127.0.0.1"},
				hostnames: []string{"elb.com", "alb.com"},
			},
			ingressRoute: fakeIngressRoute{
				host: "foo.bar",
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
			loadBalancer: fakeLoadBalancerService{
				ips:       []string{"8.8.8.8", "127.0.0.1"},
				hostnames: []string{"elb.com", "alb.com"},
			},
			ingressRoute: fakeIngressRoute{},
			expected:     []*endpoint.Endpoint{},
		},
		{
			title: "one rule.host invalid ingressroute",
			loadBalancer: fakeLoadBalancerService{
				ips:       []string{"8.8.8.8", "127.0.0.1"},
				hostnames: []string{"elb.com", "alb.com"},
			},
			ingressRoute: fakeIngressRoute{
				host:    "foo.bar",
				invalid: true,
			},
			expected: []*endpoint.Endpoint{},
		},
		{
			title:        "no targets",
			loadBalancer: fakeLoadBalancerService{},
			ingressRoute: fakeIngressRoute{},
			expected:     []*endpoint.Endpoint{},
		},
		{
			title: "delegate ingressroute",
			loadBalancer: fakeLoadBalancerService{
				hostnames: []string{"lb.com"},
			},
			ingressRoute: fakeIngressRoute{
				delegate: true,
			},
			expected: []*endpoint.Endpoint{},
		},
	} {
		t.Run(ti.title, func(t *testing.T) {
			t.Parallel()

			ctx := context.Background()
			kubeClient, contourClient, clients := fakeContourClients()

			svc := ti.loadBalancer.Service()
			_, err := kubeClient.CoreV1().Services(svc.Namespace).Create(ctx, svc, metav1.CreateOptions{})
			require.NoError(t, err)

			rt := ti.ingressRoute.IngressRoute()
			_, err = contourClient.ContourV1beta1().IngressRoutes(rt.Namespace).Create(ctx, rt, metav1.CreateOptions{})
			require.NoError(t, err)

			src, err := NewContourIngressRouteSource(clients, &Config{
				ContourLoadBalancerService: svc.Namespace + "/" + svc.Name,
				Namespace:                  "default",
				FQDNTemplate:               "{{.Name}}",
			})
			require.NoError(t, err)

			endpoints, err := src.Endpoints(ctx)
			require.NoError(t, err)
			validateEndpoints(t, endpoints, ti.expected)
		})
	}
}

func testIngressRouteEndpoints(t *testing.T) {
	t.Parallel()

	namespace := "testing"
	for _, ti := range []struct {
		title                    string
		targetNamespace          string
		annotationFilter         string
		loadBalancer             fakeLoadBalancerService
		ingressRouteItems        []fakeIngressRoute
		expected                 []*endpoint.Endpoint
		expectError              bool
		fqdnTemplate             string
		combineFQDNAndAnnotation bool
		ignoreHostnameAnnotation bool
	}{
		{
			title:           "no ingressroute",
			targetNamespace: "",
		},
		{
			title:           "two simple ingressroutes",
			targetNamespace: "",
			loadBalancer: fakeLoadBalancerService{
				name:      "envoy",
				namespace: "test-contour",
				ips:       []string{"8.8.8.8"},
				hostnames: []string{"lb.com"},
			},
			ingressRouteItems: []fakeIngressRoute{
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
			title:           "two simple ingressroutes on different namespaces",
			targetNamespace: "",
			loadBalancer: fakeLoadBalancerService{
				name:      "envoy",
				namespace: "test-contour",
				ips:       []string{"8.8.8.8"},
				hostnames: []string{"lb.com"},
			},
			ingressRouteItems: []fakeIngressRoute{
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
			title:           "two simple ingressroutes on different namespaces and a target namespace",
			targetNamespace: "testing1",
			loadBalancer: fakeLoadBalancerService{
				name:      "envoy",
				namespace: "test-contour",
				ips:       []string{"8.8.8.8"},
				hostnames: []string{"lb.com"},
			},
			ingressRouteItems: []fakeIngressRoute{
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
				name:      "envoy",
				namespace: "test-contour",
				ips:       []string{"8.8.8.8"},
			},
			ingressRouteItems: []fakeIngressRoute{
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
				name:      "envoy",
				namespace: "test-contour",
				ips:       []string{"8.8.8.8"},
			},
			ingressRouteItems: []fakeIngressRoute{
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
				name:      "envoy",
				namespace: "test-contour",
				ips:       []string{"8.8.8.8"},
			},
			ingressRouteItems: []fakeIngressRoute{
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
				name:      "envoy",
				namespace: "test-contour",
				ips:       []string{"8.8.8.8"},
			},
			ingressRouteItems: []fakeIngressRoute{
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
				name:      "envoy",
				namespace: "test-contour",
				ips:       []string{"8.8.8.8"},
			},
			ingressRouteItems: []fakeIngressRoute{
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
				name:      "envoy",
				namespace: "test-contour",
				ips:       []string{"8.8.8.8"},
			},
			ingressRouteItems: []fakeIngressRoute{
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
				name:      "envoy",
				namespace: "test-contour",
				ips:       []string{"8.8.8.8"},
			},
			ingressRouteItems: []fakeIngressRoute{
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
			title:           "template for ingressroute if host is missing",
			targetNamespace: "",
			loadBalancer: fakeLoadBalancerService{
				name:      "envoy",
				namespace: "test-contour",
				ips:       []string{"8.8.8.8"},
				hostnames: []string{"elb.com"},
			},
			ingressRouteItems: []fakeIngressRoute{
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
				name:      "envoy",
				namespace: "test-contour",
				ips:       []string{"8.8.8.8"},
			},
			ingressRouteItems: []fakeIngressRoute{
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
				name:      "envoy",
				namespace: "test-contour",
				ips:       []string{"8.8.8.8"},
			},
			ingressRouteItems: []fakeIngressRoute{
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
				name:      "envoy",
				namespace: "test-contour",
				ips:       []string{"8.8.8.8"},
			},
			ingressRouteItems: []fakeIngressRoute{
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
						targetAnnotationKey: "ingressroute-target.com",
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
					Targets:    endpoint.Targets{"ingressroute-target.com"},
					RecordType: endpoint.RecordTypeCNAME,
				},
				{
					DNSName:    "fake2.ext-dns.test.com",
					Targets:    endpoint.Targets{"ingressroute-target.com"},
					RecordType: endpoint.RecordTypeCNAME,
				},
				{
					DNSName:    "fake2.ext-dna.test.com",
					Targets:    endpoint.Targets{"ingressroute-target.com"},
					RecordType: endpoint.RecordTypeCNAME,
				},
			},
			fqdnTemplate:             "{{.Name}}.ext-dns.test.com, {{.Name}}.ext-dna.test.com",
			combineFQDNAndAnnotation: true,
		},
		{
			title:           "ingressroute rules with annotation",
			targetNamespace: "",
			loadBalancer: fakeLoadBalancerService{
				name:      "envoy",
				namespace: "test-contour",
				ips:       []string{"8.8.8.8"},
			},
			ingressRouteItems: []fakeIngressRoute{
				{
					name:      "fake1",
					namespace: namespace,
					annotations: map[string]string{
						targetAnnotationKey: "ingressroute-target.com",
					},
					host: "example.org",
				},
				{
					name:      "fake2",
					namespace: namespace,
					annotations: map[string]string{
						targetAnnotationKey: "ingressroute-target.com",
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
					Targets:    endpoint.Targets{"ingressroute-target.com"},
					RecordType: endpoint.RecordTypeCNAME,
				},
				{
					DNSName:    "example2.org",
					Targets:    endpoint.Targets{"ingressroute-target.com"},
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
			title:           "ingressroute rules with hostname annotation",
			targetNamespace: "",
			loadBalancer: fakeLoadBalancerService{
				name:      "envoy",
				namespace: "test-contour",
				ips:       []string{"1.2.3.4"},
			},
			ingressRouteItems: []fakeIngressRoute{
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
			title:           "ingressroute rules with hostname annotation having multiple hostnames",
			targetNamespace: "",
			loadBalancer: fakeLoadBalancerService{
				name:      "envoy",
				namespace: "test-contour",
				ips:       []string{"1.2.3.4"},
			},
			ingressRouteItems: []fakeIngressRoute{
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
			title:           "ingressroute rules with hostname and target annotation",
			targetNamespace: "",
			loadBalancer: fakeLoadBalancerService{
				ips: []string{},
			},
			ingressRouteItems: []fakeIngressRoute{
				{
					name:      "fake1",
					namespace: namespace,
					annotations: map[string]string{
						hostnameAnnotationKey: "dns-through-hostname.com",
						targetAnnotationKey:   "ingressroute-target.com",
					},
					host: "example.org",
				},
			},
			expected: []*endpoint.Endpoint{
				{
					DNSName:    "example.org",
					Targets:    endpoint.Targets{"ingressroute-target.com"},
					RecordType: endpoint.RecordTypeCNAME,
				},
				{
					DNSName:    "dns-through-hostname.com",
					Targets:    endpoint.Targets{"ingressroute-target.com"},
					RecordType: endpoint.RecordTypeCNAME,
				},
			},
		},
		{
			title:           "ingressroute rules with annotation and custom TTL",
			targetNamespace: "",
			loadBalancer: fakeLoadBalancerService{
				name:      "envoy",
				namespace: "test-contour",
				ips:       []string{"8.8.8.8"},
			},
			ingressRouteItems: []fakeIngressRoute{
				{
					name:      "fake1",
					namespace: namespace,
					annotations: map[string]string{
						targetAnnotationKey: "ingressroute-target.com",
						ttlAnnotationKey:    "6",
					},
					host: "example.org",
				},
				{
					name:      "fake2",
					namespace: namespace,
					annotations: map[string]string{
						targetAnnotationKey: "ingressroute-target.com",
						ttlAnnotationKey:    "1",
					},
					host: "example2.org",
				},
				{
					name:      "fake3",
					namespace: namespace,
					annotations: map[string]string{
						targetAnnotationKey: "ingressroute-target.com",
						ttlAnnotationKey:    "10s",
					},
					host: "example3.org",
				},
			},
			expected: []*endpoint.Endpoint{
				{
					DNSName:   "example.org",
					Targets:   endpoint.Targets{"ingressroute-target.com"},
					RecordTTL: endpoint.TTL(6),
				},
				{
					DNSName:   "example2.org",
					Targets:   endpoint.Targets{"ingressroute-target.com"},
					RecordTTL: endpoint.TTL(1),
				},
				{
					DNSName:   "example3.org",
					Targets:   endpoint.Targets{"ingressroute-target.com"},
					RecordTTL: endpoint.TTL(10),
				},
			},
		},
		{
			title:           "template for ingressroute with annotation",
			targetNamespace: "",
			loadBalancer: fakeLoadBalancerService{
				ips:       []string{},
				hostnames: []string{},
			},
			ingressRouteItems: []fakeIngressRoute{
				{
					name:      "fake1",
					namespace: namespace,
					annotations: map[string]string{
						targetAnnotationKey: "ingressroute-target.com",
					},
					host: "",
				},
				{
					name:      "fake2",
					namespace: namespace,
					annotations: map[string]string{
						targetAnnotationKey: "ingressroute-target.com",
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
					Targets:    endpoint.Targets{"ingressroute-target.com"},
					RecordType: endpoint.RecordTypeCNAME,
				},
				{
					DNSName:    "fake2.ext-dns.test.com",
					Targets:    endpoint.Targets{"ingressroute-target.com"},
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
			title:           "ingressroute with empty annotation",
			targetNamespace: "",
			loadBalancer: fakeLoadBalancerService{
				ips:       []string{},
				hostnames: []string{},
			},
			ingressRouteItems: []fakeIngressRoute{
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
			targetNamespace: namespace,
			loadBalancer: fakeLoadBalancerService{
				name:      "envoy",
				namespace: "test-contour",
				ips:       []string{"8.8.8.8"},
				hostnames: []string{"lb.com"},
			},
			ingressRouteItems: []fakeIngressRoute{
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
			kubeClient, contourClient, clients := fakeContourClients()

			for _, item := range ti.ingressRouteItems {
				route := item.IngressRoute()
				_, err := contourClient.ContourV1beta1().IngressRoutes(route.Namespace).Create(ctx, route, metav1.CreateOptions{})
				require.NoError(t, err)
			}

			svc := ti.loadBalancer.Service()
			_, err := kubeClient.CoreV1().Services(svc.Namespace).Create(ctx, svc, metav1.CreateOptions{})
			require.NoError(t, err)

			src, err := NewContourIngressRouteSource(clients, &Config{
				ContourLoadBalancerService: svc.Namespace + "/" + svc.Name,
				Namespace:                  ti.targetNamespace,
				AnnotationFilter:           ti.annotationFilter,
				FQDNTemplate:               ti.fqdnTemplate,
				CombineFQDNAndAnnotation:   ti.combineFQDNAndAnnotation,
				IgnoreHostnameAnnotation:   ti.ignoreHostnameAnnotation,
			})
			require.NoError(t, err)

			res, err := src.Endpoints(ctx)
			if ti.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
			validateEndpoints(t, res, ti.expected)
		})
	}
}

func fakeContourClients() (*kubefake.Clientset, *contourfake.Clientset, ClientGenerator) {
	kubeClient := kubefake.NewSimpleClientset()
	contourClient := contourfake.NewSimpleClientset()

	clients := new(MockClientGenerator)
	clients.On("KubeClient").Return(kubeClient, nil)
	clients.On("ContourClient").Return(contourClient, nil)

	return kubeClient, contourClient, clients
}

type fakeLoadBalancerService struct {
	ips       []string
	hostnames []string
	namespace string
	name      string
}

func (ig fakeLoadBalancerService) Service() *v1.Service {
	svc := &v1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: ig.namespace,
			Name:      ig.name,
		},
		Status: v1.ServiceStatus{
			LoadBalancer: v1.LoadBalancerStatus{
				Ingress: []v1.LoadBalancerIngress{},
			},
		},
	}

	for _, ip := range ig.ips {
		svc.Status.LoadBalancer.Ingress = append(svc.Status.LoadBalancer.Ingress, v1.LoadBalancerIngress{
			IP: ip,
		})
	}
	for _, hostname := range ig.hostnames {
		svc.Status.LoadBalancer.Ingress = append(svc.Status.LoadBalancer.Ingress, v1.LoadBalancerIngress{
			Hostname: hostname,
		})
	}
	return svc
}

type fakeIngressRoute struct {
	namespace   string
	name        string
	annotations map[string]string

	host     string
	invalid  bool
	delegate bool
}

func (ir fakeIngressRoute) IngressRoute() *contour_v1b1.IngressRoute {
	status := "valid"
	if ir.invalid {
		status = "invalid"
	}

	var spec contour_v1b1.IngressRouteSpec
	if !ir.delegate {
		spec.VirtualHost = &contour_v1b1.VirtualHost{
			Fqdn: ir.host,
		}
	}

	return &contour_v1b1.IngressRoute{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:   ir.namespace,
			Name:        ir.name,
			Annotations: ir.annotations,
		},
		Spec: spec,
		Status: projcontour.Status{
			CurrentStatus: status,
		},
	}
}
