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
	"k8s.io/apimachinery/pkg/runtime"
	"net"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	controllerv1alpha1 "github.com/kubeslice/kubeslice-controller/apis/controller/v1alpha1"
	"github.com/kubeslice/kubeslice-controller/util"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func ValidateSliceIpamCreate(ctx context.Context, r *controllerv1alpha1.SliceIpam) (admission.Warnings, error) {
	// Validate that the slice name is not empty
	if r.Spec.SliceName == "" {
		return nil, fmt.Errorf("invalid config, .spec.sliceName could not be empty")
	}

	// Validate that the name matches the slice name
	if r.Name != r.Spec.SliceName {
		return nil, fmt.Errorf("invalid config, name should match with slice name")
	}

	// Validate that the corresponding SliceConfig exists
	slice := &controllerv1alpha1.SliceConfig{}
	found, err := util.GetResourceIfExist(ctx, client.ObjectKey{
		Name:      r.Spec.SliceName,
		Namespace: r.Namespace,
	}, slice)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, fmt.Errorf("invalid config, sliceconfig %s not present", r.Spec.SliceName)
	}

	// Validate IPAM configuration
	if err := validateIpamConfig(r); err != nil {
		return nil, err
	}

	return nil, nil
}

func ValidateSliceIpamUpdate(ctx context.Context, r *controllerv1alpha1.SliceIpam, old runtime.Object) (admission.Warnings, error) {
	// Validate that slice name cannot be changed
	if r.Spec.SliceName == "" {
		return nil, fmt.Errorf("invalid config, .spec.sliceName could not be empty")
	}

	// Additional validation logic can be added here for updates
	// The basic immutable field validation is already handled in the webhook

	return nil, nil
}

func ValidateSliceIpamDelete(ctx context.Context, r *controllerv1alpha1.SliceIpam) (admission.Warnings, error) {
	// Check if the corresponding SliceConfig still exists
	slice := &controllerv1alpha1.SliceConfig{}
	found, err := util.GetResourceIfExist(ctx, client.ObjectKey{
		Name:      r.Spec.SliceName,
		Namespace: r.Namespace,
	}, slice)
	if err != nil {
		return nil, err
	}

	// If SliceConfig exists and is not being deleted, prevent SliceIpam deletion
	if found && slice.ObjectMeta.DeletionTimestamp.IsZero() {
		return nil, fmt.Errorf("sliceipam %s not allowed to delete unless sliceconfig is deleted", r.Name)
	}

	// If SliceConfig is not found or is being deleted, allow SliceIpam deletion
	return nil, nil
}

// validateIpamConfig validates the IPAM configuration fields
func validateIpamConfig(r *controllerv1alpha1.SliceIpam) error {
	// Validate SubnetSize range (based on kubebuilder validation)
	if r.Spec.SubnetSize < 16 || r.Spec.SubnetSize > 30 {
		return fmt.Errorf("invalid config, subnetSize must be between 16 and 30")
	}

	// Validate SliceSubnet is a valid CIDR
	if r.Spec.SliceSubnet != "" {
		_, _, err := net.ParseCIDR(r.Spec.SliceSubnet)
		if err != nil {
			return fmt.Errorf("invalid config, sliceSubnet must be a valid CIDR: %v", err)
		}
	}

	return nil
}
