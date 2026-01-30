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
from urllib.parse import urlparse

import requests

from autoindexer.content_handler.factory import ContentHandlerFactory
from autoindexer.credential_provider.credential_provider import CredentialProvider
from autoindexer.data_source_handler.handler import (
    DataSourceError,
    DataSourceHandler,
)
from autoindexer.k8s.k8s_client import AutoIndexerK8sClient
from autoindexer.rag.rag_client import KAITORAGClient
from autoindexer.rag.utils import get_file_extension_language

from kaito_rag_engine_client.models import Document

logger = logging.getLogger(__name__)


class StaticDataSourceHandler(DataSourceHandler):
    """
    Handler for static data sources (direct file URLs or content).
    
    This handler supports:
    - HTTP/HTTPS URLs pointing to text files (.txt, .md, .rst, etc.)
    - PDF files with automatic text extraction (.pdf)
    - Configuration files (.json, .yaml, .toml, etc.)
    - Source code files (.py, .go, .js, etc.)
    - Raw files from GitHub, GitLab, and other repositories
    - Direct text content provided in configuration
    """

    def __init__(self, index_name: str, config: dict[str, Any], rag_client: KAITORAGClient, autoindexer_client: AutoIndexerK8sClient, credentials: CredentialProvider | None = None):
        """
        Initialize the static data source handler.
        
        Args:
            config: Configuration dictionary containing static data source settings
            credentials: Optional credential provider for accessing the data source
        """

        self.index_name = index_name
        self.config = config
        self.credentials = credentials
        self.rag_client = rag_client
        self.autoindexer_client = autoindexer_client
        
        # Initialize content handler factory
        self.content_handler_factory = ContentHandlerFactory(config)
        
        if not self.config.get("autoindexer_name"):
            raise DataSourceError("Static data source configuration missing 'autoindexer_name' value")

        self.autoindexer_name = self.config.get("autoindexer_name")

        logger.info(f"Initialized static data source handler with config: {self.config}")
    
    def update_autoindexer_status(self):
        """
        Update the AutoIndexer CRD status based on the current index state and indexing status.
        """
        status_update = self._create_base_autoindexer_status_update(
            index_name=self.index_name,
            autoindexer_name=self.autoindexer_name,
            rag_client=self.rag_client,
            autoindexer_client=self.autoindexer_client,
            errors=self.errors,
            indexing_duration_seconds=self.total_time.seconds if self.total_time else 0
        )

        if not self.autoindexer_client.update_autoindexer_status(status_update):
            logger.error("Failed to update AutoIndexer status")


    def update_index(self) -> list[str]:
        """
        Update the index with documents from the data source.

        Returns:
            list[str]: List of error messages, if any
        """
        self.errors = []
        start_time = datetime.now(UTC)
        
        try:
            documents = []

            if "urls" in self.config:
                url_list = self.config["urls"]
                if isinstance(url_list, str):
                    url_list = [url_list]
                
                for url in url_list:
                    try:
                        content = self._fetch_content_from_url(url)
                        if content:
                            curr_doc = Document(
                                text=content,
                                metadata={
                                    "autoindexer": self.autoindexer_name,
                                    "source_type": "url",
                                    "source_url": url,
                                    "timestamp": self._get_current_timestamp()
                                }
                            )

                            file_extension_from_url = ""
                            url_parts = os.path.splitext(urlparse(url).path)
                            if len(url_parts) > 1:
                                file_extension_from_url = url_parts[1]
                            language = get_file_extension_language(file_extension_from_url)
                            if language:
                                curr_doc.metadata["language"] = language
                                curr_doc.metadata["split_type"] = "code"
                            documents.append(curr_doc)
                            logger.info(f"Successfully fetched content from {url}")

                            if len(documents) >= 10:
                                logger.info(f"Batch indexing {len(documents)} documents into index '{self.index_name}'")
                                self.rag_client.index_documents(index_name=self.index_name, documents=documents)
                                documents = []
                        else:
                            logger.warning(f"No content retrieved from {url}")
                    except Exception as e:
                        logger.error(f"Failed to fetch content from {url}: {e}")
                        self.errors.append(f"Failed to fetch content from {url}: {e}")
                        raise DataSourceError(f"Failed to fetch content from {url}: {e}")
                        
                # Index any remaining documents after processing all URLs
                if len(documents) > 0:
                    logger.info(f"Final batch indexing {len(documents)} documents into index '{self.index_name}'")
                    self.rag_client.index_documents(index_name=self.index_name, documents=documents)
            else:
                logger.warning("No 'urls' found in static data source configuration")
                return ["No documents fetched from static data source"]
            
        except DataSourceError as e:
            error_msg = f"Data source error: {e}"
            logger.error(error_msg)
            self.errors.append(error_msg)
        except Exception as e:
            error_msg = f"Unexpected error during indexing: {e}"
            logger.error(error_msg)
            self.errors.append(error_msg)
        
        self.total_time = datetime.now(UTC) - start_time
        return self.errors

    def _fetch_content_from_url(self, url: str) -> str | None:
        """
        Fetch content from a URL, optimized for downloading files.
        
        Supports various file types including:
        - Raw text files from GitHub, GitLab, etc.
        - Documentation files (.md, .txt, .rst)
        - Configuration files (.json, .yaml, .toml)
        - Source code files (.py, .go, .js, etc.)
        
        Args:
            url: The URL to fetch content from
            
        Returns:
            str | None: The content of the URL or None if failed
        """
        try:
            # Parse URL to validate
            parsed_url = urlparse(url)
            if not parsed_url.scheme or not parsed_url.netloc:
                raise DataSourceError(f"Invalid URL format: {url}")
            
            # Prepare request headers optimized for file downloads
            headers = {
                'User-Agent': 'KAITO-AutoIndexer/1.0',
                'Accept': 'text/plain, text/markdown, text/x-markdown, application/json, text/html, */*',
                'Accept-Encoding': 'gzip, deflate, br',
                'Cache-Control': 'no-cache'
            }
            
            # Add authentication if available
            if self.credentials:
                # Static Data Source Handler should only be able to use token without scopes (secret credentials)
                token = self.credentials.get_token()
                headers['Authorization'] = f'Bearer {token}'
            
            # Make request with timeout and streaming for large files
            timeout = self.config.get("timeout", 60)  # Increased timeout for file downloads
            max_size = self.config.get("max_file_size", 100 * 1024 * 1024)  # 100MB default limit
            
            logger.info(f"Fetching content from {url}")
            with requests.get(url, headers=headers, timeout=timeout, stream=True) as response:
                response.raise_for_status()
                
                # Check content length
                content_length = response.headers.get('content-length')
                if content_length and int(content_length) > max_size:
                    raise DataSourceError(f"File too large: {content_length} bytes exceeds limit of {max_size} bytes")
                
                # Handle different content types and file extensions
                content_type = response.headers.get('content-type', '').lower()
                
                # Download content with size limit protection
                content_chunks = []
                total_size = 0
                
                for chunk in response.iter_content(chunk_size=8192, decode_unicode=False):
                    if chunk:
                        total_size += len(chunk)
                        if total_size > max_size:
                            raise DataSourceError(f"File too large: exceeds limit of {max_size} bytes")
                        content_chunks.append(chunk)
                
                # Combine chunks into bytes
                raw_content = b''.join(content_chunks)
                
                logger.info(f"Fetched {total_size} bytes from {url}, content type: {content_type}")
                # Use content handler factory to extract text
                return self.content_handler_factory.extract_text(raw_content, content_type)
                
        except requests.RequestException as e:
            logger.error(f"HTTP error fetching {url}: {e}")
            raise DataSourceError(f"HTTP error fetching {url}: {e}")
        except Exception as e:
            logger.error(f"Error fetching content from {url}: {e}")
            raise DataSourceError(f"Error fetching content from {url}: {e}")

    def _get_current_timestamp(self) -> str:
        """Get current timestamp in ISO format."""
        return datetime.now(UTC).isoformat().replace('+00:00', 'Z')