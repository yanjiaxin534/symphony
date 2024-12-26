/*
 * Copyright (c) Microsoft Corporation.
 * Licensed under the MIT license.
 * SPDX-License-Identifier: MIT
 */

package vendors

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/eclipse-symphony/symphony/api/pkg/apis/v1alpha1/managers/activations"
	"github.com/eclipse-symphony/symphony/api/pkg/apis/v1alpha1/managers/campaigns"
	"github.com/eclipse-symphony/symphony/api/pkg/apis/v1alpha1/managers/solution"
	"github.com/eclipse-symphony/symphony/api/pkg/apis/v1alpha1/managers/stage"
	"github.com/eclipse-symphony/symphony/api/pkg/apis/v1alpha1/model"
	"github.com/eclipse-symphony/symphony/api/pkg/apis/v1alpha1/providers/stage/materialize"
	"github.com/eclipse-symphony/symphony/api/pkg/apis/v1alpha1/providers/stage/mock"
	"github.com/eclipse-symphony/symphony/api/pkg/apis/v1alpha1/providers/stage/wait"
	"github.com/eclipse-symphony/symphony/api/pkg/apis/v1alpha1/utils"
	api_utils "github.com/eclipse-symphony/symphony/api/pkg/apis/v1alpha1/utils"
	"github.com/eclipse-symphony/symphony/coa/pkg/apis/v1alpha2"
	"github.com/eclipse-symphony/symphony/coa/pkg/apis/v1alpha2/managers"
	"github.com/eclipse-symphony/symphony/coa/pkg/apis/v1alpha2/providers"
	"github.com/eclipse-symphony/symphony/coa/pkg/apis/v1alpha2/providers/pubsub"
	states "github.com/eclipse-symphony/symphony/coa/pkg/apis/v1alpha2/providers/states"
	"github.com/eclipse-symphony/symphony/coa/pkg/apis/v1alpha2/vendors"
	"github.com/eclipse-symphony/symphony/coa/pkg/logger"
)

const (
	DefaultPlanTimeout = 100 * time.Second
)

var sLog = logger.NewLogger("coa.runtime")

type StageVendor struct {
	vendors.Vendor
	StageManager       *stage.StageManager
	CampaignsManager   *campaigns.CampaignsManager
	ActivationsManager *activations.ActivationsManager
	SolutionManager    *solution.SolutionManager
	PlanManager        *PlanManager
}

func (s *StageVendor) GetInfo() vendors.VendorInfo {
	return vendors.VendorInfo{
		Version:  s.Vendor.Version,
		Name:     "Stage",
		Producer: "Microsoft",
	}
}

func (o *StageVendor) GetEndpoints() []v1alpha2.Endpoint {
	return []v1alpha2.Endpoint{}
}

func NewPlanManager(timeout time.Duration) *PlanManager {
	if timeout == 0 {
		timeout = DefaultPlanTimeout
	}
	return &PlanManager{
		Timeout: timeout,
	}
}
func (s *StageVendor) Init(config vendors.VendorConfig, factories []managers.IManagerFactroy, providers map[string]map[string]providers.IProvider, pubsubProvider pubsub.IPubSubProvider) error {
	err := s.Vendor.Init(config, factories, providers, pubsubProvider)
	if err != nil {
		return err
	}
	for _, m := range s.Managers {
		if c, ok := m.(*stage.StageManager); ok {
			s.StageManager = c
		}
		if c, ok := m.(*campaigns.CampaignsManager); ok {
			s.CampaignsManager = c
		}
		if c, ok := m.(*activations.ActivationsManager); ok {
			s.ActivationsManager = c
		}
		if c, ok := m.(*solution.SolutionManager); ok {
			s.SolutionManager = c
		} else {
			log.Info("some error %+v", m)
		}
	}
	log.Info("s<<<<manager %+v", s.Managers)
	log.Info("s<<<<manager %+v", s)
	if s.StageManager == nil {
		return v1alpha2.NewCOAError(nil, "stage manager is not supplied", v1alpha2.MissingConfig)
	}
	if s.CampaignsManager == nil {
		return v1alpha2.NewCOAError(nil, "campaigns manager is not supplied", v1alpha2.MissingConfig)
	}
	if s.ActivationsManager == nil {
		return v1alpha2.NewCOAError(nil, "activations manager is not supplied", v1alpha2.MissingConfig)
	}
	if s.SolutionManager == nil {
		return v1alpha2.NewCOAError(nil, "solution manager is not supplied", v1alpha2.MissingConfig)
	}
	s.PlanManager = NewPlanManager(0)
	s.Vendor.Context.Subscribe("activation", v1alpha2.EventHandler{
		Handler: func(topic string, event v1alpha2.Event) error {
			ctx := context.TODO()
			if event.Context != nil {
				ctx = event.Context
			}

			var actData v1alpha2.ActivationData
			jData, _ := json.Marshal(event.Body)
			err := json.Unmarshal(jData, &actData)
			if err != nil {
				log.ErrorCtx(ctx, "V (Stage): event body of activation event is not ActivationData ")
				return v1alpha2.NewCOAError(nil, "event body is not an activation job", v1alpha2.BadRequest)
			}
			log.InfofCtx(ctx, "V (Stage): handling activation event for activation %s in namespace %s", actData.Activation, actData.Namespace)
			campaignName := api_utils.ConvertReferenceToObjectName(actData.Campaign)

			campaign, err := s.CampaignsManager.GetState(ctx, campaignName, actData.Namespace)
			if err != nil {
				log.ErrorfCtx(ctx, "V (Stage): unable to find campaign %s with error: %+v", campaignName, err)
				err = s.reportActivationStatusWithBadRequest(actData.Activation, actData.Namespace, err)
				// If report status succeeded, return an empty err so the subscribe function will not be retried
				// The actual error will be stored in Activation cr
				return err
			}
			activation, err := s.ActivationsManager.GetState(ctx, actData.Activation, actData.Namespace)
			if err != nil {
				log.ErrorfCtx(ctx, "V (Stage): unable to find activation: %+v", err)
				return nil
			}

			evt, err := s.StageManager.HandleActivationEvent(ctx, actData, *campaign.Spec, activation)
			if err != nil {
				err = s.reportActivationStatusWithBadRequest(actData.Activation, actData.Namespace, err)
				// If report status succeeded, return an empty err so the subscribe function will not be retried
				// The actual error will be stored in Activation cr
				return err
			}

			if evt != nil {
				s.Vendor.Context.Publish("trigger", v1alpha2.Event{
					Body:    *evt,
					Context: ctx,
				})
			}
			return nil
		},
		Group: "0",
	})
	s.Vendor.Context.Subscribe("trigger", v1alpha2.EventHandler{
		Handler: func(topic string, event v1alpha2.Event) error {
			ctx := context.TODO()
			if event.Context != nil {
				ctx = event.Context
			}

			status := model.StageStatus{
				Stage:         "",
				NextStage:     "",
				Outputs:       map[string]interface{}{},
				Status:        v1alpha2.Untouched,
				StatusMessage: v1alpha2.Untouched.String(),
				ErrorMessage:  "",
				IsActive:      true,
			}
			triggerData := v1alpha2.ActivationData{}
			jData, _ := json.Marshal(event.Body)
			err := json.Unmarshal(jData, &triggerData)
			if err != nil {
				err = v1alpha2.NewCOAError(nil, "event body is not an activation job", v1alpha2.BadRequest)
				sLog.ErrorfCtx(ctx, "V (Stage): failed to deserialize activation data: %v", err)
				err = s.reportActivationStatusWithBadRequest(triggerData.Activation, triggerData.Namespace, err)
				// If report status succeeded, return an empty err so the subscribe function will not be retried
				// The actual error will be stored in Activation cr
				return err
			}
			log.InfoCtx(ctx, "V (Stage): handling trigger event for activation %s stage %s in namespace %s",
				triggerData.Activation, triggerData.Stage, triggerData.Namespace)

			status.Outputs["__namespace"] = triggerData.Namespace
			_, err = s.ActivationsManager.GetState(ctx, triggerData.Activation, triggerData.Namespace)
			if err != nil {
				sLog.ErrorfCtx(ctx, "V (Stage): unable to find activation: %+v", err)
				return nil
			}
			campaignName := api_utils.ConvertReferenceToObjectName(triggerData.Campaign)
			campaign, err := s.CampaignsManager.GetState(ctx, campaignName, triggerData.Namespace)
			if err != nil {
				sLog.ErrorfCtx(ctx, "V (Stage): failed to get campaign spec: %v", err)
				err = s.reportActivationStatusWithBadRequest(triggerData.Activation, triggerData.Namespace, err)
				// If report status succeeded, return an empty err so the subscribe function will not be retried
				// The actual error will be stored in Activation cr
				return err
			}
			status.Stage = triggerData.Stage
			status.ErrorMessage = ""
			status.Status = v1alpha2.Running
			status.StatusMessage = v1alpha2.Running.String()
			if triggerData.NeedsReport {
				sLog.DebugfCtx(ctx, "V (Stage): activation %s, stage %s in namespace %s reporting status: %v", triggerData.Activation, triggerData.Stage, triggerData.Namespace, status)
				s.Vendor.Context.Publish("report", v1alpha2.Event{
					Body:    status,
					Context: ctx,
				})
			} else {
				err = s.ActivationsManager.ReportStageStatus(ctx, triggerData.Activation, triggerData.Namespace, status)
				if err != nil {
					sLog.Errorf("V (Stage): failed to report accepted status: %v (%v)", status.ErrorMessage, err)
					return err
				}
			}

			status, activation := s.StageManager.HandleTriggerEvent(ctx, *campaign.Spec, triggerData)

			if triggerData.NeedsReport {
				sLog.DebugfCtx(ctx, "V (Stage): reporting status: %v", status)
				s.Vendor.Context.Publish("report", v1alpha2.Event{
					Body:    status,
					Context: ctx,
				})

			} else {
				err = s.ActivationsManager.ReportStageStatus(ctx, triggerData.Activation, triggerData.Namespace, status)
				if err != nil {
					sLog.ErrorfCtx(ctx, "V (Stage): failed to report status: %v (%v)", status.ErrorMessage, err)
					return err
				}
				if activation != nil && status.NextStage != "" && status.Status != v1alpha2.Paused {
					s.Vendor.Context.Publish("trigger", v1alpha2.Event{
						Body:    *activation,
						Context: ctx,
					})
				}
			}
			log.InfoCtx(ctx, "V (Stage): Finished handling trigger event")
			return nil
		},
	})
	s.Vendor.Context.Subscribe("job-report", v1alpha2.EventHandler{
		Handler: func(topic string, event v1alpha2.Event) error {
			ctx := context.TODO()
			if event.Context != nil {
				ctx = event.Context
			}
			sLog.DebugfCtx(ctx, "V (Stage): handling job report event: %v", event)
			jData, _ := json.Marshal(event.Body)
			var status model.StageStatus
			json.Unmarshal(jData, &status)
			campaign, ok := status.Outputs["__campaign"].(string)
			if !ok {
				sLog.ErrorfCtx(ctx, "V (Stage): failed to get campaign name from job report")
				return v1alpha2.NewCOAError(nil, "job-report: campaign is not valid", v1alpha2.BadRequest)
			}
			namespace, ok := status.Outputs["__namespace"].(string)
			if !ok {
				sLog.ErrorfCtx(ctx, "V (Stage): failed to get namespace from job report, use default instead")
				namespace = "default"
			}
			activation, ok := status.Outputs["__activation"].(string)
			if !ok {
				sLog.ErrorfCtx(ctx, "V (Stage): failed to get activation name from job report")
				return v1alpha2.NewCOAError(nil, "job-report: activation is not valid", v1alpha2.BadRequest)
			}

			err = s.ActivationsManager.ReportStageStatus(ctx, activation, namespace, status)
			if err != nil {
				sLog.ErrorfCtx(ctx, "V (Stage): failed to report status: %v (%v)", status.ErrorMessage, err)
				return err
			}

			if status.Status == v1alpha2.Done || status.Status == v1alpha2.OK {
				campaignName := api_utils.ConvertReferenceToObjectName(campaign)
				campaign, err := s.CampaignsManager.GetState(ctx, campaignName, namespace)
				if err != nil {
					sLog.ErrorfCtx(ctx, "V (Stage): failed to get campaign spec '%s': %v", campaign, err)
					return err
				}
				if campaign.Spec.SelfDriving {
					activation, err := s.StageManager.ResumeStage(ctx, status, *campaign.Spec)
					if err != nil {
						status.Status = v1alpha2.InternalError
						status.StatusMessage = v1alpha2.InternalError.String()
						status.IsActive = false
						status.ErrorMessage = fmt.Sprintf("failed to resume stage: %v", err)
						sLog.ErrorfCtx(ctx, "V (Stage): failed to resume stage: %v", err)
					}
					if activation != nil {
						s.Vendor.Context.Publish("trigger", v1alpha2.Event{
							Body:    *activation,
							Context: ctx,
						})
					}
				}
			}

			return nil
		},
	})
	s.Vendor.Context.Subscribe("remote-job", v1alpha2.EventHandler{
		Handler: func(topic string, event v1alpha2.Event) error {
			ctx := context.TODO()
			if event.Context != nil {
				ctx = event.Context
			}
			// Unwrap data package from event body
			jData, _ := json.Marshal(event.Body)
			var job v1alpha2.JobData
			json.Unmarshal(jData, &job)
			jData, _ = json.Marshal(job.Body)
			var dataPackage v1alpha2.InputOutputData
			err := json.Unmarshal(jData, &dataPackage)
			if err != nil {
				return err
			}

			// restore schedule
			var schedule = ""
			if v, ok := dataPackage.Inputs["__schedule"]; ok {
				schedule = utils.FormatAsString(v)
			}

			triggerData := v1alpha2.ActivationData{
				Activation:           utils.FormatAsString(dataPackage.Inputs["__activation"]),
				ActivationGeneration: utils.FormatAsString(dataPackage.Inputs["__activationGeneration"]),
				Campaign:             utils.FormatAsString(dataPackage.Inputs["__campaign"]),
				Stage:                utils.FormatAsString(dataPackage.Inputs["__stage"]),
				Inputs:               dataPackage.Inputs,
				Outputs:              dataPackage.Outputs,
				Schedule:             schedule,
				NeedsReport:          true,
				Namespace:            utils.FormatAsString(dataPackage.Inputs["__namespace"]),
			}

			triggerData.Inputs["__origin"] = event.Metadata["origin"]

			switch dataPackage.Inputs["operation"] {
			case "wait":
				triggerData.Provider = "providers.stage.wait"
				config, err := wait.WaitStageProviderConfigFromVendorMap(s.Vendor.Config.Properties)
				if err != nil {
					return err
				}
				triggerData.Config = config
			case "materialize":
				triggerData.Provider = "providers.stage.materialize"
				config, err := materialize.MaterializeStageProviderConfigFromVendorMap(s.Vendor.Config.Properties)
				if err != nil {
					return err
				}
				triggerData.Config = config
			case "mock":
				triggerData.Provider = "providers.stage.mock"
				config, err := mock.MockStageProviderConfigFromMap(s.Vendor.Config.Properties)
				if err != nil {
					return err
				}
				triggerData.Config = config
			default:
				return v1alpha2.NewCOAError(nil, fmt.Sprintf("operation %v is not supported", dataPackage.Inputs["operation"]), v1alpha2.BadRequest)
			}
			status := s.StageManager.HandleDirectTriggerEvent(ctx, triggerData)
			sLog.DebugfCtx(ctx, "V (Stage): reporting status: %v", status)
			s.Vendor.Context.Publish("report", v1alpha2.Event{
				Body:    status,
				Context: ctx,
			})
			return nil
		},
	})
	log.Info("begin to subscribe topic deployment-plan")
	s.Vendor.Context.Subscribe("deployment-plan", v1alpha2.EventHandler{
		Handler: func(topic string, event v1alpha2.Event) error {
			ctx := context.TODO()
			if event.Context != nil {
				ctx = event.Context
			}

			log.InfoCtx(ctx, "begin to execute deployment-plan")
			var planEnvelope PlanEnvelope
			jData, _ := json.Marshal(event.Body)
			log.InfoCtx(ctx, "deployment-plan: get jdata get from plan execute topic jdata &%v", jData)
			err := json.Unmarshal(jData, &planEnvelope)
			if err != nil {
				log.ErrorCtx(ctx, "failed to unmarshal plan envelope :%v", err)
				return err
			}

			log.InfoCtx(ctx, "deployment-plan: get jdata get from plan execute topic &%v", jData)
			planState := PlanState{
				PlanId:     planEnvelope.PlanId,
				StartTime:  time.Now(),
				ExpireTime: time.Now().Add(s.PlanManager.Timeout),
				TotalSteps: len(planEnvelope.Plan.Steps),
				Summary: model.SummarySpec{
					TargetResults: make(map[string]model.TargetResultSpec),
					TargetCount:   len(planEnvelope.Deployment.Targets),
				},
				PreviousDesiredState: planEnvelope.PreviousDesiredState,
				MergedState:          planEnvelope.MergedState,
				Deployment:           planEnvelope.Deployment,
				Namespace:            planEnvelope.Namespace,
			}
			log.InfoCtx(ctx, "<<<< tstore plan id %s plan  %v", planEnvelope.PlanId, planState)
			s.PlanManager.Plans.Store(planEnvelope.PlanId, planState)
			// get provider
			for i, step := range planEnvelope.Plan.Steps {
				stepId := fmt.Sprintf("%s-step-%d", planEnvelope.PlanId, i)
				log.InfoCtx(ctx, "deployment-plan: publish deployment step id2 %s step %+v", stepId, step)
				log.InfoCtx(ctx, " event %+v ", v1alpha2.Event{
					Metadata: map[string]string{
						"planId": planEnvelope.PlanId,
						"stepId": stepId,
					},
					Body: StepEnvelope{
						Step:       step,
						Deployment: planEnvelope.Deployment,
						Remove:     planEnvelope.Remove,
						Namespace:  planEnvelope.Namespace,
					},
					Context: ctx,
				})
				s.Vendor.Context.Publish("deployment-step", v1alpha2.Event{
					Body: StepEnvelope{
						Step:       step,
						Deployment: planEnvelope.Deployment,
						Remove:     planEnvelope.Remove,
						Namespace:  planEnvelope.Namespace,
						PlanId:     planEnvelope.PlanId,
						StepId:     stepId,
						PlanState:  planState,
					},
				})
			}
			return nil
		},
		Group: "stage-vendor",
	})

	s.Vendor.Context.Subscribe("step-result", v1alpha2.EventHandler{
		Handler: func(topic string, event v1alpha2.Event) error {
			// subscribe result and save summary
			//publish result to solution vendor
			ctx := event.Context
			if ctx == nil {
				ctx = context.TODO()
			}

			var stepResult StepResult
			jData, _ := json.Marshal(event.Body)
			log.InfofCtx(ctx, " subscribe step-result %v", jData)
			if err := json.Unmarshal(jData, &stepResult); err != nil {
				log.ErrorfCtx(ctx, " fail to unmarshal step result %v", err)
				return err
			}

			planId := event.Metadata["planId"]
			planStateObj, exists := s.PlanManager.Plans.Load(planId)
			if !exists {
				log.ErrorCtx(ctx, "stage plan %s not fount ", planId)
				return fmt.Errorf("plan not fount %s", planId)
			}
			planState := planStateObj.(PlanState)

			// update plan state
			log.InfoCtx(ctx, "update plan state ")
			if err := s.updatePlanState(ctx, planState, stepResult); err != nil {
				log.ErrorCtx(ctx, " failed to update plan state %v", err)
				return err
			}

			// if plan is complete -> publish plan result

			// if event.Context != nil {
			// 	ctx = event.Context
			// }
			// // Unwrap data package from event body
			// jData, _ := json.Marshal(event.Body)
			// log.InfoCtx(ctx, "<<<< test deployment-plan-result get topic info %s", jData)
			return nil
		},
		Group: "stage-vendor",
	})

	return nil
}

func (s *StageVendor) SaveSummaryInfo(ctx context.Context, planState PlanState, state model.SummaryState) error {
	_, err := s.SolutionManager.StateProvider.Upsert(ctx, states.UpsertRequest{
		Value: states.StateEntry{
			ID: fmt.Sprintf("%s-%s", "summary", planState.Deployment.Instance.ObjectMeta.Name),
			Body: model.SummaryResult{
				Summary:        planState.Summary,
				Generation:     planState.Deployment.Generation,
				Time:           time.Now().UTC(),
				State:          state,
				DeploymentHash: planState.Deployment.Hash,
			},
		},
		Metadata: map[string]interface{}{
			"namespace": planState.Namespace,
			"group":     model.SolutionGroup,
			"version":   "v1",
			"resource":  Summary,
		},
	})
	return err
}

func (s *StageVendor) updatePlanState(ctx context.Context, planState PlanState, stepResult StepResult) error {
	log.InfoCtx(ctx, "update plan state %v with step result %v", planState, stepResult)
	if planState.IsExpired() {
		if err := s.handlePlanTimeout(ctx, planState); err != nil {
			return err
		}
		s.PlanManager.DeletePlan(planState.PlanId)
	}
	log.InfoCtx(ctx, "todo update plan state")
	// update step
	planState.CompletedSteps++
	if stepResult.Success {
		planState.Summary.SuccessCount++
	}

	//update target
	log.InfoCtx(ctx, "update plan state target spec %v ", stepResult.TargetResultSpec)
	planState.Summary.TargetResults[stepResult.Step.Target] = stepResult.TargetResultSpec

	// update summary
	if err := s.SaveSummaryInfo(ctx, planState, model.SummaryStateRunning); err != nil {
		log.ErrorfCtx(ctx, "Failed to save summary progress: %v", err)
	}
	log.InfoCtx(ctx, "if plan state is completed %v ", planState)
	// check if all step is completed
	if planState.isCompleted() {
		log.InfoCtx(ctx, "plan state is completed %v ", planState)
		allSuccess := planState.Summary.SuccessCount == planState.TotalSteps
		planState.Summary.AllAssignedDeployed = allSuccess
		if !allSuccess {
			planState.Status = "failed"
		}
		log.InfoCtx(ctx, "plan state is completed %v ", allSuccess)
		// handle plan completed
		if err := s.handlePlanCompletetion(ctx, planState); err != nil {
			log.ErrorfCtx(ctx, "Failed to handle plan completion: %v", err)
			return err
		}

		s.PlanManager.DeletePlan(planState.PlanId)

	}
	return nil
}

func (p *PlanState) IsExpired() bool {
	return time.Now().After(p.ExpireTime)
}

func (p *PlanState) isCompleted() bool {
	return p.CompletedSteps == p.TotalSteps
}
func (pm *PlanManager) GetPlan(planId string) (*PlanState, bool) {
	if value, ok := pm.Plans.Load(planId); ok {
		return value.(*PlanState), true
	}
	return nil, false
}
func (pm *PlanManager) DeletePlan(planId string) {
	pm.Plans.Delete(planId)
}

func (pm *PlanManager) CreatePlan(planEnvelope PlanEnvelope) *PlanState {
	planState := &PlanState{
		PlanId:     planEnvelope.PlanId,
		StartTime:  time.Now(),
		ExpireTime: time.Now().Add(pm.Timeout),
		TotalSteps: len(planEnvelope.Plan.Steps),
		Summary: model.SummarySpec{
			TargetResults: make(map[string]model.TargetResultSpec),
			TargetCount:   len(planEnvelope.Deployment.Targets),
		},
		MergedState: planEnvelope.MergedState,
		Deployment:  planEnvelope.Deployment,
		Status:      "PlanStatusRunning",
	}
	pm.Plans.Store(planEnvelope.PlanId, planState)
	return planState
}

func (s *StageVendor) handlePlanCompletetion(ctx context.Context, planState PlanState) error {
	log.InfofCtx(ctx, "handle plan completetion:begin to handle plan completetion %v", planState)
	if err := s.SaveSummaryInfo(ctx, planState, model.SummaryStateDone); err != nil {
		log.ErrorfCtx(ctx, "Failed to save summary progress done: %v", err)
		return err
	}
	log.InfofCtx(ctx, "handle plan completetion: begin to handle plan completetion ")
	// update summary
	if err := s.SolutionManager.ConcludeSummary(ctx, planState.Deployment.Instance.ObjectMeta.Name, planState.Deployment.Generation, planState.Deployment.Hash, planState.Summary, planState.Namespace); err != nil {
		log.ErrorfCtx(ctx, "handle plan completetion: failed to conclude summary: %v", err)
		return err
	}
	log.InfofCtx(ctx, "handle plan completetion: update summary done %v", planState)
	return nil
}
func (s *StageVendor) handlePlanTimeout(ctx context.Context, planState PlanState) error {
	planState.Summary.SummaryMessage = fmt.Sprintf("plan execution time out after complete %d/%d steps", planState.CompletedSteps, planState.TotalSteps)

	if err := s.SaveSummaryInfo(ctx, planState, model.SummaryStateDone); err != nil {
		log.ErrorfCtx(ctx, "Failed to save summary progress done: %v", err)
		return err
	}
	return s.Vendor.Context.Publish("deployment-result", v1alpha2.Event{
		Metadata: map[string]string{
			"planId": planState.PlanId,
			"status": "timeout",
		},
		Body: PlanResult{
			EndTime:   time.Now(),
			PlanState: planState,
		},
		Context: ctx,
	})
}

func (s *StageVendor) reportActivationStatusWithBadRequest(activation string, namespace string, err error) error {
	status := model.StageStatus{
		Stage:         "",
		NextStage:     "",
		Outputs:       map[string]interface{}{},
		Status:        v1alpha2.BadRequest,
		StatusMessage: v1alpha2.BadRequest.String(),
		ErrorMessage:  err.Error(),
		IsActive:      false,
	}
	err = s.ActivationsManager.ReportStageStatus(context.TODO(), activation, namespace, status)
	if err != nil {
		sLog.Errorf("V (Stage): failed to report error status on activtion %s/%s: %v (%v)", namespace, activation, status.ErrorMessage, err)
	}
	return err
}
