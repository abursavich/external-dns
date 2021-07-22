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
	"fmt"
	"sort"

	informers "github.com/kong/kubernetes-ingress-controller/pkg/client/configuration/informers/externalversions"
	informers_v1b1 "github.com/kong/kubernetes-ingress-controller/pkg/client/configuration/informers/externalversions/configuration/v1beta1"
	kong_v1b1 "github.com/kong/kubernetes-ingress-controller/railgun/apis/configuration/v1beta1"
	log "github.com/sirupsen/logrus"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"

	"sigs.k8s.io/external-dns/endpoint"
)

// kongTCPIngressSource is an implementation of Source for Kong TCPIngress objects.
type kongTCPIngressSource struct {
	kubeClient         kubernetes.Interface
	tcpIngressInformer informers_v1b1.TCPIngressInformer

	namespace          string
	annotationSelector labels.Selector
}

// NewKongTCPIngressSource creates a new kongTCPIngressSource with the given config.
func NewKongTCPIngressSource(p ClientGenerator, config *Config) (Source, error) {
	annotationSelector, err := labels.Parse(config.AnnotationFilter)
	if err != nil {
		return nil, err
	}
	kubeClient, err := p.KubeClient()
	if err != nil {
		return nil, err
	}
	kongClient, err := p.KongClient()
	if err != nil {
		return nil, err
	}

	informerFactory := informers.NewSharedInformerFactoryWithOptions(kongClient, 0, informers.WithNamespace(config.Namespace))
	tcpIngressInformer := informerFactory.Configuration().V1beta1().TCPIngresses()
	tcpIngressInformer.Informer()
	informerFactory.Start(wait.NeverStop)

	// wait for the local cache to be populated.
	if err := waitForCacheSync(context.Background(), informerFactory); err != nil {
		return nil, err
	}

	return &kongTCPIngressSource{
		kubeClient:         kubeClient,
		tcpIngressInformer: tcpIngressInformer,
		namespace:          config.Namespace,
		annotationSelector: annotationSelector,
	}, nil
}

// Endpoints returns endpoint objects for each host-target combination that should be processed.
// Retrieves all TCPIngresses in the source's namespace(s).
func (sc *kongTCPIngressSource) Endpoints(ctx context.Context) ([]*endpoint.Endpoint, error) {
	tcpIngresses, err := sc.tcpIngressInformer.Lister().TCPIngresses(sc.namespace).List(labels.Everything())
	if err != nil {
		return nil, err
	}

	var endpoints []*endpoint.Endpoint
	for _, ti := range tcpIngresses {
		// Filter by annotations.
		if !sc.annotationSelector.Matches(labels.Set(ti.Annotations)) {
			continue
		}

		var targets endpoint.Targets
		for _, lb := range ti.Status.LoadBalancer.Ingress {
			if lb.IP != "" {
				targets = append(targets, lb.IP)
			}
			if lb.Hostname != "" {
				targets = append(targets, lb.Hostname)
			}
		}

		eps, err := sc.endpointsFromTCPIngress(ti, targets)
		if err != nil {
			return nil, err
		}
		if len(eps) == 0 {
			log.Debugf("No endpoints could be generated from Host %s/%s", ti.Namespace, ti.Name)
			continue
		}

		log.Debugf("Endpoints generated from TCPIngress: %s/%s: %v", ti.Namespace, ti.Name, eps)
		sc.setResourceLabel(ti, eps)
		sc.setDualstackLabel(ti, eps)
		endpoints = append(endpoints, eps...)
	}

	for _, ep := range endpoints {
		sort.Sort(ep.Targets)
	}

	return endpoints, nil
}

func (sc *kongTCPIngressSource) setResourceLabel(tcpIngress *kong_v1b1.TCPIngress, endpoints []*endpoint.Endpoint) {
	for _, ep := range endpoints {
		ep.Labels[endpoint.ResourceLabelKey] = fmt.Sprintf("tcpingress/%s/%s", tcpIngress.Namespace, tcpIngress.Name)
	}
}

func (sc *kongTCPIngressSource) setDualstackLabel(tcpIngress *kong_v1b1.TCPIngress, endpoints []*endpoint.Endpoint) {
	val, ok := tcpIngress.Annotations[ALBDualstackAnnotationKey]
	if ok && val == ALBDualstackAnnotationValue {
		log.Debugf("Adding dualstack label to TCPIngress %s/%s.", tcpIngress.Namespace, tcpIngress.Name)
		for _, ep := range endpoints {
			ep.Labels[endpoint.DualstackLabelKey] = "true"
		}
	}
}

// endpointsFromTCPIngress extracts the endpoints from a TCPIngress object
func (sc *kongTCPIngressSource) endpointsFromTCPIngress(tcpIngress *kong_v1b1.TCPIngress, targets endpoint.Targets) ([]*endpoint.Endpoint, error) {
	var endpoints []*endpoint.Endpoint

	ttl, err := getTTLFromAnnotations(tcpIngress.Annotations)
	if err != nil {
		return nil, err
	}

	providerSpecific, setIdentifier := getProviderSpecificAnnotations(tcpIngress.Annotations)

	hostnameList := getHostnamesFromAnnotations(tcpIngress.Annotations)
	for _, hostname := range hostnameList {
		endpoints = append(endpoints, endpointsForHostname(hostname, targets, ttl, providerSpecific, setIdentifier)...)
	}

	if tcpIngress.Spec.Rules != nil {
		for _, rule := range tcpIngress.Spec.Rules {
			if rule.Host != "" {
				endpoints = append(endpoints, endpointsForHostname(rule.Host, targets, ttl, providerSpecific, setIdentifier)...)
			}
		}
	}

	return endpoints, nil
}

func (sc *kongTCPIngressSource) AddEventHandler(ctx context.Context, handler func()) {
	log.Debug("Adding event handler for TCPIngress")

	// Right now there is no way to remove event handler from informer, see:
	// https://github.com/kubernetes/kubernetes/issues/79610
	sc.tcpIngressInformer.Informer().AddEventHandler(eventHandlerFunc(handler))
}
