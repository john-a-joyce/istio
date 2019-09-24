// Copyright 2019
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package bootstrap

import (
	"fmt"
	"github.com/hashicorp/go-multierror"
	"time"

	//"time"

	//"github.com/davecgh/go-spew/spew"

	//"github.com/hashicorp/go-multierror"

	kubelib "istio.io/istio/pkg/kube"
	//"istio.io/istio/pilot/pkg/config/clusterregistry"

	"istio.io/istio/pilot/pkg/serviceregistry/aggregate"

	"k8s.io/client-go/kubernetes"
)

func init() {
}

// ConfigArgs provide configuration options for the configuration controller.
type ConfigArgs struct {
	ClusterRegistriesNamespace string
	KubeConfig                 string
	KubeConfigRemote           string
	WatchedNamespace           string
}

// PilotArgs provides all of the configuration parameters for the Pilot discovery service.
type PilotArgs struct {
	Namespace                string
	Config                   ConfigArgs
}

// Server contains the runtime configuration for the Pilot discovery service.
type Server struct {
	ServiceController *aggregate.Controller

	kubeClient       kubernetes.Interface
	kubeconfig       string
	remoteKubeClient kubernetes.Interface
	remoteKubeconfig string
	startFuncs       []startFunc
}

// NewServer creates a new Server instance based on the provided arguments.
func NewServer(args PilotArgs) (*Server, error) {
	// If the namespace isn't set, try looking it up from the environment.

	if args.Config.ClusterRegistriesNamespace == "" {
		if args.Namespace != "" {
			args.Config.ClusterRegistriesNamespace = args.Namespace
		} else {
			args.Config.ClusterRegistriesNamespace = "default"
		}
	}

	s := &Server{}

	// Apply the arguments to the configuration.
	if err := s.initKubeClient(&args); err != nil {
		return nil, fmt.Errorf("local kube client: %v", err)
	}

	if err := s.initRemoteClient(&args); err != nil {
		return nil, fmt.Errorf("remote kube client: %v", err)
	}

	controller := newController(s.kubeClient, "default", s.kubeconfig, s.remoteKubeClient, s.remoteKubeconfig)
	stopCh := make(chan struct{})
	go controller.Run(stopCh)
	time.Sleep(300 * time.Second)
	return s, nil
}

// Start starts all components of the service
func (s *Server) Start(stop <-chan struct{}) error {
	// Now start all of the components.
	for _, fn := range s.startFuncs {
		if err := fn(stop); err != nil {
			return err
		}
	}

	return nil
}

// startFunc defines a function that will be used to start one or more components of the service.
type startFunc func(stop <-chan struct{}) error

func (s *Server) getKubeCfgFile(args *PilotArgs) string {
	return args.Config.KubeConfig
}

// initKubeClient creates the k8s client if running in an k8s environment.
func (s *Server) initKubeClient(args *PilotArgs) error {
	client, kuberr := kubelib.CreateClientset(args.Config.KubeConfig, "")
	if kuberr != nil {
		return multierror.Prefix(kuberr, "failed to connect to Kubernetes API.")
	}
	s.kubeClient = client
	s.kubeconfig = args.Config.KubeConfig

	return nil
}

func (s *Server) initRemoteClient(args *PilotArgs) error {
	if args.Config.KubeConfigRemote == "" {
		err := fmt.Errorf("remotekubeconfig empty")
		return multierror.Prefix(err, "failed to connect to Kubernetes API.")
	}
	client, kuberr := kubelib.CreateClientset(args.Config.KubeConfigRemote, "")
	if kuberr != nil {
		return multierror.Prefix(kuberr, "failed to connect to Kubernetes API.")
	}
	s.remoteKubeClient = client
	s.remoteKubeconfig = args.Config.KubeConfigRemote

	return nil
}

func (s *Server) addStartFunc(fn startFunc) {
	s.startFuncs = append(s.startFuncs, fn)
}

