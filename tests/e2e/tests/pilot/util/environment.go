// Copyright 2017,2018 Istio Authors
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

package util

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"io"
	"io/ioutil"
	"math/big"
	"regexp"
	"strconv"
	"strings"
	"text/template"
	"time"

	"github.com/davecgh/go-spew/spew"
	"github.com/ghodss/yaml"
	"github.com/pkg/errors"
	"k8s.io/api/core/v1"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"os"
	"path"

	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pilot/pkg/bootstrap"
	"istio.io/istio/pilot/pkg/config/clusterregistry"
	"istio.io/istio/pilot/pkg/config/kube/crd"
	"istio.io/istio/pilot/pkg/kube/inject"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/serviceregistry"
	"istio.io/istio/pilot/pkg/serviceregistry/kube"
	"istio.io/istio/pilot/test/util"
	"istio.io/istio/pkg/log"
	testutil "istio.io/istio/tests/util"
)

const (
	ingressSecretName      = "istio-ingress-certs"
	sidecarInjectorService = "istio-sidecar-injector"
	mixerConfigFile        = "/etc/istio/proxy/envoy_mixer.json"
	mixerConfigAuthFile    = "/etc/istio/proxy/envoy_mixer_auth.json"
	pilotConfigFile        = "/etc/istio/proxy/envoy_pilot.json"
	pilotConfigAuthFile    = "/etc/istio/proxy/envoy_pilot_auth.json"
)

// Environment defines the test configuration.
type Environment struct {
	// nolint: maligned
	Name   string
	Config Config

	sidecarTemplate string

	KubeClient kubernetes.Interface

	// Multicluster related parameters. A second instance of a kube client is needed while
	// clusterStore stores the clusterregistry configuration used instead of KUBECONFIG
	RemoteKubeConfig string
	RemoteKubeClient kubernetes.Interface
	clusterStore     *clusterregistry.ClusterStore

	// Directory where test data files are located.
	testDataDir string

	// Directory where test cert files are located.
	certDir string

	// map from app to pods
	Apps map[string][]string

	Auth                   meshconfig.MeshConfig_AuthPolicy
	ControlPlaneAuthPolicy meshconfig.AuthenticationPolicy
	MixerCustomConfigFile  string
	PilotCustomConfigFile  string

	namespaceCreated      bool
	istioNamespaceCreated bool

	meshConfig *meshconfig.MeshConfig
	CABundle   string

	config model.IstioConfigStore

	Err error
}

// TemplateData is a container for common fields accessed from yaml templates.
type TemplateData struct {
	// nolint: maligned
	Hub                    string
	Tag                    string
	Namespace              string
	IstioNamespace         string
	Registry               string
	AdmissionServiceName   string
	ImagePullPolicy        string
	Verbosity              int
	DebugPort              int
	Auth                   meshconfig.MeshConfig_AuthPolicy
	Mixer                  bool
	Ingress                bool
	Zipkin                 bool
	UseAdmissionWebhook    bool
	RDSv2                  bool
	ControlPlaneAuthPolicy meshconfig.AuthenticationPolicy
	PilotCustomConfigFile  string
	MixerCustomConfigFile  string
	CABundle               string
	MultiCluster           string
}

// NewEnvironment creates a new test environment based on the configuration.
func NewEnvironment(config Config) *Environment {
	e := Environment{
		Config:      config,
		Name:        "(no-auth environment)",
		testDataDir: "testdata/",
		certDir:     "../../../../pilot/docker/certs/",
		Auth:        meshconfig.MeshConfig_NONE,
		MixerCustomConfigFile: mixerConfigFile,
		PilotCustomConfigFile: pilotConfigFile,
	}

	if config.Auth {
		e.Name = "(auth environment)"
		e.Auth = meshconfig.MeshConfig_MUTUAL_TLS
		e.ControlPlaneAuthPolicy = meshconfig.AuthenticationPolicy_MUTUAL_TLS
		e.MixerCustomConfigFile = mixerConfigAuthFile
		e.PilotCustomConfigFile = pilotConfigAuthFile
	}

	return &e
}

// ToTemplateData creates a data structure containing common fields used in yaml templates.
func (e *Environment) ToTemplateData() TemplateData {
	return TemplateData{
		Hub:                    e.Config.Hub,
		Tag:                    e.Config.Tag,
		IstioNamespace:         e.Config.IstioNamespace,
		Auth:                   e.Auth,
		Zipkin:                 e.Config.Zipkin,
		Mixer:                  e.Config.Mixer,
		Namespace:              e.Config.Namespace,
		DebugPort:              e.Config.DebugPort,
		UseAdmissionWebhook:    e.Config.UseAdmissionWebhook,
		Registry:               e.Config.Registry,
		Verbosity:              e.Config.Verbosity,
		AdmissionServiceName:   e.Config.AdmissionServiceName,
		ControlPlaneAuthPolicy: e.ControlPlaneAuthPolicy,
		PilotCustomConfigFile:  e.PilotCustomConfigFile,
		MixerCustomConfigFile:  e.MixerCustomConfigFile,
		CABundle:               e.CABundle,
		RDSv2:                  e.Config.RDSv2,
		ImagePullPolicy:        e.Config.ImagePullPolicy,
		MultiCluster:           e.Config.ClusterRegistriesDir,
	}
}

// Setup creates the k8s environment and deploys the test apps
func (e *Environment) Setup() error {
	var err error
	if e.Config.KubeConfig != "" {
		if _, e.KubeClient, err = kube.CreateInterface(e.Config.KubeConfig); err != nil {
			return err
		}
	}
	// ClusterRegistiresDir indicates the Kubernetes cluster config should come from files versus KUBECONFIG
	// environmental variable.  The test config can be defined to use either a single cluster or 2 clusters
	if e.Config.ClusterRegistriesDir != "" {
		e.clusterStore, err = clusterregistry.ReadClusters(e.Config.ClusterRegistriesDir)
		if e.clusterStore == nil {
			return fmt.Errorf("failed to read clusters in the ClusterRegistriesDir %s",
				e.Config.ClusterRegistriesDir)
		}
		kubeCfgFile := e.clusterStore.GetPilotAccessConfig()
		kubeCfgFile = path.Join(e.Config.ClusterRegistriesDir, kubeCfgFile)
		e.Config.KubeConfig = kubeCfgFile
		if _, e.KubeClient, err = kube.CreateInterface(kubeCfgFile); err != nil {
			return err
		}
		// Note only a single remote cluster is currently supported.
		clusters := e.clusterStore.GetPilotClusters()
		for _, cluster := range clusters {
			kubeconfig := clusterregistry.GetClusterAccessConfig(cluster)
			e.RemoteKubeConfig = path.Join(e.Config.ClusterRegistriesDir, kubeconfig)

			log.Infof("Cluster name: %s, AccessConfigFile: %s", clusterregistry.GetClusterName(cluster), e.RemoteKubeConfig)
			// Expecting only a single remote cluster so hard code this.  The code won't throw an error
			// if more than 2 clusters are defined in the config files, but will only use the last cluster parsed.
			if _, e.RemoteKubeClient, err = kube.CreateInterface(e.RemoteKubeConfig); err != nil {
				return err
			}
		}
	}

	if e.Config.KubeConfig == "" && e.Config.ClusterRegistriesDir == "" {
		e.Config.KubeConfig = "pilot/pkg/kube/config"
		log.Info("Using linked in kube config. Set KUBECONFIG env before running the test.")
		if _, e.KubeClient, err = kube.CreateInterface(e.Config.KubeConfig); err != nil {
			return err
		}
	}

	crdclient, crderr := crd.NewClient(e.Config.KubeConfig, model.IstioConfigTypes, "")
	if crderr != nil {
		return crderr
	}
	if err = crdclient.RegisterResources(); err != nil {
		return err
	}

	e.config = model.MakeIstioStore(crdclient)

	if e.Config.Namespace == "" {
		if e.Config.Namespace, err = util.CreateNamespaceWithPrefix(e.KubeClient, "istio-test-app-", e.Config.UseAutomaticInjection); err != nil { // nolint: lll
			return err
		}
		// Create the namespace on the remote cluster if needed
		if e.RemoteKubeConfig != "" {
			if _, err = util.CreateNamespaceWithName(e.RemoteKubeClient, e.Config.Namespace, e.Config.UseAutomaticInjection); err != nil { // nolint: lll
				return err
			}
		}
		e.namespaceCreated = true
	} else {
		if _, err = e.KubeClient.CoreV1().Namespaces().Get(e.Config.Namespace, meta_v1.GetOptions{}); err != nil {
			return err
		}
		if e.RemoteKubeConfig != "" {
			if _, err = e.RemoteKubeClient.CoreV1().Namespaces().Get(e.Config.Namespace, meta_v1.GetOptions{}); err != nil { // nolint: lll
				return err
			}
		}
	}

	if e.Config.IstioNamespace == "" {
		if e.Config.IstioNamespace, err = util.CreateNamespaceWithPrefix(e.KubeClient, "istio-test-", false); err != nil {
			return err
		}
		e.istioNamespaceCreated = true
	} else {
		if _, err = e.KubeClient.CoreV1().Namespaces().Get(e.Config.IstioNamespace, meta_v1.GetOptions{}); err != nil {
			return err
		}
	}

	deploy := func(name, namespace string) error {
		var filledYaml string
		data := e.ToTemplateData()
		if filledYaml, err = e.Fill(name, data); err != nil {
			return err
		} else if err = e.KubeApply(filledYaml, namespace, false); err != nil {
			return err
		}
		return nil
	}

	if !e.Config.NoRBAC {
		if err = deploy("rbac-beta.yaml.tmpl", e.Config.IstioNamespace); err != nil {
			return err
		}
	}

	if err = deploy("config.yaml.tmpl", e.Config.IstioNamespace); err != nil {
		return err
	}

	if _, e.meshConfig, err = bootstrap.GetMeshConfig(e.KubeClient, e.Config.IstioNamespace, kube.IstioConfigMap); err != nil {
		return err
	}
	debugMode := e.Config.DebugImagesAndMode
	log.Infof("mesh %s", spew.Sdump(e.meshConfig))

	e.sidecarTemplate, err = inject.GenerateTemplateFromParams(&inject.Params{
		InitImage:       inject.InitImageName(e.Config.Hub, e.Config.Tag, debugMode),
		ProxyImage:      inject.ProxyImageName(e.Config.Hub, e.Config.Tag, debugMode),
		Verbosity:       e.Config.Verbosity,
		SidecarProxyUID: inject.DefaultSidecarProxyUID,
		EnableCoreDump:  true,
		Version:         "integration-test",
		Mesh:            e.meshConfig,
		DebugMode:       debugMode,
		ImagePullPolicy: e.Config.ImagePullPolicy,
	})
	if err != nil {
		return err
	}

	if e.Config.ClusterRegistriesDir != "" {
		if err = e.createMulticlusterConfig(); err != nil {
			return err
		}
	}

	if e.Config.UseAutomaticInjection {
		// Automatic side car injection is not supported when multiple clusters are being tested
		// as all istio control plane components are only installed on the first cluster.
		if e.RemoteKubeConfig != "" {
			return fmt.Errorf("automatic side car injection is not supported when multiple clusters are being tested")
		}
		if err = e.createSidecarInjector(); err != nil {
			return err
		}
	}

	if e.Config.UseAdmissionWebhook {
		// Admission Webhook is not supported when multiple clusters are being tested.
		if e.RemoteKubeConfig != "" {
			return fmt.Errorf("admission webhook is not supported when multiple clusters are being tested")
		}
		if err = e.createAdmissionWebhookSecret(); err != nil {
			return err
		}
	}

	if err = deploy("ca.yaml.tmpl", e.Config.IstioNamespace); err != nil {
		return err
	}
	time.Sleep(time.Second * 20)

	if err = deploy("pilot.yaml.tmpl", e.Config.IstioNamespace); err != nil {
		return err
	}
	if e.Config.Mixer {
		if err = deploy("mixer.yaml.tmpl", e.Config.IstioNamespace); err != nil {
			return err
		}
	}
	if serviceregistry.ServiceRegistry(e.Config.Registry) == serviceregistry.EurekaRegistry {
		if err = deploy("eureka.yaml.tmpl", e.Config.IstioNamespace); err != nil {
			return err
		}
	}

	// TODO probably need to augment this headless services in the remote cluster
	if err = deploy("headless.yaml.tmpl", e.Config.Namespace); err != nil {
		return err
	}
	if e.Config.Ingress {
		if err = deploy("ingress-proxy.yaml.tmpl", e.Config.IstioNamespace); err != nil {
			return err
		}
		// Create ingress key/cert in secret
		var key []byte
		key, err = ioutil.ReadFile(e.certDir + "cert.key")
		if err != nil {
			return err
		}
		var crt []byte
		crt, err = ioutil.ReadFile(e.certDir + "cert.crt")
		if err != nil {
			return err
		}
		_, err = e.KubeClient.CoreV1().Secrets(e.Config.IstioNamespace).Create(&v1.Secret{
			ObjectMeta: meta_v1.ObjectMeta{Name: ingressSecretName},
			Data: map[string][]byte{
				"tls.key": key,
				"tls.crt": crt,
			},
		})
		if err != nil {
			log.Warn("Secret already exists")
		}
	}
	if e.Config.Zipkin {
		if err = deploy("zipkin.yaml", e.Config.IstioNamespace); err != nil {
			return err
		}
	}
	if err = deploy("external-wikipedia.yaml.tmpl", e.Config.Namespace); err != nil {
		return err
	}
	if err = deploy("externalbin.yaml.tmpl", e.Config.Namespace); err != nil {
		return err
	}

	if err = e.deployApps(); err != nil {
		return err
	}

	nslist := []string{e.Config.IstioNamespace, e.Config.Namespace}
	e.Apps, err = util.GetAppPods(e.KubeClient, e.Config.KubeConfig, nslist)
	// TODO This is going to need some surgery in a couple places to get all the pods from both cluster into a single list
	return err
}

func (e *Environment) deployApps() error {
	// deploy a healthy mix of apps, with and without proxy
	if err := e.deployApp("t", "t", 8080, 80, 9090, 90, 7070, 70, "unversioned", false, false, false); err != nil {
		return err
	}
	if err := e.deployApp("a", "a", 8080, 80, 9090, 90, 7070, 70, "v1", true, false, false); err != nil {
		return err
	}
	if err := e.deployApp("b", "b", 80, 8080, 90, 9090, 70, 7070, "unversioned", true, false, false); err != nil {
		return err
	}
	if err := e.deployApp("c-v1", "c", 80, 8080, 90, 9090, 70, 7070, "v1", true, false, false); err != nil {
		return err
	}
	if err := e.deployApp("c-v2", "c", 80, 8080, 90, 9090, 70, 7070, "v2", true, false, false); err != nil {
		return err
	}
	if err := e.deployApp("d", "d", 80, 8080, 90, 9090, 70, 7070, "per-svc-auth", true, true, false); err != nil {
		return err
	}
	// If this is a multicluster test deploy some services on the remote cluster.
	// TODO This is a placeholder since tests need to be written to utilize these remote pods
	if e.RemoteKubeConfig != "" {
		if err := e.deployApp("t-remote", "t", 8080, 80, 9090, 90, 7070, 70, "unversioned", false, false, true); err != nil {
			return err
		}
	}
	// Add another service without sidecar to test mTLS blacklisting (as in the e2e test
	// environment, pilot can see only services in the test namespaces). This service
	// will be listed in mtlsExcludedServices in the mesh config.
	return e.deployApp("e", "fake-control", 80, 8080, 90, 9090, 70, 7070, "fake-control", false, false, false)
}

func (e *Environment) deployApp(deployment, svcName string, port1, port2, port3, port4, port5, port6 int,
	version string, injectProxy bool, perServiceAuth bool, remoteCluster bool) error {
	// Eureka does not support management ports
	healthPort := "true"
	if serviceregistry.ServiceRegistry(e.Config.Registry) == serviceregistry.EurekaRegistry {
		healthPort = "false"
	}

	w, err := e.Fill("app.yaml.tmpl", map[string]string{
		"Hub":             e.Config.Hub,
		"Tag":             e.Config.Tag,
		"service":         svcName,
		"perServiceAuth":  strconv.FormatBool(perServiceAuth),
		"deployment":      deployment,
		"port1":           strconv.Itoa(port1),
		"port2":           strconv.Itoa(port2),
		"port3":           strconv.Itoa(port3),
		"port4":           strconv.Itoa(port4),
		"port5":           strconv.Itoa(port5),
		"port6":           strconv.Itoa(port6),
		"version":         version,
		"istioNamespace":  e.Config.IstioNamespace,
		"injectProxy":     strconv.FormatBool(injectProxy),
		"healthPort":      healthPort,
		"ImagePullPolicy": e.Config.ImagePullPolicy,
	})
	if err != nil {
		return err
	}

	writer := new(bytes.Buffer)

	if injectProxy && !e.Config.UseAutomaticInjection {
		if err := inject.IntoResourceFile(e.sidecarTemplate, e.meshConfig, strings.NewReader(w), writer); err != nil {
			return err
		}
	} else {
		if _, err := io.Copy(writer, strings.NewReader(w)); err != nil {
			return err
		}
	}

	return e.KubeApply(writer.String(), e.Config.Namespace, remoteCluster)
}

// Teardown cleans up the k8s environment, removing any resources that were created by the tests.
func (e *Environment) Teardown() {
	if e.KubeClient == nil {
		return
	}

	needToTeardown := !e.Config.SkipCleanup

	// spill all logs on error
	if e.Err != nil {
		if e.Config.SkipCleanupOnFailure {
			needToTeardown = false
		}

		// Dump all logs on error.
		e.dumpErrorLogs()
	}

	// If configured to cleanup after each test, do so now.
	if !needToTeardown {
		return
	}

	if e.Config.UseAdmissionWebhook {
		if err := e.deleteAdmissionWebhookSecret(); err != nil {
			log.Infof("Could not delete admission webhook secret: %v", err)
		}
	}

	// automatic injection webhook is not namespaced.
	if e.Config.UseAutomaticInjection {
		e.deleteSidecarInjector()
	}

	if filledYaml, err := e.Fill("rbac-beta.yaml.tmpl", e.ToTemplateData()); err != nil {
		log.Infof("RBAC template could could not be processed, please delete stale ClusterRoleBindings: %v",
			err)
	} else if err = e.KubeDelete(filledYaml, e.Config.IstioNamespace); err != nil {
		log.Infof("RBAC config could could not be deleted: %v", err)
	}

	if e.Config.Ingress {
		if err := e.KubeClient.ExtensionsV1beta1().Ingresses(e.Config.Namespace).
			DeleteCollection(&meta_v1.DeleteOptions{}, meta_v1.ListOptions{}); err != nil {
			log.Warna(err)
		}
		if err := e.KubeClient.CoreV1().Secrets(e.Config.Namespace).
			Delete(ingressSecretName, &meta_v1.DeleteOptions{}); err != nil {
			log.Warna(err)
		}
	}

	if e.namespaceCreated {
		util.DeleteNamespace(e.KubeClient, e.Config.Namespace)
		if e.RemoteKubeConfig != "" {
			util.DeleteNamespace(e.RemoteKubeClient, e.Config.Namespace)
		}
		e.Config.Namespace = ""
	}
	if e.istioNamespaceCreated {
		util.DeleteNamespace(e.KubeClient, e.Config.IstioNamespace)
		e.Config.IstioNamespace = ""
	}
}

func (e *Environment) dumpErrorLogs() {
	// Use the common logs dumper.
	err := testutil.FetchAndSaveClusterLogs(e.Config.Namespace, e.Config.ErrorLogsDir)
	if err != nil {
		log.Errora("Failed to dump logs", err)
	}
	err = testutil.FetchAndSaveClusterLogs(e.Config.IstioNamespace, e.Config.ErrorLogsDir)
	if err != nil {
		log.Errora("Failed to dump logs", err)
	}

	// Temp: dump logs the old way, for people used with it.
	for _, pod := range util.GetPods(e.KubeClient, e.Config.Namespace) {
		var filename, content string
		if strings.HasPrefix(pod, "istio-pilot") {
			Tlog("Discovery log", pod)
			filename = "istio-pilot"
			content = util.FetchLogs(e.KubeClient, pod, e.Config.IstioNamespace, "discovery")
		} else if strings.HasPrefix(pod, "istio-mixer") {
			Tlog("Mixer log", pod)
			filename = "istio-mixer"
			content = util.FetchLogs(e.KubeClient, pod, e.Config.IstioNamespace, "mixer")
		} else if strings.HasPrefix(pod, "istio-ingress") {
			Tlog("Ingress log", pod)
			filename = "istio-ingress"
			content = util.FetchLogs(e.KubeClient, pod, e.Config.IstioNamespace, inject.ProxyContainerName)
		} else {
			Tlog("Proxy log", pod)
			filename = pod
			content = util.FetchLogs(e.KubeClient, pod, e.Config.Namespace, inject.ProxyContainerName)
			if e.RemoteKubeConfig != "" {
				for _, remotePod := range util.GetPods(e.RemoteKubeClient, e.Config.Namespace) {
					Tlog("Proxy log", remotePod)
					filename = remotePod
					content = util.FetchLogs(e.RemoteKubeClient, remotePod, e.Config.Namespace, inject.ProxyContainerName)
				}
			}
		}

		if len(e.Config.ErrorLogsDir) > 0 {
			if err := ioutil.WriteFile(e.Config.ErrorLogsDir+"/"+filename+".txt", []byte(content), 0644); err != nil {
				log.Errorf("Failed to save logs to %s:%s. Dumping on stderr\n", filename, err)
				log.Info(content)
			}
		} else {
			log.Info(content)
		}
	}
}

// KubeApply runs kubectl apply with the given yaml and namespace.
func (e *Environment) KubeApply(yaml, namespace string, remote bool) error {
	if remote {
		return util.RunInput(fmt.Sprintf("kubectl apply --kubeconfig %s -n %s -f -",
			e.RemoteKubeConfig, namespace), yaml)
	}
	return util.RunInput(fmt.Sprintf("kubectl apply --kubeconfig %s -n %s -f -",
		e.Config.KubeConfig, namespace), yaml)
}

// KubeDelete runs kubectl delete with the given yaml and namespace.
func (e *Environment) KubeDelete(yaml, namespace string) error {
	return util.RunInput(fmt.Sprintf("kubectl delete --kubeconfig %s -n %s -f -",
		e.Config.KubeConfig, namespace), yaml)
}

// Response represents a response to a client request.
type Response struct {
	// Body is the body of the response
	Body string
	// ID is a unique identifier of the resource in the response
	ID []string
	// Version is the version of the resource in the response
	Version []string
	// Port is the port of the resource in the response
	Port []string
	// Code is the response code
	Code []string
}

const httpOk = "200"

// IsHTTPOk returns true if the response code was 200
func (r *Response) IsHTTPOk() bool {
	return len(r.Code) > 0 && r.Code[0] == httpOk
}

var (
	idRex      = regexp.MustCompile("(?i)X-Request-Id=(.*)")
	versionRex = regexp.MustCompile("ServiceVersion=(.*)")
	portRex    = regexp.MustCompile("ServicePort=(.*)")
	codeRex    = regexp.MustCompile("StatusCode=(.*)")
)

// DumpConfig configuration in effect during test
func (e *Environment) DumpConfig(names ...string) (string, error) {
	cmd := fmt.Sprintf("kubectl --kubeconfig %s get %s --all-namespaces -o yaml",
		e.Config.KubeConfig, strings.Join(names, ","))

	return util.Shell(cmd)
}

// Routes gets routes from the pod or returns error
func (e *Environment) Routes(app string) (string, error) {
	if len(e.Apps[app]) == 0 {
		return "", errors.Errorf("missing pod names for app %q", app)
	}

	pod := e.Apps[app][0]

	routesURL := "http://localhost:15000/routes"
	cmd := fmt.Sprintf("kubectl exec %s --kubeconfig %s -n %s -c app -- client -url %s",
		pod, e.Config.KubeConfig, e.Config.Namespace, routesURL)

	routes, err := util.Shell(cmd)

	if err != nil {
		return "", errors.WithMessage(err, "failed to get routes")
	}

	return routes, nil
}

// ClientRequest makes the given request from within the k8s environment.
func (e *Environment) ClientRequest(app, url string, count int, extra string) Response {
	out := Response{}
	if len(e.Apps[app]) == 0 {
		log.Errorf("missing pod names for app %q", app)
		return out
	}

	pod := e.Apps[app][0]

	cmd := fmt.Sprintf("kubectl exec %s --kubeconfig %s -n %s -c app -- client -url %s -count %d %s",
		pod, e.Config.KubeConfig, e.Config.Namespace, url, count, extra)
	request, err := util.Shell(cmd)

	if err != nil {
		log.Errorf("client request error %v for %s in %s", err, url, app)
		return out
	}

	out.Body = request

	ids := idRex.FindAllStringSubmatch(request, -1)
	for _, id := range ids {
		out.ID = append(out.ID, id[1])
	}

	versions := versionRex.FindAllStringSubmatch(request, -1)
	for _, version := range versions {
		out.Version = append(out.Version, version[1])
	}

	ports := portRex.FindAllStringSubmatch(request, -1)
	for _, port := range ports {
		out.Port = append(out.Port, port[1])
	}

	codes := codeRex.FindAllStringSubmatch(request, -1)
	for _, code := range codes {
		out.Code = append(out.Code, code[1])
	}

	return out
}

// ApplyConfig fills in the given template file (if necessary) and applies the configuration.
func (e *Environment) ApplyConfig(inFile string, data map[string]string) error {
	config, err := e.Fill(inFile, data)
	if err != nil {
		return err
	}

	vs, _, err := crd.ParseInputs(config)
	if err != nil {
		return err
	}

	for _, v := range vs {
		// fill up namespace for the config
		v.Namespace = e.Config.Namespace

		old, exists := e.config.Get(v.Type, v.Name, v.Namespace)
		if exists {
			v.ResourceVersion = old.ResourceVersion
			_, err = e.config.Update(v)
		} else {
			_, err = e.config.Create(v)
		}
		if err != nil {
			return err
		}
	}

	sleepTime := time.Second * 10
	log.Infof("Sleeping %v for the config to propagate", sleepTime)
	time.Sleep(sleepTime)
	return nil
}

// DeleteConfig deletes the given configuration from the k8s environment
func (e *Environment) DeleteConfig(inFile string, data map[string]string) error {
	config, err := e.Fill(inFile, data)
	if err != nil {
		return err
	}

	vs, _, err := crd.ParseInputs(config)
	if err != nil {
		return err
	}

	for _, v := range vs {
		// fill up namespace for the config
		v.Namespace = e.Config.Namespace

		log.Infof("Delete config %s", v.Key())
		if err = e.config.Delete(v.Type, v.Name, v.Namespace); err != nil {
			return err
		}
	}

	sleepTime := time.Second * 3
	log.Infof("Sleeping %v for the config to propagate", sleepTime)
	time.Sleep(sleepTime)
	return nil
}

// DeleteAllConfigs deletes any config resources that were installed by the tests.
func (e *Environment) DeleteAllConfigs() error {
	for _, desc := range e.config.ConfigDescriptor() {
		configs, err := e.config.List(desc.Type, e.Config.Namespace)
		if err != nil {
			return err
		}
		for _, config := range configs {
			log.Infof("Delete config %s", config.Key())
			if err = e.config.Delete(desc.Type, config.Name, config.Namespace); err != nil {
				return err
			}
		}
	}
	return nil
}

func createWebhookCerts(service, namespace string) (caCertPEM, serverCertPEM, serverKeyPEM []byte, err error) { // nolint: lll
	var (
		webhookCertValidFor = 365 * 24 * time.Hour
		rsaBits             = 2048
		maxSerialNumber     = new(big.Int).Lsh(big.NewInt(1), 128)

		notBefore = time.Now()
		notAfter  = notBefore.Add(webhookCertValidFor)
	)

	// Generate self-signed CA cert
	caKey, err := rsa.GenerateKey(rand.Reader, rsaBits)
	if err != nil {
		return nil, nil, nil, err
	}
	caSerialNumber, err := rand.Int(rand.Reader, maxSerialNumber)
	if err != nil {
		return nil, nil, nil, err
	}
	caTemplate := x509.Certificate{
		SerialNumber:          caSerialNumber,
		Subject:               pkix.Name{CommonName: fmt.Sprintf("%s_a", service)},
		NotBefore:             notBefore,
		NotAfter:              notAfter,
		KeyUsage:              x509.KeyUsageCertSign,
		BasicConstraintsValid: true,
		IsCA: true,
	}
	caCert, err := x509.CreateCertificate(rand.Reader, &caTemplate, &caTemplate, &caKey.PublicKey, caKey)
	if err != nil {
		return nil, nil, nil, err
	}

	// Generate server certificate signed by self-signed CA
	serverKey, err := rsa.GenerateKey(rand.Reader, rsaBits)
	if err != nil {
		return nil, nil, nil, err
	}
	serverSerialNumber, err := rand.Int(rand.Reader, maxSerialNumber)
	if err != nil {
		return nil, nil, nil, err
	}
	serverTemplate := x509.Certificate{
		SerialNumber: serverSerialNumber,
		Subject:      pkix.Name{CommonName: fmt.Sprintf("%s.%s.svc", service, namespace)},
		NotBefore:    notBefore,
		NotAfter:     notAfter,
		KeyUsage:     x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
	}
	serverCert, err := x509.CreateCertificate(rand.Reader, &serverTemplate, &caTemplate, &serverKey.PublicKey, caKey)
	if err != nil {
		return nil, nil, nil, err
	}

	// PEM encoding
	caCertPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caCert})
	serverCertPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: serverCert})
	serverKeyPEM = pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(serverKey)})

	return caCertPEM, serverCertPEM, serverKeyPEM, nil
}

func (e *Environment) createAdmissionWebhookSecret() error {
	caCert, serverCert, serverKey, err := createWebhookCerts(e.Config.AdmissionServiceName, e.Config.IstioNamespace)
	if err != nil {
		return err
	}
	data := map[string]string{
		"webhookName": "pilot-webhook",
		"caCert":      base64.StdEncoding.EncodeToString(caCert),
		"serverCert":  base64.StdEncoding.EncodeToString(serverCert),
		"serverKey":   base64.StdEncoding.EncodeToString(serverKey),
	}
	filledYaml, err := e.Fill("pilot-webhook-secret.yaml.tmpl", data)
	if err != nil {
		return err
	}
	return e.KubeApply(filledYaml, e.Config.IstioNamespace, false)
}

func (e *Environment) deleteAdmissionWebhookSecret() error {
	return util.Run(fmt.Sprintf("kubectl delete --kubeconfig %s -n %s secret pilot-webhook",
		e.Config.KubeConfig, e.Config.IstioNamespace))
}

func (e *Environment) createSidecarInjector() error {
	configData, err := yaml.Marshal(&inject.Config{
		Policy:   inject.InjectionPolicyEnabled,
		Template: e.sidecarTemplate,
	})
	if err != nil {
		return err
	}

	// sidecar configuration template
	if _, err = e.KubeClient.CoreV1().ConfigMaps(e.Config.IstioNamespace).Create(&v1.ConfigMap{
		ObjectMeta: meta_v1.ObjectMeta{
			Name: "istio-inject",
		},
		Data: map[string]string{
			"config": string(configData),
		},
	}); err != nil {
		return err
	}

	// webhook certificates
	ca, cert, key, err := createWebhookCerts(sidecarInjectorService, e.Config.IstioNamespace) // nolint: vetshadow
	if err != nil {
		return err
	}
	if _, err := e.KubeClient.CoreV1().Secrets(e.Config.IstioNamespace).Create(&v1.Secret{ // nolint: vetshadow
		ObjectMeta: meta_v1.ObjectMeta{Name: "sidecar-injector-certs"},
		Data: map[string][]byte{
			"cert.pem": cert,
			"key.pem":  key,
		},
		Type: v1.SecretTypeOpaque,
	}); err != nil {
		return err
	}

	// webhook deployment
	e.CABundle = base64.StdEncoding.EncodeToString(ca)
	if filledYaml, err := e.Fill("sidecar-injector.yaml.tmpl", e.ToTemplateData()); err != nil { // nolint: vetshadow
		return err
	} else if err = e.KubeApply(filledYaml, e.Config.IstioNamespace, false); err != nil {
		return err
	}

	// wait until injection webhook service is running before
	// proceeding with deploying test applications
	if _, err = util.GetAppPods(e.KubeClient, e.Config.KubeConfig, []string{e.Config.IstioNamespace}); err != nil {
		return fmt.Errorf("sidecar injector failed to start: %v", err)
	}
	return nil
}

func (e *Environment) deleteSidecarInjector() {
	if filledYaml, err := e.Fill("sidecar-injector.yaml.tmpl", e.ToTemplateData()); err != nil {
		log.Infof("Sidecar injector template could not be processed, please delete stale injector webhook: %v",
			err)
	} else if err = e.KubeDelete(filledYaml, e.Config.IstioNamespace); err != nil {
		log.Infof("Sidecar injector could not be deleted: %v", err)
	}
}

// Fill fills in a template with the given values
func (e *Environment) Fill(inFile string, values interface{}) (string, error) {
	var out bytes.Buffer
	w := bufio.NewWriter(&out)

	tmpl, err := template.ParseFiles(e.testDataDir + inFile)
	if err != nil {
		return "", err
	}

	if err := tmpl.Execute(w, values); err != nil {
		return "", err
	}

	if err := w.Flush(); err != nil {
		return "", err
	}

	return out.String(), nil
}

func (e *Environment) createMulticlusterConfig() error {
	// Although this function loops through all files in the configuration directory the tests assumes a single
	// clusterregistry configuration file.

	info, err := os.Stat(e.Config.ClusterRegistriesDir)
	if err != nil {
		switch err := err.(type) {
		case *os.PathError:
			return fmt.Errorf("error reading %s: %v", e.Config.ClusterRegistriesDir, err.Err)
		default:
			return fmt.Errorf("error reading %s: %v", e.Config.ClusterRegistriesDir, err)
		}
	}

	if info.IsDir() {
		if strings.Contains(e.Config.ClusterRegistriesDir, "=") {
			return fmt.Errorf("cannot give a key name for a directory path")
		}
		fileList, err := ioutil.ReadDir(e.Config.ClusterRegistriesDir)
		if err != nil {
			return fmt.Errorf("error listing files in %s: %v", e.Config.ClusterRegistriesDir, err)
		}
		var Data []byte
		configData := make(map[string]string)
		for _, item := range fileList {
			itemPath := path.Join(e.Config.ClusterRegistriesDir, item.Name())
			if item.Mode().IsRegular() {
				keyName := item.Name()
				Data, err = ioutil.ReadFile(itemPath)
				if err != nil {
					return err
				}
				configData[keyName] = string(Data)
			}
		}
		if _, err = e.KubeClient.CoreV1().ConfigMaps(e.Config.IstioNamespace).Create(&v1.ConfigMap{

			ObjectMeta: meta_v1.ObjectMeta{
				Name: "multicluster",
			},
			Data: configData,
		}); err != nil {
			return err
		}
	}
	return nil
}
