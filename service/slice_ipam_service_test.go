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

package service

import (
	"testing"

	"github.com/kubeslice/kubeslice-controller/apis/controller/v1alpha1"
	metricMock "github.com/kubeslice/kubeslice-controller/metrics/mocks"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestNewSliceIpamService(t *testing.T) {
	// Setup
	mockMetrics := &metricMock.IMetricRecorder{}

	// Test service creation
	service := NewSliceIpamService(mockMetrics)

	// Assertions
	require.NotNil(t, service)
	require.NotNil(t, service.mf)
	require.NotNil(t, service.allocator)
	require.Equal(t, mockMetrics, service.mf)
}

func TestSliceIpamService_InterfaceCompliance(t *testing.T) {
	mockMetrics := &metricMock.IMetricRecorder{}
	var service ISliceIpamService = NewSliceIpamService(mockMetrics)

	require.NotNil(t, service)
	// Verify interface methods exist by accessing them
	require.NotNil(t, service.ReconcileSliceIpam)
	require.NotNil(t, service.AllocateSubnetForCluster)
	require.NotNil(t, service.ReleaseSubnetForCluster)
	require.NotNil(t, service.GetClusterSubnet)
	require.NotNil(t, service.CreateSliceIpam)
	require.NotNil(t, service.DeleteSliceIpam)
}

func TestSliceIpamService_CreateSliceIpamValidation(t *testing.T) {
	mockMetrics := &metricMock.IMetricRecorder{}
	service := NewSliceIpamService(mockMetrics)

	// Test with nil slice config
	require.NotNil(t, service)

	// Test with valid slice config
	sliceConfig := &v1alpha1.SliceConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-slice",
			Namespace: "test-namespace",
		},
		Spec: v1alpha1.SliceConfigSpec{
			SliceSubnet: "10.1.0.0/16",
		},
	}

	require.NotNil(t, sliceConfig)
}

// SliceIpamServiceTestBed contains test scenarios for the SliceIpamService
var SliceIpamServiceTestBed = map[string]func(t *testing.T){
	"TestSliceIpamService_Constructor": func(t *testing.T) {
		mockMetrics := &metricMock.IMetricRecorder{}
		service := NewSliceIpamService(mockMetrics)

		require.NotNil(t, service)
		require.Equal(t, mockMetrics, service.mf)
		require.NotNil(t, service.allocator)
	},
	"TestSliceIpamService_Interface": func(t *testing.T) {
		mockMetrics := &metricMock.IMetricRecorder{}
		var service ISliceIpamService = NewSliceIpamService(mockMetrics)

		require.NotNil(t, service)
		// Verify interface methods exist
		require.NotNil(t, service.ReconcileSliceIpam)
		require.NotNil(t, service.AllocateSubnetForCluster)
		require.NotNil(t, service.ReleaseSubnetForCluster)
		require.NotNil(t, service.GetClusterSubnet)
		require.NotNil(t, service.CreateSliceIpam)
		require.NotNil(t, service.DeleteSliceIpam)
	},
}

func TestSliceIpamServiceSuite(t *testing.T) {
	for name, testFunc := range SliceIpamServiceTestBed {
		t.Run(name, testFunc)
	}
}
