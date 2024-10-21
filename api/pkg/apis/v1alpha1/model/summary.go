/*
 * Copyright (c) Microsoft Corporation.
 * Licensed under the MIT license.
 * SPDX-License-Identifier: MIT
 */

package model

import (
	"fmt"
	"maps"
	"time"

	"github.com/eclipse-symphony/symphony/coa/pkg/apis/v1alpha2"
)

type ComponentResultSpec struct {
	Status  v1alpha2.State `json:"status"`
	Message string         `json:"message"`
}
type TargetResultSpec struct {
	Status           string                         `json:"status"`
	ComponentResults map[string]ComponentResultSpec `json:"components,omitempty"`
}
type SummarySpec struct {
	TargetCount         int                         `json:"targetCount"`
	SuccessCount        int                         `json:"successCount"`
	TargetResults       map[string]TargetResultSpec `json:"targets,omitempty"`
	SummaryMessage      string                      `json:"message,omitempty"`
	Skipped             bool                        `json:"skipped"`
	IsRemoval           bool                        `json:"isRemoval"`
	AllAssignedDeployed bool                        `json:"allAssignedDeployed"`
}
type SummaryResult struct {
	Summary        SummarySpec  `json:"summary"`
	Generation     string       `json:"generation"`
	Time           time.Time    `json:"time"`
	State          SummaryState `json:"state"`
	DeploymentHash string       `json:"deploymentHash"`
}

const (
	SummaryStatePending SummaryState = iota // Currently unused
	SummaryStateRunning                     // Should indicate that a reconcile operation is in progress
	SummaryStateDone                        // Should indicate that a reconcile operation has completed either successfully or unsuccessfully
)

type SummaryState int

func (s *SummarySpec) UpdateTargetResult(target string, spec TargetResultSpec) {
	fmt.Printf("summary spec %+v", s)
	fmt.Printf("old target %+v", s.TargetResults[target])
	fmt.Printf("new target %+v", spec)
	// s.TargetResults[target] = spec
	if v, ok := s.TargetResults[target]; !ok {
		s.TargetResults[target] = spec
	} else {
		v.Status = spec.Status
		fmt.Printf("old target23 %+v", s.TargetResults[target])
		maps.Copy(v.ComponentResults, spec.ComponentResults)
		s.TargetResults[target] = v
		fmt.Printf("new target2 %+v", s.TargetResults[target])
	}
}

func (summary *SummaryResult) IsDeploymentFinished() bool {
	return summary.State == SummaryStateDone
}
