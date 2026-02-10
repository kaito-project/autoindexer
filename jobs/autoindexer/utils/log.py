# Copyright (c) KAITO authors.
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

"""
OpenTelemetry Logging Configuration

This module provides OpenTelemetry native logging configuration for the AutoIndexer project.
"""

import logging
import os

# OpenTelemetry imports
from opentelemetry._logs import set_logger_provider
from opentelemetry.exporter.otlp.proto.grpc._log_exporter import OTLPLogExporter
from opentelemetry.instrumentation.logging import LoggingInstrumentor
from opentelemetry.sdk._logs import LoggerProvider, LoggingHandler
from opentelemetry.sdk._logs.export import BatchLogRecordProcessor, ConsoleLogExporter
from opentelemetry.sdk.resources import Resource
from opentelemetry.semconv.attributes.service_attributes import SERVICE_NAME, SERVICE_VERSION


def configure_otel_logging(log_level: str = "INFO", use_otlp_exporter: bool = False, namespace: str = None, autoindexer_name: str = None):
    """Configure OpenTelemetry native logging.
    
    Args:
        log_level: Logging level (DEBUG, INFO, WARNING, ERROR)
        use_otlp_exporter: Whether to enable OTLP exporter for remote collection
        namespace: Kubernetes namespace name
        autoindexer_name: AutoIndexer instance name
        
    Returns:
        LoggerProvider: Configured OTel logger provider
    """
    
    # Create resource with service and Kubernetes information
    resource_attributes = {
        SERVICE_NAME: "autoindexer",
        SERVICE_VERSION: os.getenv("SERVICE_VERSION", "unknown"),
    }
    
    # Add Kubernetes attributes if available
    if namespace:
        resource_attributes.update({
            "k8s.namespace.name": namespace,
            "k8s.pod.name": os.getenv("HOSTNAME", "unknown"),
            "k8s.container.name": "autoindexer",
        })
    
    # Add autoindexer-specific attributes
    if autoindexer_name:
        resource_attributes["autoindexer.name"] = autoindexer_name
    
    resource = Resource.create(resource_attributes)
    
    # Create logger provider
    logger_provider = LoggerProvider(resource=resource)
    set_logger_provider(logger_provider)
    
    # Configure exporters
    exporters = []
    
    # Always add console exporter for local development/debugging
    console_exporter = ConsoleLogExporter()
    exporters.append(console_exporter)
    
    # Add OTLP exporter if configured
    if use_otlp_exporter or os.getenv("OTEL_EXPORTER_OTLP_LOGS_ENDPOINT"):
        otlp_endpoint = os.getenv(
            "OTEL_EXPORTER_OTLP_LOGS_ENDPOINT",
            os.getenv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://localhost:4317")
        )
        otlp_exporter = OTLPLogExporter(
            endpoint=otlp_endpoint,
            headers=_get_otlp_headers(),
        )
        exporters.append(otlp_exporter)
    
    # Add batch processors for each exporter
    for exporter in exporters:
        processor = BatchLogRecordProcessor(exporter)
        logger_provider.add_log_record_processor(processor)
    
    # Configure Python logging integration
    handler = LoggingHandler(
        level=getattr(logging, log_level.upper(), logging.INFO),
        logger_provider=logger_provider
    )
    
    # Configure root logger
    root_logger = logging.getLogger()
    
    # Remove existing handlers
    for existing_handler in root_logger.handlers[:]:
        root_logger.removeHandler(existing_handler)
    
    root_logger.addHandler(handler)
    root_logger.setLevel(getattr(logging, log_level.upper(), logging.INFO))
    
    # Enable automatic instrumentation for standard logging
    LoggingInstrumentor().instrument(set_logging_format=True)
    
    return logger_provider


def _get_otlp_headers() -> dict[str, str]:
    """Get OTLP headers from environment variables.
    
    Returns:
        dict: Headers for OTLP exporter authentication
    """
    headers = {}
    
    # Standard OTEL headers
    if auth_header := os.getenv("OTEL_EXPORTER_OTLP_HEADERS"):
        for header in auth_header.split(","):
            if "=" in header:
                key, value = header.strip().split("=", 1)
                headers[key] = value
    
    return headers
