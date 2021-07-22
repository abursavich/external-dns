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
	"bytes"
	"context"
	"fmt"
	"sort"
	"strings"
	"text/template"

	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/wait"

	"sigs.k8s.io/external-dns/endpoint"
	projcontour_v1 "sigs.k8s.io/external-dns/third_party/projectcontour.io/apis/projectcontour/v1"
	informers "sigs.k8s.io/external-dns/third_party/projectcontour.io/informers/externalversions"
	informers_v1 "sigs.k8s.io/external-dns/third_party/projectcontour.io/informers/externalversions/projectcontour/v1"
)

// HTTPProxySource is an implementation of Source for ProjectContour HTTPProxy objects.
// The HTTPProxy implementation uses the spec.virtualHost.fqdn value for the hostname.
// Use targetAnnotationKey to explicitly set Endpoint.
type httpProxySource struct {
	httpProxyInformer informers_v1.HTTPProxyInformer

	namespace                string
	fqdnTemplate             *template.Template
	annotationSelector       labels.Selector
	combineFQDNAnnotation    bool
	ignoreHostnameAnnotation bool
}

// NewContourHTTPProxySource creates a new contourHTTPProxySource with the given config.
func NewContourHTTPProxySource(clients ClientGenerator, config *Config) (Source, error) {
	tmpl, err := parseTemplate(config.FQDNTemplate)
	if err != nil {
		return nil, err
	}
	annotationSelector, err := labels.Parse(config.AnnotationFilter)
	if err != nil {
		return nil, err
	}

	contourClient, err := clients.ContourClient()
	if err != nil {
		return nil, err
	}
	informerFactory := informers.NewSharedInformerFactoryWithOptions(contourClient, 0, informers.WithNamespace(config.Namespace))
	httpProxyInformer := informerFactory.Projectcontour().V1().HTTPProxies()
	httpProxyInformer.Informer() // Register with factory before starting
	informerFactory.Start(wait.NeverStop)

	if err := waitForCacheSync(context.Background(), informerFactory); err != nil {
		return nil, err
	}

	return &httpProxySource{
		httpProxyInformer:        httpProxyInformer,
		fqdnTemplate:             tmpl,
		annotationSelector:       annotationSelector,
		combineFQDNAnnotation:    config.CombineFQDNAndAnnotation,
		ignoreHostnameAnnotation: config.IgnoreHostnameAnnotation,
	}, nil
}

// Endpoints returns endpoint objects for each host-target combination that should be processed.
// Retrieves all HTTPProxy resources in the source's namespace(s).
func (sc *httpProxySource) Endpoints(ctx context.Context) ([]*endpoint.Endpoint, error) {
	httpProxies, err := sc.httpProxyInformer.Lister().HTTPProxies(sc.namespace).List(labels.Everything())
	if err != nil {
		return nil, err
	}

	var endpoints []*endpoint.Endpoint
	for _, hp := range httpProxies {
		// Filter by annotations.
		if !sc.annotationSelector.Matches(labels.Set(hp.Annotations)) {
			continue
		}
		// Check controller annotation to see if we are responsible.
		if controller, ok := hp.Annotations[controllerAnnotationKey]; ok && controller != controllerAnnotationValue {
			log.Debugf("Skipping HTTPProxy %s/%s because controller value does not match, found: %s, required: %s",
				hp.Namespace, hp.Name, controller, controllerAnnotationValue)
			continue
		}
		// Skip invalid ingress routes.
		if hp.Status.CurrentStatus != "valid" {
			log.Debugf("Skipping HTTPProxy %s/%s because it is not valid", hp.Namespace, hp.Name)
			continue
		}

		eps, err := sc.endpointsFromHTTPProxy(hp)
		if err != nil {
			return nil, errors.Wrap(err, "failed to get endpoints from HTTPProxy")
		}
		// Apply template if fqdn is missing on HTTPProxy.
		if (sc.combineFQDNAnnotation || len(eps) == 0) && sc.fqdnTemplate != nil {
			tmplEndpoints, err := sc.endpointsFromTemplate(hp)
			if err != nil {
				return nil, errors.Wrap(err, "failed to get endpoints from template")
			}

			if sc.combineFQDNAnnotation {
				eps = append(eps, tmplEndpoints...)
			} else {
				eps = tmplEndpoints
			}
		}
		if len(eps) == 0 {
			log.Debugf("No endpoints could be generated from HTTPProxy %s/%s", hp.Namespace, hp.Name)
			continue
		}

		log.Debugf("Endpoints generated from HTTPProxy: %s/%s: %v", hp.Namespace, hp.Name, eps)
		sc.setResourceLabel(hp, eps)
		endpoints = append(endpoints, eps...)
	}

	for _, ep := range endpoints {
		sort.Sort(ep.Targets)
	}

	return endpoints, nil
}

func (sc *httpProxySource) endpointsFromTemplate(httpProxy *projcontour_v1.HTTPProxy) ([]*endpoint.Endpoint, error) {
	// Process the whole template string
	var buf bytes.Buffer
	err := sc.fqdnTemplate.Execute(&buf, httpProxy)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to apply template on HTTPProxy %s/%s", httpProxy.Namespace, httpProxy.Name)
	}

	hostnames := buf.String()

	ttl, err := getTTLFromAnnotations(httpProxy.Annotations)
	if err != nil {
		log.Warn(err)
	}

	targets := getTargetsFromTargetAnnotation(httpProxy.Annotations)

	if len(targets) == 0 {
		for _, lb := range httpProxy.Status.LoadBalancer.Ingress {
			if lb.IP != "" {
				targets = append(targets, lb.IP)
			}
			if lb.Hostname != "" {
				targets = append(targets, lb.Hostname)
			}
		}
	}

	providerSpecific, setIdentifier := getProviderSpecificAnnotations(httpProxy.Annotations)

	var endpoints []*endpoint.Endpoint
	// splits the FQDN template and removes the trailing periods
	hostnameList := strings.Split(strings.Replace(hostnames, " ", "", -1), ",")
	for _, hostname := range hostnameList {
		hostname = strings.TrimSuffix(hostname, ".")
		endpoints = append(endpoints, endpointsForHostname(hostname, targets, ttl, providerSpecific, setIdentifier)...)
	}
	return endpoints, nil
}

func (sc *httpProxySource) setResourceLabel(httpProxy *projcontour_v1.HTTPProxy, endpoints []*endpoint.Endpoint) {
	for _, ep := range endpoints {
		ep.Labels[endpoint.ResourceLabelKey] = fmt.Sprintf("HTTPProxy/%s/%s", httpProxy.Namespace, httpProxy.Name)
	}
}

// endpointsFromHTTPProxyConfig extracts the endpoints from a Contour HTTPProxy object
func (sc *httpProxySource) endpointsFromHTTPProxy(httpProxy *projcontour_v1.HTTPProxy) ([]*endpoint.Endpoint, error) {
	if httpProxy.Status.CurrentStatus != "valid" {
		log.Warn(errors.Errorf("cannot generate endpoints for HTTPProxy with status %s", httpProxy.Status.CurrentStatus))
		return nil, nil
	}

	var endpoints []*endpoint.Endpoint

	ttl, err := getTTLFromAnnotations(httpProxy.Annotations)
	if err != nil {
		log.Warn(err)
	}

	targets := getTargetsFromTargetAnnotation(httpProxy.Annotations)

	if len(targets) == 0 {
		for _, lb := range httpProxy.Status.LoadBalancer.Ingress {
			if lb.IP != "" {
				targets = append(targets, lb.IP)
			}
			if lb.Hostname != "" {
				targets = append(targets, lb.Hostname)
			}
		}
	}

	providerSpecific, setIdentifier := getProviderSpecificAnnotations(httpProxy.Annotations)

	if virtualHost := httpProxy.Spec.VirtualHost; virtualHost != nil {
		if fqdn := virtualHost.Fqdn; fqdn != "" {
			endpoints = append(endpoints, endpointsForHostname(fqdn, targets, ttl, providerSpecific, setIdentifier)...)
		}
	}

	// Skip endpoints if we do not want entries from annotations
	if !sc.ignoreHostnameAnnotation {
		hostnameList := getHostnamesFromAnnotations(httpProxy.Annotations)
		for _, hostname := range hostnameList {
			endpoints = append(endpoints, endpointsForHostname(hostname, targets, ttl, providerSpecific, setIdentifier)...)
		}
	}

	return endpoints, nil
}

func (sc *httpProxySource) AddEventHandler(ctx context.Context, handler func()) {
	log.Debug("Adding event handler for httpproxy")

	// Right now there is no way to remove event handler from informer, see:
	// https://github.com/kubernetes/kubernetes/issues/79610
	sc.httpProxyInformer.Informer().AddEventHandler(eventHandlerFunc(handler))
}
