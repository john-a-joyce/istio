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
	"regexp"
	"strconv"
	"time"

	corev1 "k8s.io/api/core/v1"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"

	"istio.io/istio/tests/util"
	"istio.io/pkg/log"
)

const (
	svcLabel    = "nsm/servicename"
	portLabel   = "nsm/serviceport"
	maxRetries  = 5
	ifName      = "nsm"
	namespace   = "default"
)

// Controller is the controller implementation for pod resources
type Controller struct {
	kubeclientset  kubernetes.Interface
	namespace      string
	kubeconfig     string
	remoteKubeClientset kubernetes.Interface
	remoteKubeconfig string
	//need this for cleanup to save pod data ps             *PodStore
	queue          workqueue.RateLimitingInterface
	informer       cache.SharedIndexInformer
}


// NewController returns a new pod controller
func newController(
	kubeclientset kubernetes.Interface,
	namespace string, kubeconfig string,
	remoteKubeClientset kubernetes.Interface,
	remoteKubeconfig string) *Controller {

	podsInformer := cache.NewSharedIndexInformer(
		&cache.ListWatch{
			ListFunc: func(opts meta_v1.ListOptions) (runtime.Object, error) {
				opts.LabelSelector = svcLabel
				return kubeclientset.CoreV1().Pods(namespace).List(opts)
			},
			WatchFunc: func(opts meta_v1.ListOptions) (watch.Interface, error) {
				opts.LabelSelector = svcLabel
				return kubeclientset.CoreV1().Pods(namespace).Watch(opts)
			},
		},
		&corev1.Pod{}, 0, cache.Indexers{},
	)

	queue := workqueue.NewRateLimitingQueue(workqueue.DefaultControllerRateLimiter())

	controller := &Controller{
		kubeclientset:  kubeclientset,
		namespace:      namespace,
		informer:       podsInformer,
		queue:          queue,
		kubeconfig:     kubeconfig,
		remoteKubeClientset: remoteKubeClientset,
		remoteKubeconfig:	remoteKubeconfig,
	}

	log.Info("Setting up event handlers")
	podsInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			key, err := cache.MetaNamespaceKeyFunc(obj)
			log.Infof("Processing add: %s", key)
			if err == nil {
				queue.Add(key)
			}
		},
		DeleteFunc: func(obj interface{}) {
			key, err := cache.DeletionHandlingMetaNamespaceKeyFunc(obj)
			log.Infof("Processing delete: %s", key)
			if err == nil {
				queue.Add(key)
			}
		},
	})

	return controller
}

// Run starts the controller until it receives a message over stopCh
func (c *Controller) Run(stopCh <-chan struct{}) {
	defer utilruntime.HandleCrash()
	defer c.queue.ShutDown()

	log.Info("Starting Pod watcher controller")

	go c.informer.Run(stopCh)

	// Wait for the caches to be synced before starting workers
	log.Info("Waiting for informer caches to sync")
	if !cache.WaitForCacheSync(stopCh, c.informer.HasSynced) {
		utilruntime.HandleError(fmt.Errorf("timed out waiting for caches to sync"))
		return
	}
	wait.Until(c.runWorker, 5*time.Second, stopCh)
}

func (c *Controller) runWorker() {
	for c.processNextItem() {
	}
}

func (c *Controller) processNextItem() bool {
	podName, quit := c.queue.Get()

	if quit {
		return false
	}
	defer c.queue.Done(podName)

	err := c.processItem(podName.(string))
	if err == nil {
		// No error, reset the ratelimit counters
		c.queue.Forget(podName)
	} else if c.queue.NumRequeues(podName) < maxRetries {
		log.Errorf("Error processing %s (will retry): %v", podName, err)
		c.queue.AddRateLimited(podName)
	} else {
		log.Errorf("Error processing %s (giving up): %v", podName, err)
		c.queue.Forget(podName)
		utilruntime.HandleError(err)
	}

	return true
}

func (c *Controller) processItem(podName string) error {
	obj, exists, err := c.informer.GetIndexer().GetByKey(podName)
	if err != nil {
		return fmt.Errorf("error fetching object %s error: %v", podName, err)
	}

	if exists {
		c.addPod(podName, obj.(*corev1.Pod))
	} else {
		// FIXME pod maybe gone so can't pass Pod in this function
		//c.deletePod(podName, obj.(*corev1.Pod))
		c.deletePod(podName)
	}

	return nil
}

func (c *Controller) addPod(PodName string, pod *corev1.Pod) {

	podHandle := pod.Name
	log.Infof("Add Pod name = %s ", podHandle)
	svcName, portNumber := c.checkSVCName(pod)
	log.Infof("Service name = %s, Port Number = %d", svcName, portNumber)
	var endPoint string
	if svcName != "" {
		endPoint = c.getPodEndpoint(pod)
		log.Infof("Endpoint = %s", endPoint)
		if endPoint != "" {
			c.createSVC(svcName, portNumber)
			c.createEP(svcName, portNumber, endPoint)
		}
	}
}

//func (c *Controller) deletePod(PodName string, s *corev1.Pod) {
func (c *Controller) deletePod(PodName string) {
	log.Infof("FIXME need to delete Pod name = %s ", PodName)
}

func (c *Controller) checkSVCName(pod *corev1.Pod) (string, int32) {
	var svcName string
	var portNumber int32
	var intportNumber int
	var err error
	for label, value := range pod.GetLabels() {
		if label == svcLabel {
			svcName = value
		}
		if label == portLabel {
			intportNumber, err = strconv.Atoi(value)
			portNumber = int32(intportNumber)
			if err != nil {
				return "", 0
			}
		}
	}
	return svcName, portNumber
}


func (c *Controller) getPodEndpoint(pod *corev1.Pod) string {
	podHandle := pod.Name
	cmd := "ip -o addr list"
	output, err:= util.ShellSilent("kubectl exec %s --kubeconfig=%s -- %s | grep %s", podHandle, c.kubeconfig, cmd, ifName)
	if err != nil{
		for i := 1;  i<=20; i++ {
			time.Sleep(30)
			output, err = util.ShellSilent("kubectl exec %s --kubeconfig=%s -- %s | grep %s", podHandle, c.kubeconfig, cmd, ifName)
		}
	}
	if err != nil {
		log.Infof("failed to display interface (error %v)", err)
		return ""
	}
	ipString := findIP(output)
	return ipString
}

func (c *Controller) testSVC() {

	getOpt := meta_v1.GetOptions{IncludeUninitialized: true}
	//testEPS, err := c.remoteKubeClientset.CoreV1().Endpoints(namespace).Get("helloworld", getOpt)
	testSVC, err := c.remoteKubeClientset.CoreV1().Services(namespace).Get("helloworld", getOpt)
	if err == nil{
		log.Infof("helloworld = %v ", testSVC)
	}

		_, err = c.remoteKubeClientset.CoreV1().Services(namespace).Create(&corev1.Service{
		ObjectMeta: meta_v1.ObjectMeta{Name: "http-example"},
		Spec: corev1.ServiceSpec{
			Ports: []corev1.ServicePort{
				{
					Port:     80,
					Name:     "http-example",
					Protocol: corev1.ProtocolTCP,
				},
			},
		},
	})

}

func (c *Controller) createSVC(svcName string, portNumber int32) error {

	getOpt := meta_v1.GetOptions{IncludeUninitialized: true}

	_, err := c.remoteKubeClientset.CoreV1().Services(namespace).Get(svcName, getOpt)
	if err == nil {
		log.Infof("Service already exists skipping creation")
	}

	_, err = c.remoteKubeClientset.CoreV1().Services(namespace).Create(&corev1.Service{
		ObjectMeta: meta_v1.ObjectMeta{Name: svcName},
		Spec: corev1.ServiceSpec{
			Ports: []corev1.ServicePort{
				{
					Port:     portNumber,
					Name:     svcName,
					//Protocol: corev1.ProtocolTCP,
				},
			},
		},
	})

	if err != nil {
		// Note failure to create the service when it already exists is not realy a error
		log.Errorf("failed to create service (error %v)", err)
		return err
	}

	return nil
}

func (c *Controller) createEP(svcName string, portNumber int32, endPoint string) error {

        //FIXME This functions needs to be updated to append to the subset if it already exists. 
	eas := make([]corev1.EndpointAddress, 0)
	eas = append(eas, corev1.EndpointAddress{IP: endPoint})
	eps := make([]corev1.EndpointPort,0)
	eps = append(eps, corev1.EndpointPort{Name: svcName, Port: portNumber})

	endpoint := &corev1.Endpoints{
		ObjectMeta: meta_v1.ObjectMeta{
			Name:      svcName,
		},
		Subsets: []corev1.EndpointSubset{{
			Addresses: eas,
			Ports:     eps,
		}},
	}

	if _, err := c.remoteKubeClientset.CoreV1().Endpoints(namespace).Create(endpoint); err != nil {
		log.Errorf("failed to create endpoints (error %v)", err)
		return err
	}

	return nil
}

func findIP(input string) string {
	numBlock := "(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9]?[0-9])"
	regexPattern := numBlock + "\\." + numBlock + "\\." + numBlock + "\\." + numBlock
	regEx := regexp.MustCompile(regexPattern)

	return regEx.FindString(input)
}

