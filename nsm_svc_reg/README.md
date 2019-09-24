# Istio + NSM POC procedure                                                                                    
                                                                                                               
## NSM Install                                                                                                     
Install NSM per Tim’s directions. Most recent steps followed were:

```bash                                                                                                                   
helm template deployments/helm/nsm --namespace nsm-system --set                                                    
global.JaegerTracing=true --set org=tiswanso --set tag=vl3-inter-domain --set                                      
pullPolicy=Always --set admission-webhook.org=tiswanso --set
admission-webhook.tag=vl3-inter-domain --set admission-webhook.pullPolicy=Always
--set global.NSRegistrySvc=true --set global.NSMApiSvc=true --set
global.NSMApiSvcPort=30501 --set global.NSMApiSvcAddr="0.0.0.0:30501" --set
global.NSMApiSvcType=NodePort --set global.ExtraDnsServers='10.96.0.10\
10.87.49.210:20853' > ~/tmp/nsm-vl3-interd.yaml

kubectl apply -f ~/tmp/nsm_k8s_nsm_monitoring.yaml
kubectl apply -f $HOME/tmp/nsm-vl3-interd.yaml
```

## Istio CNI Install
Install the Istio-cni and use the excludeNamespaces to exclude the “nsm-system” namespace 

Followed the steps here:
https://istio.io/docs/setup/kubernetes/additional-setup/cni/
Note: although the nsm-system namespace should be excluded, it isn’t that critical. 
The envoy sidecar injection only occurs on the namespaces that are labeled
appropriately.  Don't label the nsm-system namespace to be injected.

## Istio Install
The procedure to install Istio should not matter the
steps here were followed:  https://istio.io/docs/setup/kubernetes/install/helm/
Specific commands used:
```
helm template install/kubernetes/helm/istio --name istio --namespace
istio-system     --set istio_cni.enabled=true | kubectl apply -f -
```
Sidecar injection needs to configure for the client pods (but not for the NSE
pods).  Do this by directly adding:
“Annotations:        sidecar.istio.io/inject: true” to the client pods.  
To get better visibility from Envoy edit the sidecar injector configmap to use the
proxy_debug image instead of proxyv2.   Alternatively you could adjust the yaml
on install or helm value if generating the yaml.   Additionally, edit the Istio
configmap to change the accessfilelocation (think that was the field) and set it
to /dev/stdout.  Without changing the accessfilelocation the envoy logs won't be available.

## Installing an NSM example
Installation of the NSM example pods need to occur after the Istio install.
In this Poc it was done at ths step but it could be done after multicluster is set up.   
Follow Tim directions for the install this POC used the following steps originally: 
```
git checkout tims_vl3_branch
make build-vl3
make docker-vl3 
kubectl apply -f examples/vl3_basic/k8s/vl3-client.yaml
kubectl apply -f examples/vl3_basic/k8s/vl3-nse.yaml
```
More recently used the images directly from Tim's repo as specified in his branch.
```
kubectl apply -f examples/vl3_basic/k8s/vl3-nse-ucnf.yaml
kubectl apply -f examples/vl3_basic/k8s/vl3-client.yaml
```

## KIND Install 
For KIND just follow the steps from the KIND repo.  The steps are
here:
https://github.com/kubernetes-sigs/kind
Exposing UDP ports (need for DNS) was a recent change and required KIND version 0.5 or later. 
This POC used 0.5.1. The install needs to be customized for a number of reasons:
1. Open up additional ports so Pilot can reach the API server in the KIND cluster
1. to change the kubeadm cert set-up to allow connections from the local host address
1. to expose the DNS service. 
These are the changes down for this POC (10.87.49.210 is your Nodes IP)

```
kind: Cluster
apiVersion: kind.sigs.k8s.io/v1alpha3
kubeadmConfigPatchesJson6902:
- group: kubeadm.k8s.io
  version: v1beta2
  kind: ClusterConfiguration
  patch: |
    - op: add
      path: /apiServer/certSANs/-
      value: 10.87.49.210
# 1 control plane node and 3 workers
nodes:
- role: control-plane
  extraPortMappings:
  - containerPort: 6443
    hostPort: 38782
    listenAddress: "10.87.49.210"  
   -containerPort:  30653
    hostPort: 30653
    protocol: udp
    listenAddress: "10.87.49.210"
  - containerPort:  30853
    hostPort: 30853
    listenAddress: "10.87.49.210"
  - containerPort:  31500
    hostPort: 31500
    listenAddress: "10.87.49.210"
```

You also need to the delete the kube-dns service in the cluster and recreate it
as follows:
```
apiVersion: v1
kind: Service
metadata:
  annotations:
    prometheus.io/port: "9153"
    prometheus.io/scrape: "true"
  labels:
    k8s-app: kube-dns
    kubernetes.io/cluster-service: "true"
    kubernetes.io/name: KubeDNS
  name: kube-dns
  namespace: kube-system
  resourceVersion: "201"
spec:
  ports:
  - name: dns
    port: 53
    protocol: UDP
    nodePort: 30653
  - name: dns-tcp
    port: 53
    protocol: TCP
    nodePort: 30853
  - name: metrics
    port: 9153
    protocol: TCP
    nodePort: 30153
  selector:
    k8s-app: kube-dns
  sessionAffinity: None
  type: NodePort
```

## Multicluster
The multicluster setup was a bit trickier.   Originally the Istio
multicluster setup instructions here were tried:
https://istio.io/docs/setup/kubernetes/install/multicluster/shared-vpn/
The Istio-remote yaml should not be used directly on the KIND cluster as you 
don’t want to install all of Istio there you just want an API server.    
Tried to modify the Istio-remote to just include everything related to the Istio-multi SA. 
This was not successful getting pilot to connect to the the KIND API server with
this approach.  
This POC ultimately used the following steps:
1.  Made a copy of the kubeconfig file KIND created
1.  Changed the server address to be the nodes address (10.87.49.210)
Note it matches the Kind listenAddress from above. 
1.  Created the secret in the Istio cluster per multicluster directions
1.  Labelled the secret per directions. 


## Controller to watch pods
A controller was written to watch pods with a specific set of labels and create 
services and endpoints for those pods.   This is to setup service discover to point
to the NSM interfaces instead of the CNI interfaces.  

Any application workloads wishing to be reached by the NSM interfaces should NOT
create kubernetes services via the normal way as the endpoitns will point to the 
CNI interface address.   Instead use the lables indicated below to drive service and
endpoint creation pointing to the NSM interfaces.   

This controller will be packaged as a POD shortly.   

In the command below the kubeconfig is the cluster where the NSM application workloads
will be installed.   kubeconfigremote is the k8s API server that Istio multicluster is 
watching for service and endpoints. 

Available here: https://github.com/john-a-joyce/istio/tree/nsm_play
run it like this: from  goland 
"pod-watcher --kubeconfigremote=/root/.kube/kind-config-kind_dns
--kubeconfig=/root/.kube/config"  

Add the following labels to the pods for it to create services and
endpoints:

Labels:             nsm/servicename=helloworld
                    nsm/serviceport=5000

