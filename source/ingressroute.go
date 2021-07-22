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
	"bytes"
	"context"
	"fmt"
	"sort"
	"strings"
	"text/template"

	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	kubeinformers "k8s.io/client-go/informers"
	kubeinformers_corev1 "k8s.io/client-go/informers/core/v1"

	"sigs.k8s.io/external-dns/endpoint"
	contour_v1b1 "sigs.k8s.io/external-dns/third_party/projectcontour.io/apis/contour/v1beta1"
	contourinformers "sigs.k8s.io/external-dns/third_party/projectcontour.io/informers/externalversions"
	contourinformers_v1b1 "sigs.k8s.io/external-dns/third_party/projectcontour.io/informers/externalversions/contour/v1beta1"
)

// ingressRouteSource is an implementation of Source for ProjectContour IngressRoute objects.
// The IngressRoute implementation uses the spec.virtualHost.fqdn value for the hostname.
// Use targetAnnotationKey to explicitly set Endpoint.
type ingressRouteSource struct {
	serviceInformer      kubeinformers_corev1.ServiceInformer
	ingressRouteInformer contourinformers_v1b1.IngressRouteInformer

	namespace                string
	lbService                types.NamespacedName
	fqdnTemplate             *template.Template
	annotationSelector       labels.Selector
	combineFQDNAnnotation    bool
	ignoreHostnameAnnotation bool
}

// NewContourIngressRouteSource creates a new contourIngressRouteSource with the given config.
func NewContourIngressRouteSource(clients ClientGenerator, config *Config) (Source, error) {
	tmpl, err := parseTemplate(config.FQDNTemplate)
	if err != nil {
		return nil, err
	}
	lbNamespace, lbName, err := parseContourLoadBalancerService(config.ContourLoadBalancerService)
	if err != nil {
		return nil, err
	}
	annotationSelector, err := labels.Parse(config.AnnotationFilter)
	if err != nil {
		return nil, err
	}

	kubeClient, err := clients.KubeClient()
	if err != nil {
		return nil, err
	}
	contourClient, err := clients.ContourClient()
	if err != nil {
		return nil, err
	}

	kubeInformerFactory := kubeinformers.NewSharedInformerFactoryWithOptions(kubeClient, 0, kubeinformers.WithNamespace(lbNamespace))
	serviceInformer := kubeInformerFactory.Core().V1().Services()
	serviceInformer.Informer() // Register with factory before starting
	kubeInformerFactory.Start(wait.NeverStop)

	contourInformerFactory := contourinformers.NewSharedInformerFactoryWithOptions(contourClient, 0, contourinformers.WithNamespace(config.Namespace))
	ingressRouteInformer := contourInformerFactory.Contour().V1beta1().IngressRoutes()
	ingressRouteInformer.Informer() // Register with factory before starting
	contourInformerFactory.Start(wait.NeverStop)

	// Wait for the local cache to be populated.
	if err := waitForCacheSync(context.Background(), kubeInformerFactory); err != nil {
		return nil, err
	}
	if err := waitForCacheSync(context.Background(), contourInformerFactory); err != nil {
		return nil, err
	}

	return &ingressRouteSource{
		serviceInformer:      serviceInformer,
		ingressRouteInformer: ingressRouteInformer,
		lbService: types.NamespacedName{
			Namespace: lbNamespace,
			Name:      lbName,
		},
		fqdnTemplate:             tmpl,
		annotationSelector:       annotationSelector,
		combineFQDNAnnotation:    config.CombineFQDNAndAnnotation,
		ignoreHostnameAnnotation: config.IgnoreHostnameAnnotation,
	}, nil
}

func (sc *ingressRouteSource) AddEventHandler(ctx context.Context, handler func()) {
	sc.ingressRouteInformer.Informer().AddEventHandler(eventHandlerFunc(handler))
	sc.serviceInformer.Informer().AddEventHandler(eventHandlerFunc(handler))
}

// Endpoints returns endpoint objects for each host-target combination that should be processed.
// Retrieves all ingressroute resources in the source's namespace(s).
func (sc *ingressRouteSource) Endpoints(ctx context.Context) ([]*endpoint.Endpoint, error) {
	routes, err := sc.ingressRouteInformer.Lister().IngressRoutes(sc.namespace).List(labels.Everything())
	if err != nil {
		return nil, err
	}

	var endpoints []*endpoint.Endpoint
	for _, rt := range routes {
		// Filter by annotations.
		if !sc.annotationSelector.Matches(labels.Set(rt.Annotations)) {
			continue
		}
		// Check controller annotation to see if we are responsible.
		if controller, ok := rt.Annotations[controllerAnnotationKey]; ok && controller != controllerAnnotationValue {
			log.Debugf("Skipping ingressroute %s/%s because controller value does not match, found: %s, required: %s",
				rt.Namespace, rt.Name, controller, controllerAnnotationValue)
			continue
		}
		// Skip invalid ingress routes.
		if rt.CurrentStatus != "valid" {
			log.Debugf("Skipping ingressroute %s/%s because it is not valid", rt.Namespace, rt.Name)
			continue
		}

		eps, err := sc.endpointsFromIngressRoute(ctx, rt)
		if err != nil {
			return nil, err
		}
		// Apply template if fqdn is missing on IngressRoute.
		if (sc.combineFQDNAnnotation || len(eps) == 0) && sc.fqdnTemplate != nil {
			tmplEndpoints, err := sc.endpointsFromTemplate(ctx, rt)
			if err != nil {
				return nil, err
			}

			if sc.combineFQDNAnnotation {
				eps = append(eps, tmplEndpoints...)
			} else {
				eps = tmplEndpoints
			}
		}
		if len(eps) == 0 {
			log.Debugf("No endpoints could be generated from ingressroute %s/%s", rt.Namespace, rt.Name)
			continue
		}

		log.Debugf("Endpoints generated from ingressroute: %s/%s: %v", rt.Namespace, rt.Name, eps)
		sc.setResourceLabel(rt, eps)
		endpoints = append(endpoints, eps...)
	}

	for _, ep := range endpoints {
		sort.Sort(ep.Targets)
	}

	return endpoints, nil
}

func (sc *ingressRouteSource) endpointsFromTemplate(ctx context.Context, ingressRoute *contour_v1b1.IngressRoute) ([]*endpoint.Endpoint, error) {
	// Process the whole template string
	var buf bytes.Buffer
	err := sc.fqdnTemplate.Execute(&buf, ingressRoute)
	if err != nil {
		return nil, fmt.Errorf("failed to apply template on ingressroute %s/%s: %v", ingressRoute.Namespace, ingressRoute.Name, err)
	}

	hostnames := buf.String()

	ttl, err := getTTLFromAnnotations(ingressRoute.Annotations)
	if err != nil {
		log.Warn(err)
	}

	targets := getTargetsFromTargetAnnotation(ingressRoute.Annotations)

	if len(targets) == 0 {
		targets, err = sc.targetsFromContourLoadBalancer(ctx)
		if err != nil {
			return nil, err
		}
	}

	providerSpecific, setIdentifier := getProviderSpecificAnnotations(ingressRoute.Annotations)

	var endpoints []*endpoint.Endpoint
	// splits the FQDN template and removes the trailing periods
	hostnameList := strings.Split(strings.Replace(hostnames, " ", "", -1), ",")
	for _, hostname := range hostnameList {
		hostname = strings.TrimSuffix(hostname, ".")
		endpoints = append(endpoints, endpointsForHostname(hostname, targets, ttl, providerSpecific, setIdentifier)...)
	}
	return endpoints, nil
}

func (sc *ingressRouteSource) setResourceLabel(ingressRoute *contour_v1b1.IngressRoute, endpoints []*endpoint.Endpoint) {
	for _, ep := range endpoints {
		ep.Labels[endpoint.ResourceLabelKey] = fmt.Sprintf("ingressroute/%s/%s", ingressRoute.Namespace, ingressRoute.Name)
	}
}

func (sc *ingressRouteSource) targetsFromContourLoadBalancer(ctx context.Context) (targets endpoint.Targets, err error) {
	svc, err := sc.serviceInformer.Lister().Services(sc.lbService.Namespace).Get(sc.lbService.Name)
	if err != nil {
		log.Warn(err)
		return nil, nil
	}
	for _, lb := range svc.Status.LoadBalancer.Ingress {
		if lb.IP != "" {
			targets = append(targets, lb.IP)
		}
		if lb.Hostname != "" {
			targets = append(targets, lb.Hostname)
		}
	}
	return targets, nil
}

// endpointsFromIngressRouteConfig extracts the endpoints from a Contour IngressRoute object
func (sc *ingressRouteSource) endpointsFromIngressRoute(ctx context.Context, route *contour_v1b1.IngressRoute) ([]*endpoint.Endpoint, error) {
	if route.CurrentStatus != "valid" {
		log.Warn(errors.Errorf("cannot generate endpoints for ingressroute with status %s", route.CurrentStatus))
		return nil, nil
	}

	var endpoints []*endpoint.Endpoint

	ttl, err := getTTLFromAnnotations(route.Annotations)
	if err != nil {
		log.Warn(err)
	}

	targets := getTargetsFromTargetAnnotation(route.Annotations)

	if len(targets) == 0 {
		targets, err = sc.targetsFromContourLoadBalancer(ctx)
		if err != nil {
			return nil, err
		}
	}

	providerSpecific, setIdentifier := getProviderSpecificAnnotations(route.Annotations)

	if virtualHost := route.Spec.VirtualHost; virtualHost != nil {
		if fqdn := virtualHost.Fqdn; fqdn != "" {
			endpoints = append(endpoints, endpointsForHostname(fqdn, targets, ttl, providerSpecific, setIdentifier)...)
		}
	}

	// Skip endpoints if we do not want entries from annotations
	if !sc.ignoreHostnameAnnotation {
		hostnameList := getHostnamesFromAnnotations(route.Annotations)
		for _, hostname := range hostnameList {
			endpoints = append(endpoints, endpointsForHostname(hostname, targets, ttl, providerSpecific, setIdentifier)...)
		}
	}

	return endpoints, nil
}

func parseContourLoadBalancerService(service string) (namespace, name string, err error) {
	parts := strings.Split(service, "/")
	if len(parts) != 2 {
		err = fmt.Errorf("invalid contour load balancer service (namespace/name) found '%v'", service)
	} else {
		namespace, name = parts[0], parts[1]
	}

	return
}
