package asyncui

import (
	"context"
	"fmt"

	"github.com/kyma-incubator/hydroform/parallel-install/pkg/components"
	"github.com/kyma-incubator/hydroform/parallel-install/pkg/deployment"
	"github.com/kyma-project/cli/pkg/step"
)

// StepFactory is a factory used to generate a step in the UI.
type StepFactory interface {
	NewStep(msg string) step.Step
}

// End-user messages
const (
	deployPrerequisitesPhaseMsg   string = "Deploying pre-requisites"
	undeployPrerequisitesPhaseMsg string = "Undeploying pre-requisites"
	deployComponentsPhaseMsg      string = "Deploying Kyma"
	undeployComponentsPhaseMsg    string = "Undeploying Kyma"
	deployComponentMsg            string = "Deploying component '%s'"
)

// AsyncUI renders the CLI ui based on receiving events
type AsyncUI struct {
	// used to create UI steps
	StepFactory StepFactory
	// processing context
	context context.Context
	// cancel function the caller can execute to interrupt processing
	Cancel context.CancelFunc
	// channel to retrieve update events
	updates chan deployment.ProcessUpdate
	// channel to pass errors to caller
	Errors chan error
	// internal state
	running bool
	// a failure occurred
	Failed bool
}

// Start renders the CLI UI and provides the channel for receiving events
func (ui *AsyncUI) Start() error {
	if ui.running {
		return fmt.Errorf("Duplicate call of start method detected")
	}
	ui.running = true

	// process async process updates
	ui.updates = make(chan deployment.ProcessUpdate)
	// initialize processing context
	ui.context, ui.Cancel = context.WithCancel(context.Background())

	go func() {
		defer ui.Cancel()
		ongoingSteps := make(map[deployment.InstallationPhase]step.Step)
		for procUpdateEvent := range ui.updates {
			switch procUpdateEvent.Event {
			case deployment.ProcessRunning:
				// Component related update event (components have no ProcessStart/ProcessStop event)
				if procUpdateEvent.Component.Name != "" {
					ui.dispatchError(ui.renderStopEvent(procUpdateEvent, &ongoingSteps))
				}
				continue
			case deployment.ProcessStart:
				ui.dispatchError(ui.renderStartEvent(procUpdateEvent, &ongoingSteps))
			default:
				ui.dispatchError(ui.renderStopEvent(procUpdateEvent, &ongoingSteps))
			}
		}
	}()

	return nil
}

// dispatchError will pass an error to the Caller
func (ui *AsyncUI) dispatchError(err error) {
	if err != nil {
		ui.Failed = true
		// fire error event to caller's error channel
		if ui.Errors != nil {
			ui.Errors <- err
		}
	}
}

// Stop will close the update channel and wait until the the UI rendering is finished
func (ui *AsyncUI) Stop() {
	if !ui.running {
		return
	}
	close(ui.updates)
	<-ui.context.Done()
	ui.running = false
}

// renderStartEvent dispatches an start event to an UI step
func (ui *AsyncUI) renderStartEvent(procUpdEvent deployment.ProcessUpdate, ongoingSteps *map[deployment.InstallationPhase]step.Step) error {
	if _, exists := (*ongoingSteps)[procUpdEvent.Phase]; exists {
		return fmt.Errorf("Illegal state: start-step for installation phase '%s' already exists", procUpdEvent.Phase)
	}
	// create a major step
	var stepMsg string
	switch procUpdEvent.Phase {
	case deployment.InstallPreRequisites:
		stepMsg = deployPrerequisitesPhaseMsg
	case deployment.UninstallPreRequisites:
		stepMsg = undeployPrerequisitesPhaseMsg
	case deployment.InstallComponents:
		stepMsg = deployComponentsPhaseMsg
	case deployment.UninstallComponents:
		stepMsg = undeployComponentsPhaseMsg
	default:
		// non-deployment specific installation phase
		// e.g. steps triggered by CLI before or after the deployment
		stepMsg = string(procUpdEvent.Phase)
	}
	(*ongoingSteps)[procUpdEvent.Phase] = ui.StepFactory.NewStep(stepMsg)
	return nil
}

// renderStartEvent dispatches a stop event to an running step
func (ui *AsyncUI) renderStopEvent(procUpdEvent deployment.ProcessUpdate, ongoingSteps *map[deployment.InstallationPhase]step.Step) error {
	if _, exists := (*ongoingSteps)[procUpdEvent.Phase]; !exists {
		return fmt.Errorf("Illegal state: step for installation phase '%s' does not exist", procUpdEvent.Phase)
	}
	// improve readability
	comp := procUpdEvent.Component
	event := procUpdEvent.Event
	installPhase := procUpdEvent.Phase

	// for events related to major installation phases (they don't contain a reference to a component) just stop the spinner
	if comp.Name == "" {
		if event == deployment.ProcessFinished {
			//all good
			(*ongoingSteps)[installPhase].Success()
			return nil
		}
		//something went wrong
		(*ongoingSteps)[installPhase].Failure()
		return fmt.Errorf("Deployment phase '%s' failed: %s", installPhase, event)
	}

	// for component specific installation event show the result in a dedicated step
	step := ui.StepFactory.NewStep(fmt.Sprintf(deployComponentMsg, comp.Name))
	if comp.Status == components.StatusError {
		step.Failure()
		return fmt.Errorf("Deployment of component '%s' failed", comp.Name)
	}
	step.Success()
	return nil
}

//AddStep adds an additional installation step
func (ui *AsyncUI) AddStep(step string) (step.Step, error) {
	if !ui.running {
		return nil, fmt.Errorf("Cannot add an step because AsyncUI is not running")
	}
	return ui.StepFactory.NewStep(step), nil
}

// UpdateChannel returns the update channel which retrieves process update events
func (ui *AsyncUI) UpdateChannel() (chan<- deployment.ProcessUpdate, error) {
	if !ui.running {
		return nil, fmt.Errorf("Update channel cannot be retrieved because AsyncUI is not running")
	}
	return ui.updates, nil
}

func (ui *AsyncUI) IsRunning() bool {
	return ui.running
}
