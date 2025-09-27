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
	"context"
	"fmt"
	"time"

	"github.com/kubeslice/kubeslice-controller/apis/controller/v1alpha1"
	"github.com/kubeslice/kubeslice-controller/metrics"
	"github.com/kubeslice/kubeslice-controller/util"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

type ISliceIpamService interface {
	ReconcileSliceIpam(ctx context.Context, req ctrl.Request) (ctrl.Result, error)
	AllocateSubnetForCluster(ctx context.Context, sliceName, clusterName, namespace string) (string, error)
	ReleaseSubnetForCluster(ctx context.Context, sliceName, clusterName, namespace string) error
	GetClusterSubnet(ctx context.Context, sliceName, clusterName, namespace string) (string, error)
	CreateSliceIpam(ctx context.Context, sliceConfig *v1alpha1.SliceConfig) error
	DeleteSliceIpam(ctx context.Context, sliceName, namespace string) error
	CleanupExpiredReleasedSubnets(ctx context.Context, sliceName, namespace string, expirationDuration time.Duration) error
}

// SliceIpamService follows existing service struct pattern
type SliceIpamService struct {
	mf        metrics.IMetricRecorder
	allocator *util.IpamAllocator
}

// NewSliceIpamService creates a new SliceIpamService instance
func NewSliceIpamService(mf metrics.IMetricRecorder) *SliceIpamService {
	return &SliceIpamService{
		mf:        mf,
		allocator: util.NewIpamAllocator(),
	}
}

// ReconcileSliceIpam reconciles a SliceIpam resource
func (s *SliceIpamService) ReconcileSliceIpam(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := util.CtxLogger(ctx)
	logger.Infof("Starting reconciliation for SliceIpam %s", req.NamespacedName)

	// Load metrics with project name and namespace
	s.mf.WithProject(util.GetProjectName(req.Namespace)).
		WithNamespace(req.Namespace)

	// Get SliceIpam resource
	sliceIpam := &v1alpha1.SliceIpam{}
	found, err := util.GetResourceIfExist(ctx, req.NamespacedName, sliceIpam)
	if err != nil {
		logger.Errorf("Error getting SliceIpam resource: %v", err)
		return ctrl.Result{}, err
	}
	if !found {
		logger.Infof("SliceIpam %s not found, may have been deleted", req.NamespacedName)
		return ctrl.Result{}, nil
	}

	// Handle deletion
	if sliceIpam.DeletionTimestamp != nil {
		logger.Infof("Handling deletion for SliceIpam %s", req.NamespacedName)
		return s.handleSliceIpamDeletion(ctx, sliceIpam)
	}

	// Add finalizer if not present
	if !controllerutil.ContainsFinalizer(sliceIpam, SliceIpamFinalizer) {
		logger.Debugf("Adding finalizer to SliceIpam %s", req.NamespacedName)
		controllerutil.AddFinalizer(sliceIpam, SliceIpamFinalizer)
		err = util.UpdateResource(ctx, sliceIpam)
		if shouldReturn, result, reconErr := util.IsReconciled(ctrl.Result{}, err); shouldReturn {
			return result, reconErr
		}
	}

	// Reconcile SliceIpam state
	return s.reconcileSliceIpamState(ctx, sliceIpam)
}

// handleSliceIpamDeletion handles the deletion of SliceIpam resource
func (s *SliceIpamService) handleSliceIpamDeletion(ctx context.Context, sliceIpam *v1alpha1.SliceIpam) (ctrl.Result, error) {
	logger := util.CtxLogger(ctx)
	logger.Infof("Processing deletion for SliceIpam %s", sliceIpam.Name)

	// Check if there are any active allocations
	activeAllocations := 0
	for _, allocation := range sliceIpam.Status.AllocatedSubnets {
		if allocation.Status == v1alpha1.SubnetStatusAllocated || allocation.Status == v1alpha1.SubnetStatusInUse {
			activeAllocations++
		}
	}

	if activeAllocations > 0 {
		logger.Warnf("SliceIpam %s has %d active allocations, cannot delete", sliceIpam.Name, activeAllocations)
		return ctrl.Result{RequeueAfter: RequeueTime}, fmt.Errorf("SliceIpam has %d active subnet allocations", activeAllocations)
	}

	// Remove finalizer
	if controllerutil.ContainsFinalizer(sliceIpam, SliceIpamFinalizer) {
		logger.Debugf("Removing finalizer from SliceIpam %s", sliceIpam.Name)
		controllerutil.RemoveFinalizer(sliceIpam, SliceIpamFinalizer)
		updateErr := util.UpdateResource(ctx, sliceIpam)
		if shouldReturn, result, reconErr := util.IsReconciled(ctrl.Result{}, updateErr); shouldReturn {
			return result, reconErr
		}
	}

	logger.Infof("Successfully deleted SliceIpam %s", sliceIpam.Name)
	return ctrl.Result{}, nil
}

// reconcileSliceIpamState reconciles the state of SliceIpam resource
func (s *SliceIpamService) reconcileSliceIpamState(ctx context.Context, sliceIpam *v1alpha1.SliceIpam) (ctrl.Result, error) {
	logger := util.CtxLogger(ctx)
	logger.Debugf("Reconciling state for SliceIpam %s", sliceIpam.Name)

	// Calculate available subnets using CalculateMaxClusters method
	totalSubnets, err := s.allocator.CalculateMaxClusters(sliceIpam.Spec.SliceSubnet, sliceIpam.Spec.SubnetSize)
	if err != nil {
		logger.Errorf("Error calculating total subnets: %v", err)
		return ctrl.Result{}, err
	}

	allocatedCount := len(sliceIpam.Status.AllocatedSubnets)
	availableCount := totalSubnets - allocatedCount

	// Perform periodic cleanup of expired released subnets (24 hours)
	cleanupErr := s.CleanupExpiredReleasedSubnets(ctx, sliceIpam.Name, sliceIpam.Namespace, 24*time.Hour)
	if cleanupErr != nil {
		logger.Warnf("Failed to cleanup expired released subnets: %v", cleanupErr)
		// Don't fail reconciliation for cleanup errors, just log them
	}

	// Recalculate available subnets after potential cleanup
	allocatedCount = len(sliceIpam.Status.AllocatedSubnets)
	availableCount = totalSubnets - allocatedCount

	// Update status
	sliceIpam.Status.TotalSubnets = totalSubnets
	sliceIpam.Status.AvailableSubnets = availableCount
	sliceIpam.Status.LastUpdated = metav1.Now()

	// Update resource
	updateErr := util.UpdateResource(ctx, sliceIpam)
	if shouldReturn, result, reconErr := util.IsReconciled(ctrl.Result{}, updateErr); shouldReturn {
		return result, reconErr
	}

	logger.Debugf("Successfully reconciled SliceIpam %s - Total: %d, Available: %d",
		sliceIpam.Name, totalSubnets, availableCount)

	// Requeue after 30 minutes for periodic cleanup and monitoring
	return ctrl.Result{RequeueAfter: 30 * time.Minute}, nil
}

// AllocateSubnetForCluster allocates a subnet for a specific cluster
func (s *SliceIpamService) AllocateSubnetForCluster(ctx context.Context, sliceName, clusterName, namespace string) (string, error) {
	logger := util.CtxLogger(ctx)
	logger.Infof("Allocating subnet for cluster %s in slice %s", clusterName, sliceName)

	// Get SliceIpam resource
	sliceIpam := &v1alpha1.SliceIpam{}
	key := types.NamespacedName{Name: sliceName, Namespace: namespace}
	found, err := util.GetResourceIfExist(ctx, key, sliceIpam)
	if err != nil {
		logger.Errorf("Error getting SliceIpam resource: %v", err)
		return "", err
	}
	if !found {
		logger.Errorf("SliceIpam %s not found in namespace %s", sliceName, namespace)
		return "", fmt.Errorf("SliceIpam %s not found", sliceName)
	}

	// Check if cluster already has allocation
	for _, allocation := range sliceIpam.Status.AllocatedSubnets {
		if allocation.ClusterName == clusterName {
			if allocation.Status == v1alpha1.SubnetStatusAllocated || allocation.Status == v1alpha1.SubnetStatusInUse {
				logger.Infof("Cluster %s already has subnet %s allocated", clusterName, allocation.Subnet)
				return allocation.Subnet, nil
			}
		}
	}

	// Convert API allocations to util types for IPAM processing
	utilAllocations := s.convertAllocationsToUtil(sliceIpam.Status.AllocatedSubnets)

	// Try to find available subnet with reclamation support (5 minute wait period)
	subnet, isReclaimed, err := s.allocator.FindNextAvailableSubnetWithReclamation(
		sliceIpam.Spec.SliceSubnet,
		sliceIpam.Spec.SubnetSize,
		utilAllocations,
		5*time.Minute, // Wait 5 minutes before reclaiming released subnets
	)
	if err != nil {
		logger.Errorf("Failed to find available subnet: %v", err)
		return "", fmt.Errorf("failed to find available subnet: %v", err)
	}

	// Handle reclaimed vs new subnet allocation
	if isReclaimed {
		// Remove the old released allocation and add new one
		s.removeReleasedAllocation(sliceIpam, subnet)
		logger.Infof("Reclaimed previously released subnet %s for cluster %s", subnet, clusterName)
	}

	// Add allocation to status
	allocation := v1alpha1.ClusterSubnetAllocation{
		ClusterName: clusterName,
		Subnet:      subnet,
		AllocatedAt: metav1.Now(),
		Status:      v1alpha1.SubnetStatusAllocated,
	}
	sliceIpam.Status.AllocatedSubnets = append(sliceIpam.Status.AllocatedSubnets, allocation)
	sliceIpam.Status.AvailableSubnets--
	sliceIpam.Status.LastUpdated = metav1.Now()

	// Update resource
	err = util.UpdateResource(ctx, sliceIpam)
	if err != nil {
		logger.Errorf("Failed to update SliceIpam: %v", err)
		return "", fmt.Errorf("failed to update SliceIpam: %v", err)
	}

	logger.Infof("Successfully allocated subnet %s to cluster %s", subnet, clusterName)
	return subnet, nil
}

// ReleaseSubnetForCluster releases the subnet allocated to a specific cluster
func (s *SliceIpamService) ReleaseSubnetForCluster(ctx context.Context, sliceName, clusterName, namespace string) error {
	logger := util.CtxLogger(ctx)
	logger.Infof("Releasing subnet for cluster %s in slice %s", clusterName, sliceName)

	// Get SliceIpam resource
	sliceIpam := &v1alpha1.SliceIpam{}
	key := types.NamespacedName{Name: sliceName, Namespace: namespace}
	found, err := util.GetResourceIfExist(ctx, key, sliceIpam)
	if err != nil {
		logger.Errorf("Error getting SliceIpam resource: %v", err)
		return err
	}
	if !found {
		logger.Errorf("SliceIpam %s not found in namespace %s", sliceName, namespace)
		return fmt.Errorf("SliceIpam %s not found", sliceName)
	}

	// Find and update allocation
	found = false
	for i, allocation := range sliceIpam.Status.AllocatedSubnets {
		if allocation.ClusterName == clusterName {
			if allocation.Status == v1alpha1.SubnetStatusAllocated || allocation.Status == v1alpha1.SubnetStatusInUse {
				// Mark as released
				sliceIpam.Status.AllocatedSubnets[i].Status = v1alpha1.SubnetStatusReleased

				// Set ReleasedAt timestamp for subnet reclamation tracking
				now := metav1.Now()
				sliceIpam.Status.AllocatedSubnets[i].ReleasedAt = &now

				sliceIpam.Status.AvailableSubnets++
				sliceIpam.Status.LastUpdated = metav1.Now()
				found = true
				logger.Infof("Marked subnet %s as released for cluster %s at %v", allocation.Subnet, clusterName, now)
				break
			}
		}
	}

	if !found {
		logger.Warnf("No active subnet allocation found for cluster %s", clusterName)
		return fmt.Errorf("no active subnet allocation found for cluster %s", clusterName)
	}

	// Update resource
	err = util.UpdateResource(ctx, sliceIpam)
	if err != nil {
		logger.Errorf("Failed to update SliceIpam: %v", err)
		return fmt.Errorf("failed to update SliceIpam: %v", err)
	}

	logger.Infof("Successfully released subnet for cluster %s", clusterName)
	return nil
}

// GetClusterSubnet returns the subnet allocated to a specific cluster
func (s *SliceIpamService) GetClusterSubnet(ctx context.Context, sliceName, clusterName, namespace string) (string, error) {
	logger := util.CtxLogger(ctx)
	logger.Debugf("Getting subnet for cluster %s in slice %s", clusterName, sliceName)

	// Get SliceIpam resource
	sliceIpam := &v1alpha1.SliceIpam{}
	key := types.NamespacedName{Name: sliceName, Namespace: namespace}
	found, err := util.GetResourceIfExist(ctx, key, sliceIpam)
	if err != nil {
		logger.Errorf("Error getting SliceIpam resource: %v", err)
		return "", err
	}
	if !found {
		logger.Errorf("SliceIpam %s not found in namespace %s", sliceName, namespace)
		return "", fmt.Errorf("SliceIpam %s not found", sliceName)
	}

	// Find allocation for cluster
	for _, allocation := range sliceIpam.Status.AllocatedSubnets {
		if allocation.ClusterName == clusterName {
			if allocation.Status == v1alpha1.SubnetStatusAllocated || allocation.Status == v1alpha1.SubnetStatusInUse {
				logger.Debugf("Found subnet %s for cluster %s", allocation.Subnet, clusterName)
				return allocation.Subnet, nil
			}
		}
	}

	logger.Debugf("No active subnet allocation found for cluster %s", clusterName)
	return "", fmt.Errorf("no active subnet allocation found for cluster %s", clusterName)
}

// CreateSliceIpam creates a new SliceIpam resource for a SliceConfig
func (s *SliceIpamService) CreateSliceIpam(ctx context.Context, sliceConfig *v1alpha1.SliceConfig) error {
	logger := util.CtxLogger(ctx)
	logger.Infof("Creating SliceIpam for slice %s", sliceConfig.Name)

	// Check if SliceIpam already exists
	sliceIpam := &v1alpha1.SliceIpam{}
	key := types.NamespacedName{Name: sliceConfig.Name, Namespace: sliceConfig.Namespace}
	found, err := util.GetResourceIfExist(ctx, key, sliceIpam)
	if err != nil {
		logger.Errorf("Error checking if SliceIpam exists: %v", err)
		return err
	}
	if found {
		logger.Infof("SliceIpam %s already exists", sliceConfig.Name)
		return nil
	}

	// Calculate total subnets using CalculateMaxClusters method
	totalSubnets, err := s.allocator.CalculateMaxClusters(sliceConfig.Spec.SliceSubnet, 24) // Default subnet size
	if err != nil {
		logger.Errorf("Error calculating total subnets: %v", err)
		return err
	}

	// Create SliceIpam resource
	sliceIpam = &v1alpha1.SliceIpam{
		ObjectMeta: metav1.ObjectMeta{
			Name:      sliceConfig.Name,
			Namespace: sliceConfig.Namespace,
		},
		Spec: v1alpha1.SliceIpamSpec{
			SliceName:   sliceConfig.Name,
			SliceSubnet: sliceConfig.Spec.SliceSubnet,
			SubnetSize:  24, // Default subnet size
		},
		Status: v1alpha1.SliceIpamStatus{
			TotalSubnets:     totalSubnets,
			AvailableSubnets: totalSubnets,
			AllocatedSubnets: []v1alpha1.ClusterSubnetAllocation{},
			LastUpdated:      metav1.Now(),
		},
	}

	// Set owner reference
	if err := ctrl.SetControllerReference(sliceConfig, sliceIpam, util.GetKubeSliceControllerRequestContext(ctx).Scheme); err != nil {
		logger.Errorf("Error setting controller reference: %v", err)
		return err
	}

	// Create resource
	err = util.CreateResource(ctx, sliceIpam)
	if err != nil {
		logger.Errorf("Failed to create SliceIpam: %v", err)
		return fmt.Errorf("failed to create SliceIpam: %v", err)
	}

	logger.Infof("Successfully created SliceIpam %s", sliceConfig.Name)
	return nil
}

// DeleteSliceIpam deletes a SliceIpam resource
func (s *SliceIpamService) DeleteSliceIpam(ctx context.Context, sliceName, namespace string) error {
	logger := util.CtxLogger(ctx)
	logger.Infof("Deleting SliceIpam %s", sliceName)

	// Get SliceIpam resource
	sliceIpam := &v1alpha1.SliceIpam{}
	key := types.NamespacedName{Name: sliceName, Namespace: namespace}
	found, err := util.GetResourceIfExist(ctx, key, sliceIpam)
	if err != nil {
		logger.Errorf("Error getting SliceIpam resource: %v", err)
		return err
	}
	if !found {
		logger.Infof("SliceIpam %s not found, already deleted", sliceName)
		return nil
	}

	// Delete resource
	err = util.DeleteResource(ctx, sliceIpam)
	if err != nil {
		logger.Errorf("Failed to delete SliceIpam: %v", err)
		return fmt.Errorf("failed to delete SliceIpam: %v", err)
	}

	logger.Infof("Successfully deleted SliceIpam %s", sliceName)
	return nil
}

// convertAllocationsToUtil converts API ClusterSubnetAllocation to util types
func (s *SliceIpamService) convertAllocationsToUtil(allocations []v1alpha1.ClusterSubnetAllocation) []util.ClusterSubnetAllocation {
	utilAllocations := make([]util.ClusterSubnetAllocation, len(allocations))

	for i, allocation := range allocations {
		utilAllocations[i] = util.ClusterSubnetAllocation{
			ClusterName: allocation.ClusterName,
			Subnet:      allocation.Subnet,
			AllocatedAt: allocation.AllocatedAt.Time,
			Status:      string(allocation.Status),
		}

		// Convert ReleasedAt if present
		if allocation.ReleasedAt != nil {
			utilAllocations[i].ReleasedAt = &allocation.ReleasedAt.Time
		}
	}

	return utilAllocations
}

// removeReleasedAllocation removes a released allocation entry when it's being reclaimed
func (s *SliceIpamService) removeReleasedAllocation(sliceIpam *v1alpha1.SliceIpam, subnet string) {
	newAllocations := []v1alpha1.ClusterSubnetAllocation{}

	for _, allocation := range sliceIpam.Status.AllocatedSubnets {
		// Keep all allocations except the released one being reclaimed
		if !(allocation.Subnet == subnet && allocation.Status == v1alpha1.SubnetStatusReleased) {
			newAllocations = append(newAllocations, allocation)
		}
	}

	sliceIpam.Status.AllocatedSubnets = newAllocations
}

// CleanupExpiredReleasedSubnets removes subnet allocations that have been in "Released" state beyond the expiration duration
func (s *SliceIpamService) CleanupExpiredReleasedSubnets(ctx context.Context, sliceName, namespace string, expirationDuration time.Duration) error {
	logger := util.CtxLogger(ctx)
	logger.Debugf("Cleaning up expired released subnets for slice %s", sliceName)

	// Get SliceIpam resource
	sliceIpam := &v1alpha1.SliceIpam{}
	key := types.NamespacedName{Name: sliceName, Namespace: namespace}
	found, err := util.GetResourceIfExist(ctx, key, sliceIpam)
	if err != nil {
		logger.Errorf("Error getting SliceIpam resource: %v", err)
		return err
	}
	if !found {
		logger.Debugf("SliceIpam %s not found, skipping cleanup", sliceName)
		return nil
	}

	// Filter out expired released subnets
	currentTime := time.Now()
	newAllocations := []v1alpha1.ClusterSubnetAllocation{}
	removedCount := 0

	for _, allocation := range sliceIpam.Status.AllocatedSubnets {
		shouldRemove := false

		if allocation.Status == v1alpha1.SubnetStatusReleased && allocation.ReleasedAt != nil {
			// Check if the subnet has been released longer than expiration duration
			if currentTime.Sub(allocation.ReleasedAt.Time) > expirationDuration {
				shouldRemove = true
				removedCount++
				logger.Infof("Removing expired released subnet %s (released %v ago)",
					allocation.Subnet, currentTime.Sub(allocation.ReleasedAt.Time))
			}
		}

		if !shouldRemove {
			newAllocations = append(newAllocations, allocation)
		}
	}

	// Update if any subnets were removed
	if removedCount > 0 {
		sliceIpam.Status.AllocatedSubnets = newAllocations
		sliceIpam.Status.LastUpdated = metav1.Now()

		// Update resource
		err = util.UpdateResource(ctx, sliceIpam)
		if err != nil {
			logger.Errorf("Failed to update SliceIpam after cleanup: %v", err)
			return fmt.Errorf("failed to update SliceIpam after cleanup: %v", err)
		}

		logger.Infof("Successfully cleaned up %d expired released subnets for slice %s", removedCount, sliceName)
	} else {
		logger.Debugf("No expired released subnets found for slice %s", sliceName)
	}

	return nil
}
