package utils

const (
	// AutoIndexerDriftDetectedAnnotation is the annotation key to indicate drift detection
	AutoIndexerDriftDetectedAnnotation = "autoindexer.kaito.sh/drift-detected"
	// AutoIndexerLastDriftDetectedAnnotation is the annotation key to record the last drift detection time
	AutoIndexerLastDriftDetectedAnnotation = "autoindexer.kaito.sh/last-drift-detected"
	// AutoIndexerLastDriftClearedAnnotation is the annotation key to record the last drift cleared time
	AutoIndexerLastDriftClearedAnnotation = "autoindexer.kaito.sh/last-drift-cleared"
)
