// Package pipeline creates CI pipeline from a json configuration file
package pipeline

// Pipeline represents a CI pipeline interface
type Pipeline interface {
	// CreatePipeline creates a pipeline on a ci system
	CreatePipeline(pipelineName string) (string, error)

	// UnmarshalJSON translates the pipeline configuration to the CI server structure
	UnmarshalJSON(jsonString []byte) error

	// UpdatePipeline updates pipeline
	UpdatePipeline(pipelineName string) (string, error)
}
