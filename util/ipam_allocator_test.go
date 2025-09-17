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

package util

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

func TestNewIpamAllocator(t *testing.T) {
	allocator := NewIpamAllocator()
	if allocator == nil {
		t.Fatal("Expected non-nil allocator")
	}
	if allocator.cache == nil {
		t.Fatal("Expected non-nil cache")
	}
	if len(allocator.cache) != 0 {
		t.Fatal("Expected empty cache")
	}
}

func TestValidateSliceSubnet(t *testing.T) {
	allocator := NewIpamAllocator()

	tests := []struct {
		name        string
		subnet      string
		expectError bool
		errorMsg    string
	}{
		{
			name:        "valid-private-subnet",
			subnet:      "10.1.0.0/16",
			expectError: false,
		},
		{
			name:        "valid-private-subnet-192",
			subnet:      "192.168.1.0/24",
			expectError: false,
		},
		{
			name:        "valid-private-subnet-172",
			subnet:      "172.16.0.0/20",
			expectError: false,
		},
		{
			name:        "empty-subnet",
			subnet:      "",
			expectError: true,
			errorMsg:    "slice subnet cannot be empty",
		},
		{
			name:        "invalid-cidr-format",
			subnet:      "not-a-cidr",
			expectError: true,
			errorMsg:    "invalid CIDR format",
		},
		{
			name:        "invalid-cidr-mask",
			subnet:      "10.1.0.0/33",
			expectError: true,
			errorMsg:    "invalid CIDR format",
		},
		{
			name:        "ipv6-subnet",
			subnet:      "2001:db8::/64",
			expectError: true,
			errorMsg:    "only IPv4 subnets are supported",
		},
		{
			name:        "public-subnet",
			subnet:      "8.8.8.0/24",
			expectError: true,
			errorMsg:    "slice subnet must be from private IP ranges",
		},
		{
			name:        "subnet-too-small",
			subnet:      "10.1.0.0/30",
			expectError: true,
			errorMsg:    "slice subnet is too small",
		},
		{
			name:        "subnet-too-large",
			subnet:      "10.0.0.0/7",
			expectError: true,
			errorMsg:    "slice subnet is too large",
		},
		{
			name:        "non-network-address",
			subnet:      "10.1.0.1/24",
			expectError: true,
			errorMsg:    "slice subnet must be a network address",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := allocator.ValidateSliceSubnet(tt.subnet)
			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error for subnet %s, but got none", tt.subnet)
				} else if tt.errorMsg != "" && !containsString(err.Error(), tt.errorMsg) {
					t.Errorf("Expected error message to contain '%s', got '%s'", tt.errorMsg, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("Expected no error for subnet %s, but got: %v", tt.subnet, err)
				}
			}
		})
	}
}

func TestCalculateMaxClusters(t *testing.T) {
	allocator := NewIpamAllocator()

	tests := []struct {
		name        string
		sliceSubnet string
		subnetSize  int
		expectError bool
		expectedMax int
	}{
		{
			name:        "valid-16-to-24",
			sliceSubnet: "10.1.0.0/16",
			subnetSize:  24,
			expectError: false,
			expectedMax: 256, // 2^(24-16) = 2^8 = 256
		},
		{
			name:        "valid-20-to-24",
			sliceSubnet: "10.1.0.0/20",
			subnetSize:  24,
			expectError: false,
			expectedMax: 16, // 2^(24-20) = 2^4 = 16
		},
		{
			name:        "valid-24-to-26",
			sliceSubnet: "192.168.1.0/24",
			subnetSize:  26,
			expectError: false,
			expectedMax: 4, // 2^(26-24) = 2^2 = 4
		},
		{
			name:        "invalid-slice-subnet",
			sliceSubnet: "invalid",
			subnetSize:  24,
			expectError: true,
		},
		{
			name:        "subnet-size-too-small",
			sliceSubnet: "10.1.0.0/16",
			subnetSize:  15,
			expectError: true,
		},
		{
			name:        "subnet-size-too-large",
			sliceSubnet: "10.1.0.0/16",
			subnetSize:  31,
			expectError: true,
		},
		{
			name:        "subnet-size-equal-to-slice",
			sliceSubnet: "10.1.0.0/24",
			subnetSize:  24,
			expectError: true,
		},
		{
			name:        "subnet-size-smaller-than-slice",
			sliceSubnet: "10.1.0.0/24",
			subnetSize:  20,
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			maxClusters, err := allocator.CalculateMaxClusters(tt.sliceSubnet, tt.subnetSize)
			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error for slice %s with subnet size %d, but got none", tt.sliceSubnet, tt.subnetSize)
				}
			} else {
				if err != nil {
					t.Errorf("Expected no error for slice %s with subnet size %d, but got: %v", tt.sliceSubnet, tt.subnetSize, err)
				} else if maxClusters != tt.expectedMax {
					t.Errorf("Expected max clusters %d, got %d", tt.expectedMax, maxClusters)
				}
			}
		})
	}
}

func TestGenerateSubnetList(t *testing.T) {
	allocator := NewIpamAllocator()

	tests := []struct {
		name                string
		sliceSubnet         string
		subnetSize          int
		expectError         bool
		expectedCount       int
		expectedFirstSubnet string
		expectedLastSubnet  string
	}{
		{
			name:                "small-range-24-to-26",
			sliceSubnet:         "192.168.1.0/24",
			subnetSize:          26,
			expectError:         false,
			expectedCount:       4,
			expectedFirstSubnet: "192.168.1.0/26",
			expectedLastSubnet:  "192.168.1.192/26",
		},
		{
			name:                "medium-range-20-to-24",
			sliceSubnet:         "10.1.0.0/20",
			subnetSize:          24,
			expectError:         false,
			expectedCount:       16,
			expectedFirstSubnet: "10.1.0.0/24",
			expectedLastSubnet:  "10.1.15.0/24",
		},
		{
			name:        "invalid-slice-subnet",
			sliceSubnet: "invalid",
			subnetSize:  24,
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			subnets, err := allocator.GenerateSubnetList(tt.sliceSubnet, tt.subnetSize)
			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error for slice %s with subnet size %d, but got none", tt.sliceSubnet, tt.subnetSize)
				}
			} else {
				if err != nil {
					t.Errorf("Expected no error for slice %s with subnet size %d, but got: %v", tt.sliceSubnet, tt.subnetSize, err)
				} else {
					if len(subnets) != tt.expectedCount {
						t.Errorf("Expected %d subnets, got %d", tt.expectedCount, len(subnets))
					}
					if len(subnets) > 0 {
						if subnets[0] != tt.expectedFirstSubnet {
							t.Errorf("Expected first subnet %s, got %s", tt.expectedFirstSubnet, subnets[0])
						}
						if subnets[len(subnets)-1] != tt.expectedLastSubnet {
							t.Errorf("Expected last subnet %s, got %s", tt.expectedLastSubnet, subnets[len(subnets)-1])
						}
					}

					// Ensure no duplicates
					seen := make(map[string]bool)
					for _, subnet := range subnets {
						if seen[subnet] {
							t.Errorf("Duplicate subnet found: %s", subnet)
						}
						seen[subnet] = true
					}
				}
			}
		})
	}
}

func TestFindNextAvailableSubnet(t *testing.T) {
	allocator := NewIpamAllocator()

	tests := []struct {
		name             string
		sliceSubnet      string
		subnetSize       int
		allocatedSubnets []string
		expectError      bool
		expectedSubnet   string
	}{
		{
			name:             "no-allocations",
			sliceSubnet:      "192.168.1.0/24",
			subnetSize:       26,
			allocatedSubnets: []string{},
			expectError:      false,
			expectedSubnet:   "192.168.1.0/26",
		},
		{
			name:             "first-allocated",
			sliceSubnet:      "192.168.1.0/24",
			subnetSize:       26,
			allocatedSubnets: []string{"192.168.1.0/26"},
			expectError:      false,
			expectedSubnet:   "192.168.1.64/26",
		},
		{
			name:             "multiple-allocations",
			sliceSubnet:      "192.168.1.0/24",
			subnetSize:       26,
			allocatedSubnets: []string{"192.168.1.0/26", "192.168.1.64/26"},
			expectError:      false,
			expectedSubnet:   "192.168.1.128/26",
		},
		{
			name:             "all-allocated",
			sliceSubnet:      "192.168.1.0/24",
			subnetSize:       26,
			allocatedSubnets: []string{"192.168.1.0/26", "192.168.1.64/26", "192.168.1.128/26", "192.168.1.192/26"},
			expectError:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			subnet, err := allocator.FindNextAvailableSubnet(tt.sliceSubnet, tt.subnetSize, tt.allocatedSubnets)
			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error but got subnet: %s", subnet)
				}
			} else {
				if err != nil {
					t.Errorf("Expected no error but got: %v", err)
				} else if subnet != tt.expectedSubnet {
					t.Errorf("Expected subnet %s, got %s", tt.expectedSubnet, subnet)
				}
			}
		})
	}
}

func TestFindOptimalSubnet(t *testing.T) {
	allocator := NewIpamAllocator()

	// Test basic functionality
	subnet, err := allocator.FindOptimalSubnet("192.168.1.0/24", 26, []string{}, "cluster-1")
	if err != nil {
		t.Errorf("Expected no error but got: %v", err)
	}
	if subnet == "" {
		t.Error("Expected a subnet but got empty string")
	}

	// Test with allocations
	allocatedSubnets := []string{"192.168.1.0/26"}
	subnet, err = allocator.FindOptimalSubnet("192.168.1.0/24", 26, allocatedSubnets, "cluster-2")
	if err != nil {
		t.Errorf("Expected no error but got: %v", err)
	}
	if subnet == "192.168.1.0/26" {
		t.Error("Should not allocate an already allocated subnet")
	}

	// Test without cluster hint
	subnet, err = allocator.FindOptimalSubnet("192.168.1.0/24", 26, []string{}, "")
	if err != nil {
		t.Errorf("Expected no error but got: %v", err)
	}
	if subnet == "" {
		t.Error("Expected a subnet but got empty string")
	}
}

func TestIsSubnetOverlapping(t *testing.T) {
	allocator := NewIpamAllocator()

	tests := []struct {
		name        string
		subnet1     string
		subnet2     string
		expectError bool
		overlapping bool
	}{
		{
			name:        "identical-subnets",
			subnet1:     "192.168.1.0/24",
			subnet2:     "192.168.1.0/24",
			expectError: false,
			overlapping: true,
		},
		{
			name:        "subnet1-contains-subnet2",
			subnet1:     "192.168.1.0/24",
			subnet2:     "192.168.1.0/26",
			expectError: false,
			overlapping: true,
		},
		{
			name:        "subnet2-contains-subnet1",
			subnet1:     "192.168.1.0/26",
			subnet2:     "192.168.1.0/24",
			expectError: false,
			overlapping: true,
		},
		{
			name:        "non-overlapping-subnets",
			subnet1:     "192.168.1.0/26",
			subnet2:     "192.168.1.64/26",
			expectError: false,
			overlapping: false,
		},
		{
			name:        "different-networks",
			subnet1:     "192.168.1.0/24",
			subnet2:     "192.168.2.0/24",
			expectError: false,
			overlapping: false,
		},
		{
			name:        "invalid-subnet1",
			subnet1:     "invalid",
			subnet2:     "192.168.1.0/24",
			expectError: true,
		},
		{
			name:        "invalid-subnet2",
			subnet1:     "192.168.1.0/24",
			subnet2:     "invalid",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			overlapping, err := allocator.IsSubnetOverlapping(tt.subnet1, tt.subnet2)
			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error for subnets %s and %s, but got none", tt.subnet1, tt.subnet2)
				}
			} else {
				if err != nil {
					t.Errorf("Expected no error for subnets %s and %s, but got: %v", tt.subnet1, tt.subnet2, err)
				} else if overlapping != tt.overlapping {
					t.Errorf("Expected overlapping=%t for subnets %s and %s, got %t", tt.overlapping, tt.subnet1, tt.subnet2, overlapping)
				}
			}
		})
	}
}

func TestGetSubnetUtilization(t *testing.T) {
	allocator := NewIpamAllocator()

	tests := []struct {
		name           string
		sliceSubnet    string
		subnetSize     int
		allocatedCount int
		expectError    bool
		expectedUtil   float64
	}{
		{
			name:           "no-allocations",
			sliceSubnet:    "192.168.1.0/24",
			subnetSize:     26,
			allocatedCount: 0,
			expectError:    false,
			expectedUtil:   0.0,
		},
		{
			name:           "half-allocated",
			sliceSubnet:    "192.168.1.0/24",
			subnetSize:     26,
			allocatedCount: 2,
			expectError:    false,
			expectedUtil:   50.0,
		},
		{
			name:           "fully-allocated",
			sliceSubnet:    "192.168.1.0/24",
			subnetSize:     26,
			allocatedCount: 4,
			expectError:    false,
			expectedUtil:   100.0,
		},
		{
			name:           "over-allocated",
			sliceSubnet:    "192.168.1.0/24",
			subnetSize:     26,
			allocatedCount: 8,
			expectError:    false,
			expectedUtil:   200.0,
		},
		{
			name:           "invalid-slice-subnet",
			sliceSubnet:    "invalid",
			subnetSize:     26,
			allocatedCount: 1,
			expectError:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			util, err := allocator.GetSubnetUtilization(tt.sliceSubnet, tt.subnetSize, tt.allocatedCount)
			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error for slice %s with subnet size %d and allocated count %d, but got none", tt.sliceSubnet, tt.subnetSize, tt.allocatedCount)
				}
			} else {
				if err != nil {
					t.Errorf("Expected no error for slice %s with subnet size %d and allocated count %d, but got: %v", tt.sliceSubnet, tt.subnetSize, tt.allocatedCount, err)
				} else if util != tt.expectedUtil {
					t.Errorf("Expected utilization %.2f%%, got %.2f%%", tt.expectedUtil, util)
				}
			}
		})
	}
}

func TestCompactAllocations(t *testing.T) {
	allocator := NewIpamAllocator()

	tests := []struct {
		name          string
		sliceSubnet   string
		subnetSize    int
		activeSubnets []string
		expectError   bool
		expectedCount int
	}{
		{
			name:          "empty-active-subnets",
			sliceSubnet:   "192.168.1.0/24",
			subnetSize:    26,
			activeSubnets: []string{},
			expectError:   false,
			expectedCount: 0,
		},
		{
			name:          "single-active-subnet",
			sliceSubnet:   "192.168.1.0/24",
			subnetSize:    26,
			activeSubnets: []string{"192.168.1.0/26"},
			expectError:   false,
			expectedCount: 1,
		},
		{
			name:          "multiple-active-subnets",
			sliceSubnet:   "192.168.1.0/24",
			subnetSize:    26,
			activeSubnets: []string{"192.168.1.64/26", "192.168.1.0/26", "192.168.1.128/26"},
			expectError:   false,
			expectedCount: 3,
		},
		{
			name:          "invalid-slice-subnet",
			sliceSubnet:   "invalid",
			subnetSize:    26,
			activeSubnets: []string{"192.168.1.0/26"},
			expectError:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			compacted, err := allocator.CompactAllocations(tt.sliceSubnet, tt.subnetSize, tt.activeSubnets)
			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error for slice %s with subnet size %d, but got none", tt.sliceSubnet, tt.subnetSize)
				}
			} else {
				if err != nil {
					t.Errorf("Expected no error for slice %s with subnet size %d, but got: %v", tt.sliceSubnet, tt.subnetSize, err)
				} else if len(compacted) != tt.expectedCount {
					t.Errorf("Expected %d compacted subnets, got %d", tt.expectedCount, len(compacted))
				}
			}
		})
	}
}

func TestPredictAllocationNeeds(t *testing.T) {
	allocator := NewIpamAllocator()

	tests := []struct {
		name               string
		sliceSubnet        string
		subnetSize         int
		currentAllocations int
		growthRate         float64
		expectError        bool
		expectedPrediction int
	}{
		{
			name:               "no-growth",
			sliceSubnet:        "192.168.1.0/24",
			subnetSize:         26,
			currentAllocations: 2,
			growthRate:         0.0,
			expectError:        false,
			expectedPrediction: 2,
		},
		{
			name:               "50-percent-growth",
			sliceSubnet:        "192.168.1.0/24",
			subnetSize:         26,
			currentAllocations: 2,
			growthRate:         0.5,
			expectError:        false,
			expectedPrediction: 3,
		},
		{
			name:               "100-percent-growth",
			sliceSubnet:        "192.168.1.0/24",
			subnetSize:         26,
			currentAllocations: 2,
			growthRate:         1.0,
			expectError:        false,
			expectedPrediction: 4,
		},
		{
			name:               "growth-exceeds-capacity",
			sliceSubnet:        "192.168.1.0/24",
			subnetSize:         26,
			currentAllocations: 3,
			growthRate:         2.0,
			expectError:        false,
			expectedPrediction: 4, // Capped at max capacity
		},
		{
			name:               "negative-growth-rate",
			sliceSubnet:        "192.168.1.0/24",
			subnetSize:         26,
			currentAllocations: 2,
			growthRate:         -0.5,
			expectError:        true,
		},
		{
			name:               "invalid-slice-subnet",
			sliceSubnet:        "invalid",
			subnetSize:         26,
			currentAllocations: 2,
			growthRate:         0.5,
			expectError:        true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			prediction, err := allocator.PredictAllocationNeeds(tt.sliceSubnet, tt.subnetSize, tt.currentAllocations, tt.growthRate)
			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error for slice %s with subnet size %d, current allocations %d, and growth rate %.2f, but got none", tt.sliceSubnet, tt.subnetSize, tt.currentAllocations, tt.growthRate)
				}
			} else {
				if err != nil {
					t.Errorf("Expected no error for slice %s with subnet size %d, current allocations %d, and growth rate %.2f, but got: %v", tt.sliceSubnet, tt.subnetSize, tt.currentAllocations, tt.growthRate, err)
				} else if prediction != tt.expectedPrediction {
					t.Errorf("Expected prediction %d, got %d", tt.expectedPrediction, prediction)
				}
			}
		})
	}
}

func TestValidateAllocationConsistency(t *testing.T) {
	allocator := NewIpamAllocator()

	tests := []struct {
		name        string
		allocations []ClusterSubnetAllocation
		expectError bool
		errorMsg    string
	}{
		{
			name:        "empty-allocations",
			allocations: []ClusterSubnetAllocation{},
			expectError: false,
		},
		{
			name: "single-allocation",
			allocations: []ClusterSubnetAllocation{
				{ClusterName: "cluster-1", Subnet: "192.168.1.0/26", AllocatedAt: time.Now(), Status: "Allocated"},
			},
			expectError: false,
		},
		{
			name: "valid-multiple-allocations",
			allocations: []ClusterSubnetAllocation{
				{ClusterName: "cluster-1", Subnet: "192.168.1.0/26", AllocatedAt: time.Now(), Status: "Allocated"},
				{ClusterName: "cluster-2", Subnet: "192.168.1.64/26", AllocatedAt: time.Now(), Status: "Allocated"},
			},
			expectError: false,
		},
		{
			name: "duplicate-cluster-names",
			allocations: []ClusterSubnetAllocation{
				{ClusterName: "cluster-1", Subnet: "192.168.1.0/26", AllocatedAt: time.Now(), Status: "Allocated"},
				{ClusterName: "cluster-1", Subnet: "192.168.1.64/26", AllocatedAt: time.Now(), Status: "Allocated"},
			},
			expectError: true,
			errorMsg:    "duplicate cluster name",
		},
		{
			name: "empty-cluster-name",
			allocations: []ClusterSubnetAllocation{
				{ClusterName: "", Subnet: "192.168.1.0/26", AllocatedAt: time.Now(), Status: "Allocated"},
			},
			expectError: true,
			errorMsg:    "cluster name cannot be empty",
		},
		{
			name: "empty-subnet",
			allocations: []ClusterSubnetAllocation{
				{ClusterName: "cluster-1", Subnet: "", AllocatedAt: time.Now(), Status: "Allocated"},
			},
			expectError: true,
			errorMsg:    "subnet cannot be empty",
		},
		{
			name: "invalid-subnet-format",
			allocations: []ClusterSubnetAllocation{
				{ClusterName: "cluster-1", Subnet: "invalid", AllocatedAt: time.Now(), Status: "Allocated"},
			},
			expectError: true,
			errorMsg:    "invalid subnet",
		},
		{
			name: "overlapping-subnets",
			allocations: []ClusterSubnetAllocation{
				{ClusterName: "cluster-1", Subnet: "192.168.1.0/24", AllocatedAt: time.Now(), Status: "Allocated"},
				{ClusterName: "cluster-2", Subnet: "192.168.1.0/26", AllocatedAt: time.Now(), Status: "Allocated"},
			},
			expectError: true,
			errorMsg:    "overlapping subnets detected",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := allocator.ValidateAllocationConsistency(tt.allocations)
			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error for allocations, but got none")
				} else if tt.errorMsg != "" && !containsString(err.Error(), tt.errorMsg) {
					t.Errorf("Expected error message to contain '%s', got '%s'", tt.errorMsg, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("Expected no error for allocations, but got: %v", err)
				}
			}
		})
	}
}

func TestCacheOperations(t *testing.T) {
	allocator := NewIpamAllocator()

	// Test empty cache
	cache, exists := allocator.GetCachedAllocation("test-slice")
	if exists {
		t.Error("Expected no cache entry for non-existent slice")
	}
	if cache != nil {
		t.Error("Expected nil cache for non-existent slice")
	}

	// Test updating cache
	testCache := &AllocationCache{
		SliceSubnet:      "192.168.1.0/24",
		SubnetSize:       26,
		AllocatedSubnets: map[string]bool{"192.168.1.0/26": true},
		TTL:              DefaultCacheTTL,
	}

	allocator.UpdateCache("test-slice", testCache)

	// Test retrieving cache
	cache, exists = allocator.GetCachedAllocation("test-slice")
	if !exists {
		t.Error("Expected cache entry for test-slice")
	}
	if cache == nil {
		t.Fatal("Expected non-nil cache")
	}
	if cache.SliceSubnet != testCache.SliceSubnet {
		t.Errorf("Expected slice subnet %s, got %s", testCache.SliceSubnet, cache.SliceSubnet)
	}

	// Test cache expiration - manually set an expired cache
	expiredCache := &AllocationCache{
		SliceSubnet:      "192.168.2.0/24",
		SubnetSize:       26,
		AllocatedSubnets: map[string]bool{},
		LastUpdated:      time.Now().Add(-10 * time.Minute), // Expired
		TTL:              5 * time.Minute,
	}

	// Manually set the expired cache to avoid LastUpdated being overwritten
	allocator.mutex.Lock()
	allocator.cache["expired-slice"] = expiredCache
	allocator.mutex.Unlock()

	_, exists = allocator.GetCachedAllocation("expired-slice")
	if exists {
		t.Error("Expected no cache entry for expired slice")
	}

	// Test invalidating cache
	allocator.InvalidateCache("test-slice")
	_, exists = allocator.GetCachedAllocation("test-slice")
	if exists {
		t.Error("Expected no cache entry after invalidation")
	}

	// Test updating with nil cache (deletion)
	allocator.UpdateCache("test-slice", testCache)
	allocator.UpdateCache("test-slice", nil)
	_, exists = allocator.GetCachedAllocation("test-slice")
	if exists {
		t.Error("Expected no cache entry after nil update")
	}
}

func TestCleanupExpiredCache(t *testing.T) {
	allocator := NewIpamAllocator()

	// Add valid cache entry
	validCache := &AllocationCache{
		SliceSubnet:      "192.168.1.0/24",
		SubnetSize:       26,
		AllocatedSubnets: map[string]bool{},
		TTL:              DefaultCacheTTL,
	}
	allocator.UpdateCache("valid-slice", validCache)

	// Add expired cache entry manually to avoid LastUpdated being overwritten
	expiredCache := &AllocationCache{
		SliceSubnet:      "192.168.2.0/24",
		SubnetSize:       26,
		AllocatedSubnets: map[string]bool{},
		LastUpdated:      time.Now().Add(-10 * time.Minute),
		TTL:              5 * time.Minute,
	}

	// Manually set the expired cache
	allocator.mutex.Lock()
	allocator.cache["expired-slice"] = expiredCache
	allocator.mutex.Unlock()

	// Cleanup expired cache
	allocator.CleanupExpiredCache()

	// Check that valid cache still exists
	_, exists := allocator.GetCachedAllocation("valid-slice")
	if !exists {
		t.Error("Expected valid cache to still exist after cleanup")
	}

	// Check that expired cache is removed
	allocator.mutex.RLock()
	_, exists = allocator.cache["expired-slice"]
	allocator.mutex.RUnlock()
	if exists {
		t.Error("Expected expired cache to be removed after cleanup")
	}
}

func TestGetCacheStats(t *testing.T) {
	allocator := NewIpamAllocator()

	// Test empty cache stats
	stats := allocator.GetCacheStats()
	if stats["total_entries"] != 0 {
		t.Errorf("Expected 0 total entries, got %v", stats["total_entries"])
	}

	// Add cache entries
	cache1 := &AllocationCache{
		SliceSubnet:      "192.168.1.0/24",
		SubnetSize:       26,
		AllocatedSubnets: map[string]bool{"192.168.1.0/26": true},
		TTL:              DefaultCacheTTL,
	}
	allocator.UpdateCache("slice-1", cache1)

	cache2 := &AllocationCache{
		SliceSubnet:      "192.168.2.0/24",
		SubnetSize:       24,
		AllocatedSubnets: map[string]bool{},
		TTL:              DefaultCacheTTL,
	}
	allocator.UpdateCache("slice-2", cache2)

	// Test populated cache stats
	stats = allocator.GetCacheStats()
	if stats["total_entries"] != 2 {
		t.Errorf("Expected 2 total entries, got %v", stats["total_entries"])
	}

	entries, ok := stats["entries"].(map[string]interface{})
	if !ok {
		t.Fatal("Expected entries to be a map")
	}

	if len(entries) != 2 {
		t.Errorf("Expected 2 entry details, got %d", len(entries))
	}

	// Check slice-1 details
	slice1Details, ok := entries["slice-1"].(map[string]interface{})
	if !ok {
		t.Fatal("Expected slice-1 details to be a map")
	}
	if slice1Details["slice_subnet"] != "192.168.1.0/24" {
		t.Errorf("Expected slice subnet 192.168.1.0/24, got %v", slice1Details["slice_subnet"])
	}
	if slice1Details["subnet_size"] != 26 {
		t.Errorf("Expected subnet size 26, got %v", slice1Details["subnet_size"])
	}
	if slice1Details["allocated_count"] != 1 {
		t.Errorf("Expected allocated count 1, got %v", slice1Details["allocated_count"])
	}
}

// Performance tests
func BenchmarkSubnetAllocation(b *testing.B) {
	allocator := NewIpamAllocator()
	sliceSubnet := "10.0.0.0/16"
	subnetSize := 24

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := allocator.FindNextAvailableSubnet(sliceSubnet, subnetSize, []string{})
		if err != nil {
			b.Fatalf("Allocation failed: %v", err)
		}
	}
}

func BenchmarkSubnetGeneration(b *testing.B) {
	allocator := NewIpamAllocator()
	sliceSubnet := "10.0.0.0/16"
	subnetSize := 24

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := allocator.GenerateSubnetList(sliceSubnet, subnetSize)
		if err != nil {
			b.Fatalf("Subnet generation failed: %v", err)
		}
	}
}

func BenchmarkCacheOperations(b *testing.B) {
	allocator := NewIpamAllocator()
	cache := &AllocationCache{
		SliceSubnet:      "192.168.1.0/24",
		SubnetSize:       26,
		AllocatedSubnets: map[string]bool{},
		TTL:              DefaultCacheTTL,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sliceName := fmt.Sprintf("slice-%d", i%100)
		allocator.UpdateCache(sliceName, cache)
		allocator.GetCachedAllocation(sliceName)
	}
}

// Concurrent access tests
func TestConcurrentCacheAccess(t *testing.T) {
	allocator := NewIpamAllocator()
	cache := &AllocationCache{
		SliceSubnet:      "192.168.1.0/24",
		SubnetSize:       26,
		AllocatedSubnets: map[string]bool{},
		TTL:              DefaultCacheTTL,
	}

	// Test concurrent updates
	var wg sync.WaitGroup
	numGoroutines := 10
	numOperations := 100

	wg.Add(numGoroutines)
	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < numOperations; j++ {
				sliceName := fmt.Sprintf("slice-%d-%d", id, j)
				allocator.UpdateCache(sliceName, cache)
				allocator.GetCachedAllocation(sliceName)
				allocator.InvalidateCache(sliceName)
			}
		}(i)
	}

	wg.Wait()

	// Verify no race conditions occurred (test will fail if there are any race conditions)
	stats := allocator.GetCacheStats()
	if stats["total_entries"].(int) < 0 {
		t.Error("Unexpected negative total entries after concurrent access")
	}
}

// Property-based tests
func TestSubnetAllocationProperties(t *testing.T) {
	allocator := NewIpamAllocator()

	// Test that generated subnets don't overlap
	testCases := []struct {
		sliceSubnet string
		subnetSize  int
	}{
		{"192.168.1.0/24", 26},
		{"10.1.0.0/20", 24},
		{"172.16.0.0/16", 20},
	}

	for _, tc := range testCases {
		t.Run(fmt.Sprintf("no-overlap-%s-%d", tc.sliceSubnet, tc.subnetSize), func(t *testing.T) {
			subnets, err := allocator.GenerateSubnetList(tc.sliceSubnet, tc.subnetSize)
			if err != nil {
				t.Fatalf("Failed to generate subnet list: %v", err)
			}

			// Check no overlaps
			for i := 0; i < len(subnets); i++ {
				for j := i + 1; j < len(subnets); j++ {
					overlapping, err := allocator.IsSubnetOverlapping(subnets[i], subnets[j])
					if err != nil {
						t.Fatalf("Failed to check overlap: %v", err)
					}
					if overlapping {
						t.Errorf("Found overlapping subnets: %s and %s", subnets[i], subnets[j])
					}
				}
			}
		})
	}

	// Test allocation invariants
	t.Run("allocation-invariants", func(t *testing.T) {
		sliceSubnet := "192.168.1.0/24"
		subnetSize := 26

		maxClusters, err := allocator.CalculateMaxClusters(sliceSubnet, subnetSize)
		if err != nil {
			t.Fatalf("Failed to calculate max clusters: %v", err)
		}

		subnets, err := allocator.GenerateSubnetList(sliceSubnet, subnetSize)
		if err != nil {
			t.Fatalf("Failed to generate subnet list: %v", err)
		}

		// Verify the number of generated subnets matches the calculated max
		if len(subnets) != maxClusters {
			t.Errorf("Generated subnet count (%d) doesn't match calculated max (%d)", len(subnets), maxClusters)
		}

		// Test sequential allocation
		allocated := []string{}
		for i := 0; i < maxClusters; i++ {
			subnet, err := allocator.FindNextAvailableSubnet(sliceSubnet, subnetSize, allocated)
			if err != nil {
				t.Fatalf("Failed to allocate subnet %d: %v", i, err)
			}
			allocated = append(allocated, subnet)
		}

		// Verify no more subnets can be allocated
		_, err = allocator.FindNextAvailableSubnet(sliceSubnet, subnetSize, allocated)
		if err == nil {
			t.Error("Expected error when trying to allocate beyond capacity")
		}
	})
}

// Helper function to check if a string contains a substring
func containsString(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 || (len(s) > len(substr) && (s[:len(substr)] == substr || s[len(s)-len(substr):] == substr || containsSubstring(s, substr))))
}

func containsSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
