package engine

import "fmt"

// RunState accumulates outputs and resolved inputs from completed steps during execution.
type RunState struct {
	outputs map[string]map[string]any // stepID → outputName → value
	inputs  map[string]map[string]any // stepID → inputName → resolved value
	order   []string                  // step IDs in the order their outputs were first stored
}

// NewRunState creates an empty RunState.
func NewRunState() *RunState {
	return &RunState{
		outputs: make(map[string]map[string]any),
		inputs:  make(map[string]map[string]any),
	}
}

// StoreOutputs records the outputs for a completed step.
func (s *RunState) StoreOutputs(nodeName string, outputs map[string]any) {
	if _, seen := s.outputs[nodeName]; !seen {
		s.order = append(s.order, nodeName)
	}
	s.outputs[nodeName] = outputs
}

// GetOutput retrieves a specific output value from a previously completed step.
func (s *RunState) GetOutput(nodeName, outputName string) (any, error) {
	nodeOutputs, ok := s.outputs[nodeName]
	if !ok {
		return nil, outputMissingError{fmt.Sprintf("no outputs for node %q", nodeName)}
	}
	val, ok := nodeOutputs[outputName]
	if !ok {
		return nil, outputMissingError{fmt.Sprintf("output %q not found for node %q", outputName, nodeName)}
	}
	return val, nil
}

// GetAllOutputs returns all outputs for a completed step.
func (s *RunState) GetAllOutputs(nodeName string) (map[string]any, bool) {
	outputs, ok := s.outputs[nodeName]
	return outputs, ok
}

// StoreInputs records the resolved inputs for a step.
func (s *RunState) StoreInputs(stepID string, inputs map[string]any) {
	s.inputs[stepID] = inputs
}

// GetInput retrieves a specific resolved input value from a previously executed step.
func (s *RunState) GetInput(stepID, inputName string) (any, error) {
	stepInputs, ok := s.inputs[stepID]
	if !ok {
		return nil, fmt.Errorf("no inputs for step %q", stepID)
	}
	val, ok := stepInputs[inputName]
	if !ok {
		return nil, fmt.Errorf("input %q not found for step %q", inputName, stepID)
	}
	return val, nil
}

// ExecutedSteps returns the IDs of all steps that have stored outputs, in
// execution order (earliest first).
func (s *RunState) ExecutedSteps() []string {
	keys := make([]string, len(s.order))
	copy(keys, s.order)
	return keys
}
