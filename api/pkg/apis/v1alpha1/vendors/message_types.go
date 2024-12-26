/*
* Copyright (c) Microsoft Corporation.
* Licensed under the MIT license.
* SPDX-License-Identifier: MIT
 */

package vendors

import (
	"sync"
	"time"

	"github.com/eclipse-symphony/symphony/api/pkg/apis/v1alpha1/managers/solution"
	"github.com/eclipse-symphony/symphony/api/pkg/apis/v1alpha1/model"
)

const (
	SYMPHONY_AGENT string = "/symphony-agent:"
	ENV_NAME       string = "SYMPHONY_AGENT_ADDRESS"

	// DeploymentType_Update indicates the type of deployment is Update. This is
	// to give a deployment status on Symphony Target deployment.
	DeploymentType_Update string = "Target Update"
	// DeploymentType_Delete indicates the type of deployment is Delete. This is
	// to give a deployment status on Symphony Target deployment.
	DeploymentType_Delete string = "Target Delete"

	Summary         = "Summary"
	DeploymentState = "DeployState"
)

type PlanEnvelope struct {
	Plan                 model.DeploymentPlan                     `json:"plan"`
	Deployment           model.DeploymentSpec                     `json:"deployment"`
	MergedState          model.DeploymentState                    `json:"mergedState"`
	PreviousDesiredState *solution.SolutionManagerDeploymentState `json:"previousDesiredState"`
	Remove               bool                                     `json:"remove"`
	Namespace            string                                   `json:"namespace"`
	PlanId               string                                   `json:"planId"`
	Generation           string                                   `json:"generation"` // deployment version
	Hash                 string                                   `json:"hash"`
}
type PlanResult struct {
	PlanState *PlanState `json:"planstate"`
	EndTime   time.Time  `json:"endTime"`
	Error     string     `json:"error,omitempty"`
}
type StepEnvelope struct {
	Step       model.DeploymentStep `json:"step"`
	Deployment model.DeploymentSpec `json:"deployment"`
	Remove     bool                 `json:"remove"`
	Namespace  string               `json:"Namespace"`
	PlanId     string               `json:"planId"`
	StepId     string               `json:"stepId"`
	PlanState  PlanState            `json:"planState"`
	// Provider   providers.IProvider  `json:"provider"`
}

type PlanManager struct {
	Plans   sync.Map // map[string] *Planstate
	Timeout time.Duration
}

type StepResult struct {
	Step             model.DeploymentStep                 `json:"step"`
	Success          bool                                 `json:"success"`
	TargetResultSpec model.TargetResultSpec               `json:"targetResult"`
	Components       map[string]model.ComponentResultSpec `json:"components"`
	PlanId           string                               `json:"planId"`
	StepId           string                               `json:"stepId"`
	Timestamp        time.Time                            `json:"timestamp"`
}

type PlanState struct {
	PlanId               string                                   `json:"planId"`
	StartTime            time.Time                                `json:"startTime"`
	ExpireTime           time.Time                                `json:"expireTime"`
	TotalSteps           int                                      `json:"totalSteps"`
	CompletedSteps       int                                      `json:"completedSteps"`
	Summary              model.SummarySpec                        `json:"summary"`
	MergedState          model.DeploymentState                    `json:"mergedState"`
	Deployment           model.DeploymentSpec                     `json:"deployment"`
	PreviousDesiredState *solution.SolutionManagerDeploymentState `json:"previous`
	Status               string                                   `json:"status"`
	Namespace            string                                   `json:"namespace"`
}

var deploymentTypeMap = map[bool]string{
	true:  DeploymentType_Delete,
	false: DeploymentType_Update,
}
