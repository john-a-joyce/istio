// Copyright 2017 Istio Authors
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

package main

import (
	"fmt"
	//"istio.io/istio/pkg/keepalive"
	//"istio.io/pkg/ctrlz"
	"os"
//	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/cobra/doc"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"istio.io/istio/nsm_svc_reg/pkg/bootstrap"
//	"istio.io/istio/pilot/pkg/serviceregistry"
	"istio.io/istio/pkg/cmd"
	"istio.io/pkg/collateral"
	"istio.io/pkg/log"
	"istio.io/pkg/version"
)
var (

	serverArgs = bootstrap.PilotArgs{
	}


	loggingOptions = log.DefaultOptions()

	rootCmd = &cobra.Command{
		Use:          "jaj-test",
		Short:        "Istio Pilot.",
		Long:         "Service Mesh.",
		SilenceUsage: true,
	}

	discoveryCmd = &cobra.Command{
		Use:   "jaj-test",
		Short: "Start Istio proxy discovery service.",
		Args:  cobra.ExactArgs(0),
		RunE: func(c *cobra.Command, args []string) error {
			cmd.PrintFlags(c.Flags())
			if err := log.Configure(loggingOptions); err != nil {
				return err
			}

			// Create the stop channel for all of the servers.
			stop := make(chan struct{})

			// Create the server for the discovery service.
			discoveryServer, err := bootstrap.NewServer(serverArgs)
			if err != nil {
				return fmt.Errorf("failed to create discovery service: %v", err)
			}

			// Start the server
			if err := discoveryServer.Start(stop); err != nil {
				return fmt.Errorf("failed to start discovery service: %v", err)
			}

			cmd.WaitSignal(stop)
			return nil
		},
	}
)

/*
// when we run on k8s, the default trust domain is 'cluster.local', otherwise it is the empty string
func hasKubeRegistry() bool {
	for _, r := range serverArgs.Service.Registries {
		if serviceregistry.ServiceRegistry(r) == serviceregistry.KubernetesRegistry {
			return true
		}
	}
	return false
}

*/
func init() {
	discoveryCmd.PersistentFlags().StringVar(&serverArgs.Config.KubeConfig, "kubeconfig", "",
		"Kubernetes configuration file of local cluster")
	discoveryCmd.PersistentFlags().StringVar(&serverArgs.Config.KubeConfigRemote, "kubeconfigremote", "",
		"Kubernetes configuration file of remote cluster")
	discoveryCmd.PersistentFlags().StringVarP(&serverArgs.Config.WatchedNamespace, "appNamespace",
		"a", metav1.NamespaceAll,
		"Restrict the applications namespace the controller manages; if not set, controller watches all namespaces")

	// Attach the Istio logging options to the command.
	loggingOptions.AttachCobraFlags(rootCmd)

	cmd.AddFlags(rootCmd)

	rootCmd.AddCommand(discoveryCmd)
	rootCmd.AddCommand(version.CobraCommand())
	rootCmd.AddCommand(collateral.CobraCommand(rootCmd, &doc.GenManHeader{
		Title:   "Istio Pilot Discovery",
		Section: "pilot-discovery CLI",
		Manual:  "Istio Pilot Discovery",
	}))

}

func main() {
	if err := rootCmd.Execute(); err != nil {
		log.Errora(err)
		os.Exit(-1)
	}
}
