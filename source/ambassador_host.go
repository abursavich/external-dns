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
	"fmt"
	"sort"
	"strings"

	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	api "k8s.io/kubernetes/pkg/apis/core"

	"sigs.k8s.io/external-dns/endpoint"
	ambassador_v2 "sigs.k8s.io/external-dns/third_party/getambassador.io/apis/ambassador/v2"
	informers "sigs.k8s.io/external-dns/third_party/getambassador.io/informers/externalversions"
	informers_v2 "sigs.k8s.io/external-dns/third_party/getambassador.io/informers/externalversions/ambassador/v2"
)

// ambHostAnnotation is the annotation in the Host that maps to a Service
const ambHostAnnotation = "external-dns.ambassador-service"

// ambassadorHostSource is an implementation of Source for Ambassador Host objects.
// The IngressRoute implementation uses the spec.virtualHost.fqdn value for the hostname.
// Use targetAnnotationKey to explicitly set Endpoint.
type ambassadorHostSource struct {
	kubeClient   kubernetes.Interface
	hostInformer informers_v2.HostInformer
	namespace    string
}

// NewAmbassadorHostSource creates a new ambassadorHostSource with the given config.
func NewAmbassadorHostSource(clients ClientGenerator, config *Config) (Source, error) {
	kubeClient, err := clients.KubeClient()
	if err != nil {
		return nil, err
	}

	ambassadorClient, err := clients.AmbassadorClient()
	if err != nil {
		return nil, err
	}

	informerFactory := informers.NewSharedInformerFactoryWithOptions(ambassadorClient, 0, informers.WithNamespace(config.Namespace))
	hostInformer := informerFactory.Getambassador().V2().Hosts()
	hostInformer.Informer() // Register with factory before starting
	informerFactory.Start(wait.NeverStop)

	if err := waitForCacheSync(context.Background(), informerFactory); err != nil {
		return nil, err
	}

	return &ambassadorHostSource{
		kubeClient:   kubeClient,
		hostInformer: hostInformer,
		namespace:    config.Namespace,
	}, nil
}

func (sc *ambassadorHostSource) AddEventHandler(ctx context.Context, handler func()) {
	sc.hostInformer.Informer().AddEventHandler(eventHandlerFunc(handler))
}

// Endpoints returns endpoint objects for each host-target combination that should be processed.
// Retrieves all Hosts in the source's namespace(s).
func (sc *ambassadorHostSource) Endpoints(ctx context.Context) ([]*endpoint.Endpoint, error) {
	hosts, err := sc.hostInformer.Lister().Hosts(sc.namespace).List(labels.Everything())
	if err != nil {
		return nil, err
	}

	var endpoints []*endpoint.Endpoint
	for _, host := range hosts {
		// look for the "exernal-dns.ambassador-service" annotation. If it is not there then just ignore this `Host`
		service, found := host.Annotations[ambHostAnnotation]
		if !found {
			log.Debugf("Host %s/%s ignored: no annotation %q found", host.Namespace, host.Name, ambHostAnnotation)
			continue
		}

		targets, err := sc.targetsFromAmbassadorLoadBalancer(ctx, service)
		if err != nil {
			return nil, err
		}

		hostEndpoints, err := sc.endpointsFromHost(ctx, host, targets)
		if err != nil {
			return nil, err
		}
		if len(hostEndpoints) == 0 {
			log.Debugf("No endpoints could be generated from Host %s/%s", host.Namespace, host.Name)
			continue
		}

		log.Debugf("Endpoints generated from Host: %s/%s: %v", host.Namespace, host.Name, hostEndpoints)
		endpoints = append(endpoints, hostEndpoints...)
	}

	for _, ep := range endpoints {
		sort.Sort(ep.Targets)
	}

	return endpoints, nil
}

// endpointsFromHost extracts the endpoints from a Host object
func (sc *ambassadorHostSource) endpointsFromHost(ctx context.Context, host *ambassador_v2.Host, targets endpoint.Targets) ([]*endpoint.Endpoint, error) {
	ttl, err := getTTLFromAnnotations(host.Annotations)
	if err != nil {
		return nil, err
	}

	if host.Spec != nil && host.Spec.Hostname != "" {
		providerSpecific := endpoint.ProviderSpecific{}
		setIdentifier := ""
		return endpointsForHostname(host.Spec.Hostname, targets, ttl, providerSpecific, setIdentifier), nil
	}

	return nil, nil
}

func (sc *ambassadorHostSource) targetsFromAmbassadorLoadBalancer(ctx context.Context, service string) (targets endpoint.Targets, err error) {
	lbNamespace, lbName, err := parseAmbLoadBalancerService(service)
	if err != nil {
		return nil, err
	}

	svc, err := sc.kubeClient.CoreV1().Services(lbNamespace).Get(ctx, lbName, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}

	for _, lb := range svc.Status.LoadBalancer.Ingress {
		if lb.IP != "" {
			targets = append(targets, lb.IP)
		}
		if lb.Hostname != "" {
			targets = append(targets, lb.Hostname)
		}
	}

	return
}

// parseAmbLoadBalancerService returns a name/namespace tuple from the annotation in
// an Ambassador Host CRD
//
// This is a thing because Ambassador has historically supported cross-namespace
// references using a name.namespace syntax, but here we want to also support
// namespace/name.
//
// Returns namespace, name, error.
func parseAmbLoadBalancerService(service string) (namespace, name string, err error) {
	// Start by assuming that we have namespace/name.
	parts := strings.Split(service, "/")

	if len(parts) == 1 {
		// No "/" at all, so let's try for name.namespace. To be consistent with the
		// rest of Ambassador, use SplitN to limit this to one split, so that e.g.
		// svc.foo.bar uses service "svc" in namespace "foo.bar".
		parts = strings.SplitN(service, ".", 2)

		if len(parts) == 2 {
			// We got a namespace, great.
			name := parts[0]
			namespace := parts[1]

			return namespace, name, nil
		}

		// If here, we have no separator, so the whole string is the service, and
		// we can assume the default namespace.
		name := service
		namespace := api.NamespaceDefault

		return namespace, name, nil
	}
	if len(parts) == 2 {
		// This is "namespace/name". Note that the name could be qualified,
		// which is fine.
		namespace := parts[0]
		name := parts[1]

		return namespace, name, nil
	}

	// If we got here, this string is simply ill-formatted. Return an error.
	return "", "", errors.New(fmt.Sprintf("invalid external-dns service: %s", service))
}
