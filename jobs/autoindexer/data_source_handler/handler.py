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


from abc import ABC, abstractmethod
from datetime import UTC, datetime

import logging

from autoindexer.k8s.k8s_client import AutoIndexerK8sClient
from autoindexer.rag.rag_client import KAITORAGClient

logger = logging.getLogger(__name__)

class DataSourceError(Exception):
    """Exception raised for data source related errors."""
    pass


class DataSourceHandler(ABC):
    """Abstract base class for data source handlers."""

    @abstractmethod
    def update_index(self) -> list[str]:
        """
        Update the index with documents from the data source.

        Returns:
            list[str]: List of error messages, if any
        """
        pass

    @abstractmethod
    def update_autoindexer_status(self):
        """
        Update the AutoIndexer CRD status based on the current index state and indexing status.
        """
        pass

    def _create_base_autoindexer_status_update(self,
            index_name: str,
            autoindexer_name: str,
            rag_client: KAITORAGClient,
            autoindexer_client: AutoIndexerK8sClient,
            errors: list,
            indexing_duration_seconds: int) -> dict:
        """
        Create the base structure for the AutoIndexer status.

        Returns:
            dict: Base status dictionary
        """
        current_ai = autoindexer_client.get_autoindexer()
        current_status = current_ai["status"]
        current_generation = current_ai.get("metadata", {}).get("generation", 0)

        status_update = {
            "lastIndexingTimestamp": datetime.now(UTC).isoformat().replace("+00:00", "Z"),
            "lastIndexingDurationSeconds": indexing_duration_seconds,
            "numOfDocumentInIndex": 0,
            "conditions": current_status.get("conditions", [])
        }

        try:
            list_docs_resp = rag_client.list_documents(
                index_name, 
                metadata_filter={"autoindexer":autoindexer_name},
                limit=1
            )
            logger.info(f"Document list response: {list_docs_resp}")
            status_update["numOfDocumentInIndex"] = list_docs_resp.total_items
        except Exception as e:
            logger.error(f"Failed to list documents: {e}")
        
        # Create conditions
        conditions = {}
        conditions["AutoIndexerSucceeded"] = autoindexer_client._create_condition(
            "AutoIndexerSucceeded", "True", "IndexingCompleted", "Indexing completed successfully", observed_generation=current_generation
        )
        
        if errors:
            conditions["AutoIndexerError"] = autoindexer_client._create_condition(
                "AutoIndexerError", "True", "IndexingErrors", f"Indexing completed with errors: {errors}", observed_generation=current_generation
            )
        else:
            conditions["AutoIndexerError"] = autoindexer_client._create_condition(
                "AutoIndexerError", "False", "IndexingCompleted", "No errors during indexing", observed_generation=current_generation
            )
            
        conditions["AutoIndexerFailed"] = autoindexer_client._create_condition(
            "AutoIndexerFailed", "False", "IndexingCompleted", "Indexing completed successfully", observed_generation=current_generation
        )
        conditions["AutoIndexerIndexing"] = autoindexer_client._create_condition(
            "AutoIndexerIndexing", "False", "IndexingCompleted", "Document indexing process completed successfully", observed_generation=current_generation
        )

        # Update conditions
        for condition_type, condition_data in conditions.items():
            found = False
            for i, existing_condition in enumerate(status_update["conditions"]):
                if existing_condition.get("type") == condition_type:
                    status_update["conditions"][i] = condition_data
                    found = True
                    break
            
            if not found:
                status_update["conditions"].append(condition_data)

        return status_update