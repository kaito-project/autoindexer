# Drift Detection System

The drift detection system monitors AutoIndexer resources in the cluster and validates that the document count in their status matches the actual count in the associated RAG engine.

## Overview

The drift detection system consists of several components:

1. **DriftDetector**: A background goroutine that periodically scans all AutoIndexer objects
2. **RAGEngineClient**: Queries RAG engines to get actual document counts
3. **DriftReconciler**: Handles detected drift by triggering appropriate actions

## How it Works

1. **Periodic Scanning**: The drift detector runs every 5 minutes (configurable) and lists all AutoIndexer objects in the cluster.

2. **State Validation**: For each AutoIndexer, the system checks:
   - The AutoIndexer is in a stable state (Completed or Failed phase)
   - The AutoIndexer is not suspended
   - The AutoIndexer has a valid RAG engine reference

3. **Document Count Comparison**: The system:
   - Reads the `NumOfDocumentInIndex` field from the AutoIndexer status
   - Queries the RAG engine to get the actual document count using the index name and autoindexer name as a metadata filter
   - Compares the two counts

4. **Drift Handling**: If drift is detected, the system takes different actions:
   - **Scheduled AutoIndexers**: Updates the status to reflect the actual count and adds a drift condition. The next scheduled run will handle the drift.
   - **One-time AutoIndexers**: Creates a new "drift remediation" job to re-index the documents and brings the index back to the expected state.

## Configuration

The drift detection system can be configured using environment variables:

- `DRIFT_DETECTION_ENABLED`: Set to "false" to disable drift detection (default: true)
- `DRIFT_DETECTION_INTERVAL`: Set the check interval (default: "5m")

## Status Conditions

The drift detection system adds the following conditions to AutoIndexer status:

- **DriftDetected**: Indicates document count drift was detected
- **DriftRemediation**: Indicates a drift remediation job was created for one-time AutoIndexers

## Architecture

```
┌─────────────────┐    ┌──────────────────┐    ┌─────────────────┐
│   AutoIndexer   │    │  DriftDetector   │    │  RAGEngine      │
│   Controller    │    │                  │    │                 │
│                 │    │                  │    │                 │
│ ┌─────────────┐ │    │ ┌──────────────┐ │    │ ┌─────────────┐ │
│ │ Reconciler  │◄┼────┼─│ Periodic     │ │    │ │ Document    │ │
│ │             │ │    │ │ Scanner      │◄┼────┼─│ Count API   │ │
│ └─────────────┘ │    │ └──────────────┘ │    │ └─────────────┘ │
│                 │    │                  │    │                 │
│ ┌─────────────┐ │    │ ┌──────────────┐ │    │                 │
│ │ Job Creator │◄┼────┼─│ Drift        │ │    │                 │
│ │             │ │    │ │ Reconciler   │ │    │                 │
│ └─────────────┘ │    │ └──────────────┘ │    │                 │
└─────────────────┘    └──────────────────┘    └─────────────────┘
```

## Key Features

1. **Non-intrusive**: The drift detection runs independently and doesn't interfere with normal indexing operations
2. **Configurable**: Check interval and enable/disable can be controlled via environment variables
3. **Smart Actions**: Different handling for scheduled vs one-time AutoIndexers
4. **Resilient**: Includes retry logic for RAG engine communication
5. **Observable**: Adds conditions to AutoIndexer status to indicate drift detection results

## Error Handling

- If the RAG engine is unreachable, the drift detector logs the error and continues with other AutoIndexers
- Failed drift detection attempts don't affect the normal operation of AutoIndexers
- Drift remediation jobs follow the same pattern as regular indexing jobs

## Security

The drift detection system uses the same RBAC permissions as the AutoIndexer controller:
- Read access to AutoIndexer resources
- Write access to AutoIndexer status
- Create access to Jobs for drift remediation

## Future Enhancements

Possible future enhancements could include:

1. Configurable drift thresholds (only trigger remediation if drift exceeds a certain amount)
2. Different remediation strategies (e.g., incremental updates vs full re-indexing)
3. Drift detection metrics for monitoring and alerting
4. Integration with external monitoring systems