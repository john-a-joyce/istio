Requirements to run this code.  

A system with a kubernetes environment and valid Kube context

The config context from a GKE cluster that will be used as the main/local cluster
The config context from a GKE cluster that will be used as the remote

Your on your own for the initial requiremenets to properly connect and use GKE. 

Put those config files in an emptry directory.  Name one as local
the name of the other file is flexible.  

Assume cluster directory name is:  /root/kubecon_clu 

git clone this repo.  

Set you TAG and HUB values
echo $TAG
kubeconfig_fix
echo $HUB
docker.io/johnajoyce

Images were built for above HUB and TAG,  but you can always build
your own if you wish.  

Run:
```
  make clean
  make init
  make build
```
To prime the test environment.   

Due to a but of quirkyness that is not yet fix you need to have valid istio-system configmaps installed
on your local cluster. The test code wasn't updated to use the proper configmap for kube-inject to work

Execute the following commands to create cluster roles on the GKE clusters. 
```
kubectl create clusterrolebinding prow-cluster-admin-binding    --clusterrole=cluster-admin    --user="johnajoyce17@gmail.com" --kubeconfig=/root/kubecon_clu/local
kubectl create clusterrolebinding prow-cluster-admin-binding    --clusterrole=cluster-admin    --user="johnajoyce17@gmail.com" --kubeconfig=/root/kubecon_clu/remote
```
run the following command on your local system from the base of the git tree i.e. "istio"
```
make e2e_bookinfo E2E_ARGS="--skip_cleanup --cluster_registry_dir=/root/kubecon_clu --namespace=istio-system" | tee $HOME/pilot_log
```
Note the directory name used in the command.  

Then the Istio componets should be setup on both the local and remote while the bookinfo app should be only on the local. 

Need to ensure that istio-citadel is using the same root cert by following the instructions here:
https://istio.io/docs/tasks/security/plugin-ca-cert.html
