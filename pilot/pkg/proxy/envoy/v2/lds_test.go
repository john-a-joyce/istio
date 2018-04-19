// Copyright 2018 Istio Authors
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
package v2_test

import (
	"context"
	"testing"

	xdsapi "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	envoy_api_v2_core1 "github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	"google.golang.org/grpc"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/tests/util"
)

func connectLDS(url string, t *testing.T) xdsapi.ListenerDiscoveryService_StreamListenersClient {
	conn, err := grpc.Dial(util.MockPilotGrpcAddr, grpc.WithInsecure())
	if err != nil {
		t.Fatal("Connection failed", err)
	}

	xds := xdsapi.NewListenerDiscoveryServiceClient(conn)
	ldsstr, err := xds.StreamListeners(context.Background())
	if err != nil {
		t.Fatal("Rpc failed", err)
	}
	err = ldsstr.Send(&xdsapi.DiscoveryRequest{
		Node: &envoy_api_v2_core1.Node{
			Id: "sidecar~10.1.10.1~b~c",
		},
	})
	if err != nil {
		t.Fatal("Send failed", err)
	}
	return ldsstr
}

// Regression for envoy restart and overlapping connections
func TestLDS(t *testing.T) {
	initMocks()

	ldsr := connectLDS(util.MockPilotGrpcAddr, t)

	res, err := ldsr.Recv()
	if err != nil {
		t.Fatal("Failed to receive LDS", err)
		return
	}

	strResponse, _ := model.ToJSONWithIndent(res, " ")

	t.Log("LDS response", strResponse)
	if len(res.Resources) == 0 {
		t.Fatal("No response")
	}

	// TODO: dump the response resources, compare with some golden once it's stable
	// check that each mocked service and destination rule has a corresponding resource

	// TODO: dynamic checks ( see EDS )
}
