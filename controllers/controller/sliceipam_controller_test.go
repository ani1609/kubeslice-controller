/*
 * 	Copyright (c) 2022 Avesha, Inc. All rights reserved. # # SPDX-License-Identifier: Apache-2.0
 *
 * 	Licensed under the Apache License, Version 2.0 (the "License");
 * 	you may not use this file except in compliance with the License.
 * 	You may obtain a copy of the License at
 *
 * 	http://www.apache.org/licenses/LICENSE-2.0
 *
 * 	Unless required by applicable law or agreed to in writing, software
 * 	distributed under the License is distributed on an "AS IS" BASIS,
 * 	WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * 	See the License for the specific language governing permissions and
 * 	limitations under the License.
 */

package controller

import (
	"context"

	"github.com/kubeslice/kubeslice-controller/apis/controller/v1alpha1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

const (
	sliceIpamName = "test-slice-ipam"
)

var _ = Describe("SliceIpam controller", func() {
	Context("When creating SliceIpam CR", func() {
		It("Should successfully create and reconcile", func() {
			By("Creating a new SliceIpam CR")
			ctx := context.Background()

			sliceIpam := &v1alpha1.SliceIpam{
				ObjectMeta: metav1.ObjectMeta{
					Name:      sliceIpamName,
					Namespace: controlPlaneNamespace,
				},
				Spec: v1alpha1.SliceIpamSpec{
					SliceName:   sliceIpamName,
					SliceSubnet: "10.1.0.0/16",
					SubnetSize:  24,
				},
				Status: v1alpha1.SliceIpamStatus{
					TotalSubnets:     256,
					AvailableSubnets: 256,
					AllocatedSubnets: []v1alpha1.ClusterSubnetAllocation{},
					LastUpdated:      metav1.Now(),
				},
			}

			Expect(k8sClient.Create(ctx, sliceIpam)).Should(Succeed())

			By("Looking up the created SliceIpam CR")
			sliceIpamLookupKey := types.NamespacedName{
				Name:      sliceIpamName,
				Namespace: controlPlaneNamespace,
			}
			createdSliceIpam := &v1alpha1.SliceIpam{}
			Eventually(func() bool {
				err := k8sClient.Get(ctx, sliceIpamLookupKey, createdSliceIpam)
				return err == nil
			}, timeout, interval).Should(BeTrue())

			By("Checking the SliceIpam spec is set correctly")
			Expect(createdSliceIpam.Spec.SliceName).To(Equal(sliceIpamName))
			Expect(createdSliceIpam.Spec.SliceSubnet).To(Equal("10.1.0.0/16"))
			Expect(createdSliceIpam.Spec.SubnetSize).To(Equal(24))

			By("Checking that finalizers are added")
			Eventually(func() bool {
				err := k8sClient.Get(ctx, sliceIpamLookupKey, createdSliceIpam)
				if err != nil {
					return false
				}
				return len(createdSliceIpam.GetFinalizers()) > 0
			}, timeout, interval).Should(BeTrue())

			By("Cleaning up the SliceIpam CR")
			Eventually(func() bool {
				err := k8sClient.Delete(ctx, createdSliceIpam)
				return err == nil
			}, timeout, interval).Should(BeTrue())
		})
	})
})