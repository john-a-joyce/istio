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

package bootstrap

import (
	"istio.io/pkg/log"
	"regexp"
	//"strconv"
)



func getPodEndpoint() int {
        test_output := `Defaulting container name to busybox.
Use kubectl describe pod/busybox -n default to see all of the containers in this pod.
eth0      Link encap:Ethernet  HWaddr 6A:11:BD:14:D8:45  
          inet addr:192.168.160.216  Bcast:0.0.0.0  Mask:255.255.255.255
          UP BROADCAST RUNNING MULTICAST  MTU:1500  Metric:1
          RX packets:173461 errors:0 dropped:0 overruns:0 frame:0
          TX packets:168872 errors:0 dropped:0 overruns:0 carrier:0
          collisions:0 txqueuelen:0 
          RX bytes:20399340 (19.4 MiB)  TX bytes:130026479 (124.0 MiB)
`

	//match :=  regexp.MustCompile("inet addr: (\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3})").FindStringSubmatch(output)
	//match :=  regexp.MustCompile("inet addr:(\\d+),").FindStringSubmatch(output)

	match :=  regexp.MustCompile("inet addr:(\\d) ").FindStringSubmatch(test_output)
	log.Infof("JAJ the match0 ip is %v", match)
	match =  regexp.MustCompile("inet addr:(\\d+) ").FindStringSubmatch(test_output)
	log.Infof("JAJ the match1 ip is %v", match)
	match =  regexp.MustCompile("inet addr:(\\d+) ").FindStringSubmatch(test_output)
	log.Infof("JAJ the match2 ip is %v", match)
	//match =  regexp.MustCompile("inet addr:([0-9][1-9][0-9]|1[0-9]{2}|2[0-4][0-9]|25[0-5])\.){3}([0-9]|[1-9][0-9]|1[0-9]{2}|2[0-4][0-9]|25[0-5])``) ").FindStringSubmatch(test_output)
	//log.Infof("JAJ the match3 ip is %v", match[0])

	//regexp.Compile(`^(([0-9]|[1-9][0-9]|1[0-9]{2}|2[0-4][0-9]|25[0-5])\.){3}([0-9]|[1-9][0-9]|1[0-9]{2}|2[0-4][0-9]|25[0-5])$`)
	//log.Infof("JAJ the match ip is %v", match[0])
	//ip := 0
	//if (match != nil) {
	//	if i, err := strconv.Atoi(match[1]); err == nil {
	//		ip = i
	//	}
	//}
	//log.Infof("JAJ the ip is %d", ip)
	//if err == nil {
	//	ip := output
	//	log.Infof("JAJ the ip is %s", ip)
	//	ip = "10.10.10.10"
	//	return ip
	//}
	return 0
}
