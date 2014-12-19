package pipeline

// CIServer represents a CI server
type CIServer interface {
	// DeletePipeline deletes the named pipeline from the CI server
	DeletePipeline(name string) (int, error)

	// Check returns nil when the CI server is setup correctly, an error otherwise
	Check() error
}
