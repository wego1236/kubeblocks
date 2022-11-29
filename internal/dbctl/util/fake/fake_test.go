/*
Copyright ApeCloud Inc.

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

package fake

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("test fake", func() {
	It("cluster", func() {
		cluster := Cluster(ClusterName, Namespace)
		Expect(cluster).ShouldNot(BeNil())
		Expect(cluster.Name).Should(Equal(ClusterName))
	})

	It("cluster definition", func() {
		clusterDef := ClusterDef()
		Expect(clusterDef).ShouldNot(BeNil())
		Expect(clusterDef.Name).Should(Equal(ClusterDefName))
	})

	It("cluster definition", func() {
		appVersion := Appversion()
		Expect(appVersion).ShouldNot(BeNil())
		Expect(appVersion.Name).Should(Equal(AppVersionName))
	})

	It("pods", func() {
		pods := Pods(3, Namespace, ClusterName)
		Expect(pods).ShouldNot(BeNil())
		Expect(len(pods.Items)).Should(Equal(3))
	})

	It("secrets", func() {
		secrets := Secrets(Namespace, ClusterName)
		Expect(secrets).ShouldNot(BeNil())
		Expect(len(secrets.Items)).Should(Equal(1))
	})

	It("services", func() {
		svcs := Services()
		Expect(svcs).ShouldNot(BeNil())
		Expect(len(svcs.Items)).Should(Equal(4))
	})

	It("node", func() {
		node := Node()
		Expect(node).ShouldNot(BeNil())
		Expect(node.Name).Should(Equal(NodeName))
	})

	It("fake client set", func() {
		client := NewClientSet()
		Expect(client).ShouldNot(BeNil())
	})

	It("fake dynamic set", func() {
		dynamic := NewDynamicClient()
		Expect(dynamic).ShouldNot(BeNil())
	})
})