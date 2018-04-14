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

package framework

import (
	"io/ioutil"
	"os"
	"path/filepath"

	"k8s.io/api/core/v1"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"istio.io/istio/pkg/log"
	"k8s.io/client-go/kubernetes"
	"time"
)

func getKubeConfigFromFile(dirname string) (string, error) {
	// The tests assume that only a single remote cluster (i.e. a single file) is in play

	var remoteKube string
	err := filepath.Walk(dirname, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if (info.Mode() & os.ModeType) != 0 {
			return nil
		}
		_, err = ioutil.ReadFile(path)
		if err != nil {
			log.Warnf("Failed to read %s: %v", path, err)
			return err
		}
		remoteKube = path
		return nil
	})
	if err != nil {
		return "", nil
	}
	return remoteKube, nil
}

// createMultiClusterSecrets will create the secrets and configmap associated with the remote cluster
func createMultiClusterSecrets(namespace string, KubeClient kubernetes.Interface, RemoteKubeConfig string) (error) {
	_, err := ShellMuteOutput("kubectl config view --raw=true --minify=true > %s", filename)
	if err != nil {
		return err
	}
	log.Infof("kubeconfig file %s created\n", filename)
	return nil


	if _, err := KubeClient.CoreV1().ConfigMaps(namespace).Create(&v1.ConfigMap{
		ObjectMeta: meta_v1.ObjectMeta{
			Name: "istio-inject",
		},
		Data: map[string]string{
			"config": string(configData),
		},
	}); err != nil {
		return err
	}
	if _, err := KubeClient.CoreV1().Secrets(namespace).Create(&v1.Secret{
		TypeMeta: meta_v1.TypeMeta{
			Kind:       "",
			APIVersion: "",
		},
		ObjectMeta: meta_v1.ObjectMeta{
			Name:            "",
			GenerateName:    "",
			Namespace:       "",
			SelfLink:        "",
			UID:             "",
			ResourceVersion: "",
			Generation:      0,
			CreationTimestamp: meta_v1.Time{
				Time: time.Time{},
			},
			DeletionTimestamp: &meta_v1.Time{
				Time: time.Time{},
			},
			DeletionGracePeriodSeconds: nil,
			Labels:                     nil,
			Annotations:                nil,
			OwnerReferences:            nil,
			Initializers: &meta_v1.Initializers{
				Pending: nil,
				Result: &meta_v1.Status{
					TypeMeta: meta_v1.TypeMeta{
						Kind:       "",
						APIVersion: "",
					},
					ListMeta: meta_v1.ListMeta{
						SelfLink:        "",
						ResourceVersion: "",
						Continue:        "",
					},
					Status:  "",
					Message: "",
					Reason:  "",
					Details: &meta_v1.StatusDetails{
						Name:              "",
						Group:             "",
						Kind:              "",
						UID:               "",
						Causes:            nil,
						RetryAfterSeconds: 0,
					},
					Code: 0,
				},
			},
			Finalizers:  nil,
			ClusterName: "",
		},
		Data:       nil,
		StringData: nil,
		Type:       "",
	}); err != nil {

	}
	if _, err = KubeClient.CoreV1().ConfigMaps(e.Config.IstioNamespace).Create(&v1.ConfigMap{

		ObjectMeta: meta_v1.ObjectMeta{
			Name: "multicluster",
		},
		Data: configData,
	}); err != nil {
		return err
	}
}
}