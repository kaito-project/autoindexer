package utils

const (
	// AutoIndexerDriftDetectedAnnotation is the annotation key to indicate drift detection
	AutoIndexerDriftDetectedAnnotation = "autoindexer.kaito.sh/drift-detected"
	// AutoIndexerLastDriftDetectedAnnotation is the annotation key to record the last drift detection time
	AutoIndexerLastDriftDetectedAnnotation = "autoindexer.kaito.sh/last-drift-detected"

	// AutoIndexerLastDriftRemediatedAnnotation is the annotation key to record the last drift remediated time
	AutoIndexerLastDriftRemediatedAnnotation = "autoindexer.kaito.sh/last-drift-remediated"

	// AutoIndexerFinalizer is the finalizer name for AutoIndexer resources
	AutoIndexerFinalizer = "finalizer.autoindexer.kaito.sh"
)
