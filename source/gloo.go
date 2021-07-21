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
	"strings"

	log "github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"

	"sigs.k8s.io/external-dns/endpoint"
	gloov1 "sigs.k8s.io/external-dns/third_party/solo.io/apis/gloo/v1"
	gloo "sigs.k8s.io/external-dns/third_party/solo.io/clientset/versioned"
	informers "sigs.k8s.io/external-dns/third_party/solo.io/informers/externalversions"
	informers_v1 "sigs.k8s.io/external-dns/third_party/solo.io/informers/externalversions/gloo/v1"
)

type glooSource struct {
	kubeClient kubernetes.Interface
	glooClient gloo.Interface

	proxyInformer informers_v1.ProxyInformer
	namespace     string
}

// NewGlooSource creates a new Gloo Source with the given clients and config.
func NewGlooSource(clients ClientGenerator, config *Config) (Source, error) {
	kubeClient, err := clients.KubeClient()
	if err != nil {
		return nil, err
	}
	glooClient, err := clients.GlooClient()
	if err != nil {
		return nil, err
	}

	informerFactory := informers.NewSharedInformerFactoryWithOptions(glooClient, 0, informers.WithNamespace(config.GlooNamespace))
	proxyInformer := informerFactory.Gateway().V1().Proxies()

	// Register informers with factory before starting.
	proxyInformer.Informer()
	informerFactory.Start(wait.NeverStop)

	if err := waitForCacheSync(context.Background(), informerFactory); err != nil {
		return nil, err
	}

	return &glooSource{
		kubeClient:    kubeClient,
		glooClient:    glooClient,
		proxyInformer: proxyInformer,
		namespace:     config.GlooNamespace,
	}, nil
}

func (gs *glooSource) AddEventHandler(ctx context.Context, handler func()) {
	gs.proxyInformer.Informer().AddEventHandler(eventHandlerFunc(handler))
}

// Endpoints returns endpoint objects.
func (gs *glooSource) Endpoints(ctx context.Context) ([]*endpoint.Endpoint, error) {
	proxies, err := gs.proxyInformer.Lister().Proxies(gs.namespace).List(labels.Everything())
	if err != nil {
		return nil, err
	}
	var endpoints []*endpoint.Endpoint
	for _, p := range proxies {
		log.Debugf("Gloo: Find %s proxy", p.Name)
		targets, err := gs.proxyTargets(ctx, p.Name)
		if err != nil {
			return nil, err
		}
		log.Debugf("Gloo[%s]: Find %d target(s) (%+v)", p.Name, len(targets), targets)
		eps, err := gs.endpointsFromProxy(ctx, p, targets)
		if err != nil {
			return nil, err
		}
		log.Debugf("Gloo[%s]: Generate %d endpoint(s)", p.Name, len(eps))
		endpoints = append(endpoints, eps...)
	}
	return endpoints, nil
}

func (gs *glooSource) endpointsFromProxy(ctx context.Context, proxy *gloov1.Proxy, targets endpoint.Targets) ([]*endpoint.Endpoint, error) {
	var endpoints []*endpoint.Endpoint
	for _, listener := range proxy.Spec.Listeners {
		for _, vhost := range listener.HTTPListener.VirtualHosts {
			annotations, err := gs.annotationsFromVirtualHost(ctx, &vhost)
			if err != nil {
				return nil, err
			}
			ttl, err := getTTLFromAnnotations(annotations)
			if err != nil {
				return nil, err
			}
			providerSpecific, setIdentifier := getProviderSpecificAnnotations(annotations)
			for _, domain := range vhost.Domains {
				endpoints = append(endpoints, endpointsForHostname(strings.TrimSuffix(domain, "."), targets, ttl, providerSpecific, setIdentifier)...)
			}
		}
	}
	return endpoints, nil
}

func (gs *glooSource) annotationsFromVirtualHost(ctx context.Context, vhost *gloov1.VirtualHost) (map[string]string, error) {
	annotations := map[string]string{}
	for _, src := range vhost.Metadata.Source {
		if src.Kind == "*v1.VirtualService" {
			svc, err := gs.glooClient.GatewayV1().VirtualServices(src.Namespace).Get(ctx, src.Name, metav1.GetOptions{})
			if err != nil {
				return nil, err
			}
			for key, value := range svc.GetAnnotations() {
				annotations[key] = value
			}
		}
	}
	return annotations, nil
}

func (gs *glooSource) proxyTargets(ctx context.Context, name string) (endpoint.Targets, error) {
	svc, err := gs.kubeClient.CoreV1().Services(gs.namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	var targets endpoint.Targets
	switch svc.Spec.Type {
	case corev1.ServiceTypeLoadBalancer:
		for _, lb := range svc.Status.LoadBalancer.Ingress {
			if lb.IP != "" {
				targets = append(targets, lb.IP)
			}
			if lb.Hostname != "" {
				targets = append(targets, lb.Hostname)
			}
		}
	default:
		log.WithField("gateway", name).WithField("service", svc).Warn("Gloo: Proxy service type not supported")
	}
	return targets, nil
}
