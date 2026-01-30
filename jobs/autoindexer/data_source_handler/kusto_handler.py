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


import logging
import os
from datetime import UTC, datetime
from typing import Any

from autoindexer.data_source_handler.handler import (
    DataSourceError,
    DataSourceHandler,
)
from autoindexer.k8s.k8s_client import AutoIndexerK8sClient
from autoindexer.rag.rag_client import KAITORAGClient

from kaito_rag_engine_client.models import Document

from autoindexer.credential_provider.credential_provider import CredentialProvider

logger = logging.getLogger(__name__)


class KustoDataSourceHandler(DataSourceHandler):
    """
    Handler for Azure Data Explorer (Kusto) data source.
    
    This handler supports:
    - Running initial query on first execution
    - Running incremental query with $LAST_CHECKPOINT_TIME substitution on subsequent runs
    - Tracking last execution time via AutoIndexer status
    """

    def __init__(self, index_name: str, config: dict[str, Any], rag_client: KAITORAGClient, autoindexer_client: AutoIndexerK8sClient, credentials: CredentialProvider | None = None):
        """Initialize the Kusto data source handler."""
        self.index_name = index_name
        self.config = config
        self.credentials = credentials
        self.rag_client = rag_client
        self.autoindexer_client = autoindexer_client
        
        # Validate required config
        if not self.config.get("autoindexer_name"):
            raise DataSourceError("Database data source configuration missing 'autoindexer_name' value")
        if not self.config.get("language"):
            raise DataSourceError("Database data source configuration missing 'language' value")
        if self.config.get("language") != "Kusto":
            raise DataSourceError(f"Unsupported database language: {self.config.get('language')}. Only 'Kusto' is currently supported.")
        if not self.config.get("initialQuery"):
            raise DataSourceError("Database data source configuration missing 'initialQuery' value")
        
        self.autoindexer_name = self.config.get("autoindexer_name")
        self.language = self.config.get("language")
        self.initial_query = self.config.get("initialQuery")
        self.incremental_query = self.config.get("incrementalQuery")
        
        self.errors = []
        self.total_time = None

        logger.info(f"Initialized Kusto data source handler (language: {self.language})")

    def _get_last_checkpoint_time(self) -> str | None:
        """
        Get the last indexing timestamp from AutoIndexer status.
        Returns ISO 8601 datetime string or None if this is the first run.
        """
        try:
            if self.autoindexer_client:
                autoindexer = self.autoindexer_client.get_autoindexer()
                if autoindexer:
                    status = autoindexer.get("status", {})
                    last_timestamp = status.get("lastIndexingTimestamp")
                    if last_timestamp:
                        logger.info(f"Found last indexing timestamp: {last_timestamp}")
                        return last_timestamp
                    else:
                        logger.info("No last indexing timestamp found, this is the first run")
                        return None
        except Exception as e:
            logger.warning(f"Failed to get last indexing timestamp: {e}")
        
        return None



    def _build_query(self) -> str:
        """
        Build the KQL query based on whether this is first run or incremental.
        For incremental queries, replaces $LAST_INDEXING_TIMESTAMP with the actual timestamp.
        """
        last_timestamp = self._get_last_checkpoint_time()
        
        if last_timestamp is None:
            # First run: use initial query
            logger.info("🆕 FIRST RUN: Using initialQuery")
            query = self.initial_query
        else:
            # Incremental run: use incremental query with substitution
            logger.info("🔄 INCREMENTAL RUN: Using incrementalQuery")
            query = self.incremental_query.replace("$LAST_INDEXING_TIMESTAMP", last_timestamp)
        
        logger.info(f"Built KQL query:\n{query}")
        return query

    def _execute_kusto_query(self, query: str) -> list[dict[str, Any]]:
        """
        Execute a KQL query against Kusto.
        The query must include cluster() and database() calls.
        
        Supports cross-cluster queries (multiple cluster() calls in the same query).
        The first cluster() will be used as the execution context.
        
        Authentication: Uses AZURE_ACCESS_TOKEN environment variable.
        """
        try:
            from azure.kusto.data import KustoClient, KustoConnectionStringBuilder
        except ImportError:
            raise DataSourceError("azure-kusto-data package is required for Kusto data source")

        # Extract primary cluster URL and database from query
        # Format: cluster('https://...').database('...')
        import re
        
        # Find the first cluster() call - this will be our execution context
        cluster_match = re.search(r"cluster\(['\"]([^'\"()]+)['\"]\)", query)
        if not cluster_match:
            raise DataSourceError("Query must include at least one cluster('...') specification")
        
        cluster_url = cluster_match.group(1).strip()
        
        # Add https:// if not present
        if not cluster_url.startswith("http"):
            cluster_url = f"https://{cluster_url}"
        
        # Ensure .kusto.windows.net suffix
        if not cluster_url.endswith(".kusto.windows.net"):
            cluster_name = cluster_url.replace("https://", "").replace("http://", "")
            cluster_url = f"https://{cluster_name}.kusto.windows.net"
        
        logger.info(f"Extracted primary cluster URL: {cluster_url}")
        
        # Find all cluster() calls to log them
        all_clusters = re.findall(r"cluster\(['\"]([^'\"]+)['\"]\)", query)
        if len(all_clusters) > 1:
            logger.info(f"Cross-cluster query detected. Additional clusters: {all_clusters[1:]}")

        all_clusters_as_scopes = " ".join([f"https://{cluster_url.replace("https://", "").replace("http://", "")}.kusto.windows.net/.default" for cluster_url in all_clusters])
        # Get access token
        if not self.credentials:
            raise DataSourceError("Credentials are required for Kusto authentication")
        
        access_token = self.credentials.get_token(scopes=all_clusters_as_scopes)
        if not access_token:
            raise DataSourceError("Failed to retrieve access token for Kusto authentication")
        
        logger.info("Using Access Token authentication")
        kcsb = KustoConnectionStringBuilder.with_aad_application_token_authentication(
            cluster_url,
            access_token
        )

        if not kcsb:
            raise DataSourceError("Failed to build Kusto connection string")

        # Execute query
        logger.info(f"Executing Kusto query against {cluster_url}")
        client = KustoClient(kcsb)
        
        try:
            # For queries with cluster().database() syntax, execute with the first database name
            # extracted from the query. If query has database() calls, extract the first one.
            # Otherwise, fall back to a dummy database name (query must be fully qualified)
            db_match = re.search(r"\.database\(['\"]([^'\"]+)['\"]\)", query)
            database = db_match.group(1) if db_match else "NetDefaultDB"
            
            logger.info(f"Executing query with database context: {database}")
            response = client.execute(database, query)
            primary_results = response.primary_results[0]
            
            # Convert to list of dictionaries
            records = []
            for row in primary_results:
                record = {}
                for i, col in enumerate(primary_results.columns):
                    record[col.column_name] = row[i]
                records.append(record)
            
            logger.info(f"✓ Query returned {len(records)} records")
            return records
            
        except Exception as e:
            raise DataSourceError(f"Kusto query execution failed: {e}")

    def _display_sample_records(self, records: list[dict[str, Any]], sample_size: int = 3):
        """Display sample records for debugging."""
        if not records:
            logger.info("No records to display")
            return
        
        logger.info(f"\n{'='*80}")
        logger.info(f"Sample Records (showing {min(sample_size, len(records))} of {len(records)} total)")
        logger.info(f"{'='*80}")
        
        for i, record in enumerate(records[:sample_size]):
            logger.info(f"\nRecord {i+1}:")
            for key, value in record.items():
                # Truncate long values
                value_str = str(value)
                if len(value_str) > 100:
                    value_str = value_str[:100] + "..."
                logger.info(f"  {key}: {value_str}")
        
        logger.info(f"{'='*80}\n")

    def _format_documents(self, records: list[dict[str, Any]]) -> list[Document]:
        """
        Format Kusto records into Document objects for indexing.
        
        Following the same pattern as git_handler and static_handler:
        - Metadata: Only identification/classification info (autoindexer, source_type, timestamp)
        - Text: All record data as "key: value" format
        """
        documents = []
        
        for record in records:
            try:
                # Build text content from ALL columns
                text_parts = []
                for key, value in record.items():
                    if value is not None and str(value).strip():
                        text_parts.append(f"{key}: {value}")
                
                if not text_parts:
                    logger.warning("Skipping record with no content")
                    continue
                
                # Metadata: only core identification fields (following git_handler/static_handler pattern)
                metadata = {
                    "autoindexer": self.autoindexer_name,
                    "source_type": "kusto",
                    "timestamp": self._get_current_timestamp()
                }
                
                # Create document
                text_content = "\n".join(text_parts)
                doc = Document(
                    text=text_content,
                    metadata=metadata
                )
                documents.append(doc)
                
            except Exception as e:
                logger.warning(f"Failed to format record: {e}")
                continue
        
        logger.info(f"✓ Formatted {len(documents)} documents from {len(records)} records")
        return documents
    
    def _get_current_timestamp(self) -> str:
        """Get current timestamp in ISO format."""
        return datetime.now(UTC).isoformat().replace('+00:00', 'Z')

    def update_index(self) -> list[str]:
        """
        Main method to fetch data from Kusto and index it.
        
        Returns:
            list[str]: List of error messages (empty if successful)
        """
        from datetime import timedelta
        start_time = datetime.now(UTC)
        
        try:
            logger.info("Starting Kusto data source indexing")
            
            # Step 1: Build query (initial or incremental)
            query = self._build_query()
            
            # Step 2: Execute query
            records = self._execute_kusto_query(query)
            
            if not records:
                logger.warning("No records returned from Kusto query")
                self.total_time = datetime.now(UTC) - start_time
                return []
            
            # Step 3: Display samples
            self._display_sample_records(records)
            
            # Step 4: Format as documents
            documents = self._format_documents(records)
            
            if not documents:
                logger.warning("No documents to index after formatting")
                self.total_time = datetime.now(UTC) - start_time
                return []
            
            # Step 5: Index documents in batches
            logger.info(f"Indexing {len(documents)} documents to RAG engine")
            batch_size = 50  # Index in batches of 50
            errors = []
            
            for i in range(0, len(documents), batch_size):
                batch = documents[i:i + batch_size]
                try:
                    logger.info(f"Indexing batch {i//batch_size + 1}/{(len(documents)-1)//batch_size + 1} ({len(batch)} documents)")
                    self.rag_client.index_documents(index_name=self.index_name, documents=batch)
                except Exception as e:
                    error_msg = f"Failed to index batch {i//batch_size + 1}: {e}"
                    logger.error(error_msg)
                    errors.append(error_msg)
                    self.errors.append(error_msg)
            
            # Calculate total time
            self.total_time = datetime.now(UTC) - start_time
            
            logger.info("✓ Kusto indexing completed successfully")
            return errors
            
        except DataSourceError as e:
            error_msg = f"Kusto data source error: {e}"
            logger.error(error_msg)
            self.total_time = datetime.now(UTC) - start_time
            return [error_msg]
        except Exception as e:
            error_msg = f"Unexpected error in Kusto handler: {e}"
            logger.exception(error_msg)
            self.total_time = datetime.now(UTC) - start_time
            return [error_msg]

    def update_autoindexer_status(self):
        """
        Update the AutoIndexer CRD status based on the current index state and indexing status.
        """
        from datetime import timedelta
        
        # Calculate duration
        duration_seconds = 0
        if self.total_time:
            if isinstance(self.total_time, timedelta):
                duration_seconds = int(self.total_time.total_seconds())
            else:
                duration_seconds = int(self.total_time)
        
        status_update = self._create_base_autoindexer_status_update(
            index_name=self.index_name,
            autoindexer_name=self.autoindexer_name,
            rag_client=self.rag_client,
            autoindexer_client=self.autoindexer_client,
            errors=self.errors,
            indexing_duration_seconds=duration_seconds
        )

        if not self.autoindexer_client.update_autoindexer_status(status_update):
            logger.error("Failed to update AutoIndexer status")
