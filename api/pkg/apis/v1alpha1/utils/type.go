type AllState struct {
	MergedState          model.DeploymentState                   `json:"mergedState"`
	previousDesiredState solution.SolutionManagerDeploymentState `json:"previousDesiredState"`
	currentState         solution.SolutionManagerDeploymentState `json:"currentState"`
}
type PlanEnvelope struct {
	Plan                 model.DeploymentPlan                    `json:"plan"`
	Deployment           model.DeploymentSpec                    `json:"deployment"`
	MergedState          model.DeploymentState                   `json:"mergedState"`
	previousDesiredState solution.SolutionManagerDeploymentState `json:"previousDesiredState"`
	AllState             AllState                                `json:"allstate"`
	Remove               bool                                    `json:"remove"`
	Namespace            string                                  `json:"namespace"`
	PlanId               string                                  `json:"planId"`
	Generation           string                                  `json:"generation"` // deployment version
	Hash                 string                                  `json:"hash"`
}