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
	"errors"
	"testing"

	cloudfoundry "github.com/cloudfoundry-community/go-cfclient"
	kong "github.com/kong/kubernetes-ingress-controller/pkg/client/configuration/clientset/versioned"
	kongfake "github.com/kong/kubernetes-ingress-controller/pkg/client/configuration/clientset/versioned/fake"
	openshift "github.com/openshift/client-go/route/clientset/versioned"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/suite"
	istio "istio.io/client-go/pkg/clientset/versioned"
	istiofake "istio.io/client-go/pkg/clientset/versioned/fake"
	"k8s.io/client-go/kubernetes"
	kubefake "k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/rest"

	ambassador "sigs.k8s.io/external-dns/third_party/getambassador.io/clientset/versioned"
	contour "sigs.k8s.io/external-dns/third_party/projectcontour.io/clientset/versioned"
	contourfake "sigs.k8s.io/external-dns/third_party/projectcontour.io/clientset/versioned/fake"
	gloo "sigs.k8s.io/external-dns/third_party/solo.io/clientset/versioned"
)

type MockClientGenerator struct {
	mock.Mock
	restConfig         *rest.Config
	kubeClient         kubernetes.Interface
	istioClient        istio.Interface
	cloudFoundryClient *cloudfoundry.Client
	openshiftClient    openshift.Interface
	glooClient         gloo.Interface
	contourClient      contour.Interface
	ambassadorClient   ambassador.Interface
	kongClient         kong.Interface
}

func (m *MockClientGenerator) RESTConfig() (*rest.Config, error) {
	args := m.Called()
	if args.Error(1) != nil {
		return nil, args.Error(1)
	}
	m.restConfig = args.Get(0).(*rest.Config)
	return m.restConfig, nil
}

func (m *MockClientGenerator) KubeClient() (kubernetes.Interface, error) {
	args := m.Called()
	if args.Error(1) != nil {
		return nil, args.Error(1)
	}
	m.kubeClient = args.Get(0).(kubernetes.Interface)
	return m.kubeClient, nil
}

func (m *MockClientGenerator) IstioClient() (istio.Interface, error) {
	args := m.Called()
	if args.Error(1) != nil {
		return nil, args.Error(1)
	}
	m.istioClient = args.Get(0).(istio.Interface)
	return m.istioClient, nil
}

func (m *MockClientGenerator) CloudFoundryClient(cfAPIEndpoint string, cfUsername string, cfPassword string) (*cloudfoundry.Client, error) {
	args := m.Called()
	if args.Error(1) != nil {
		return nil, args.Error(1)
	}
	m.cloudFoundryClient = args.Get(0).(*cloudfoundry.Client)
	return m.cloudFoundryClient, nil
}

func (m *MockClientGenerator) OpenShiftClient() (openshift.Interface, error) {
	args := m.Called()
	if args.Error(1) != nil {
		return nil, args.Error(1)
	}
	m.openshiftClient = args.Get(0).(openshift.Interface)
	return m.openshiftClient, nil
}

func (m *MockClientGenerator) GlooClient() (gloo.Interface, error) {
	args := m.Called()
	if args.Error(1) == nil {
		m.glooClient = args.Get(0).(gloo.Interface)
		return m.glooClient, nil
	}
	return nil, args.Error(1)
}

func (m *MockClientGenerator) ContourClient() (contour.Interface, error) {
	args := m.Called()
	if args.Error(1) == nil {
		m.contourClient = args.Get(0).(contour.Interface)
		return m.contourClient, nil
	}
	return nil, args.Error(1)
}

func (m *MockClientGenerator) AmbassadorClient() (ambassador.Interface, error) {
	args := m.Called()
	if args.Error(1) == nil {
		m.ambassadorClient = args.Get(0).(ambassador.Interface)
		return m.ambassadorClient, nil
	}
	return nil, args.Error(1)
}

func (m *MockClientGenerator) KongClient() (kong.Interface, error) {
	args := m.Called()
	if args.Error(1) != nil {
		return nil, args.Error(1)
	}
	m.kongClient = args.Get(0).(kong.Interface)
	return m.kongClient, nil
}

type ByNamesTestSuite struct {
	suite.Suite
}

func (suite *ByNamesTestSuite) TestAllInitialized() {
	mockClientGenerator := new(MockClientGenerator)
	mockClientGenerator.On("KubeClient").Return(kubefake.NewSimpleClientset(), nil)
	mockClientGenerator.On("IstioClient").Return(istiofake.NewSimpleClientset(), nil)
	mockClientGenerator.On("ContourClient").Return(contourfake.NewSimpleClientset(), nil)
	mockClientGenerator.On("KongClient").Return(kongfake.NewSimpleClientset(), nil)

	sources, err := ByNames(mockClientGenerator, []string{"service", "ingress", "istio-gateway", "contour-ingressroute", "contour-httpproxy", "kong-tcpingress", "fake"}, minimalConfig)
	suite.NoError(err, "should not generate errors")
	suite.Len(sources, 7, "should generate all six sources")
}

func (suite *ByNamesTestSuite) TestOnlyFake() {
	mockClientGenerator := new(MockClientGenerator)
	mockClientGenerator.On("KubeClient").Return(kubefake.NewSimpleClientset(), nil)

	sources, err := ByNames(mockClientGenerator, []string{"fake"}, minimalConfig)
	suite.NoError(err, "should not generate errors")
	suite.Len(sources, 1, "should generate kubefake source")
	suite.Nil(mockClientGenerator.kubeClient, "client should not be created")
}

func (suite *ByNamesTestSuite) TestSourceNotFound() {
	mockClientGenerator := new(MockClientGenerator)
	mockClientGenerator.On("KubeClient").Return(kubefake.NewSimpleClientset(), nil)

	sources, err := ByNames(mockClientGenerator, []string{"foo"}, minimalConfig)
	suite.Equal(err, ErrSourceNotFound, "should return source not found")
	suite.Len(sources, 0, "should not returns any source")
}

func (suite *ByNamesTestSuite) TestKubeClientFails() {
	mockClientGenerator := new(MockClientGenerator)
	mockClientGenerator.On("KubeClient").Return(nil, errors.New("foo"))

	_, err := ByNames(mockClientGenerator, []string{"service"}, minimalConfig)
	suite.Error(err, "should return an error if kubernetes client cannot be created")

	_, err = ByNames(mockClientGenerator, []string{"ingress"}, minimalConfig)
	suite.Error(err, "should return an error if kubernetes client cannot be created")

	_, err = ByNames(mockClientGenerator, []string{"istio-gateway"}, minimalConfig)
	suite.Error(err, "should return an error if kubernetes client cannot be created")

	_, err = ByNames(mockClientGenerator, []string{"contour-ingressroute"}, minimalConfig)
	suite.Error(err, "should return an error if kubernetes client cannot be created")

	_, err = ByNames(mockClientGenerator, []string{"kong-tcpingress"}, minimalConfig)
	suite.Error(err, "should return an error if kubernetes client cannot be created")
}

func (suite *ByNamesTestSuite) TestIstioClientFails() {
	mockClientGenerator := new(MockClientGenerator)
	mockClientGenerator.On("KubeClient").Return(kubefake.NewSimpleClientset(), nil)
	mockClientGenerator.On("IstioClient").Return(nil, errors.New("foo"))
	mockClientGenerator.On("ContourClient").Return(nil, errors.New("foo"))
	mockClientGenerator.On("DynamicKubernetesClient").Return(nil, errors.New("foo"))

	_, err := ByNames(mockClientGenerator, []string{"istio-gateway"}, minimalConfig)
	suite.Error(err, "should return an error if istio client cannot be created")

	_, err = ByNames(mockClientGenerator, []string{"contour-ingressroute"}, minimalConfig)
	suite.Error(err, "should return an error if contour client cannot be created")
	_, err = ByNames(mockClientGenerator, []string{"contour-httpproxy"}, minimalConfig)
	suite.Error(err, "should return an error if contour client cannot be created")
}

func TestByNames(t *testing.T) {
	suite.Run(t, new(ByNamesTestSuite))
}

var minimalConfig = &Config{
	ContourLoadBalancerService: "heptio-contour/contour",
}
