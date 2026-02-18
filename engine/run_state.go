package engine

import "fmt"

// RunState accumulates outputs from completed steps during execution.
type RunState struct {
	outputs map[string]map[string]any // nodeName → outputName → value
}

// NewRunState creates an empty RunState.
func NewRunState() *RunState {
	return &RunState{
		outputs: make(map[string]map[string]any),
	}
}

// StoreOutputs records the outputs for a completed step.
func (s *RunState) StoreOutputs(nodeName string, outputs map[string]any) {
	s.outputs[nodeName] = outputs
}

// GetOutput retrieves a specific output value from a previously completed step.
func (s *RunState) GetOutput(nodeName, outputName string) (any, error) {
	nodeOutputs, ok := s.outputs[nodeName]
	if !ok {
		return nil, fmt.Errorf("no outputs for node %q", nodeName)
	}
	val, ok := nodeOutputs[outputName]
	if !ok {
		return nil, fmt.Errorf("output %q not found for node %q", outputName, nodeName)
	}
	return val, nil
}

// GetAllOutputs returns all outputs for a completed step.
func (s *RunState) GetAllOutputs(nodeName string) (map[string]any, bool) {
	outputs, ok := s.outputs[nodeName]
	return outputs, ok
}

// ExecutedSteps returns the IDs of all steps that have stored outputs.
func (s *RunState) ExecutedSteps() []string {
	keys := make([]string, 0, len(s.outputs))
	for k := range s.outputs {
		keys = append(keys, k)
	}
	return keys
}
