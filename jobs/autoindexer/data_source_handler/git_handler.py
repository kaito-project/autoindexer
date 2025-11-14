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
import git
import os
import tempfile
import shutil
from datetime import UTC, datetime
from typing import Any
from pathlib import Path

from autoindexer.content_handler.factory import ContentHandlerFactory
from autoindexer.data_source_handler.handler import (
    DataSourceError,
    DataSourceHandler,
)
from autoindexer.k8s.k8s_client import AutoIndexerK8sClient
from autoindexer.rag.rag_client import KAITORAGClient
from autoindexer.rag.utils import get_file_extension_language

from orgecc.filematcher import get_factory, MatcherImplementation
from orgecc.filematcher.patterns.pattern_kit import DenyPatternSourceImpl

from kaito_rag_engine_client.models import Document

logger = logging.getLogger(__name__)

class GitDataSourceHandler(DataSourceHandler):
    """
    Handler for Git data sources.
    
    This handler supports:
    - Cloning Git repositories
    - Checking out specific branches or commits
    - Reading files from the repository
    - Incremental indexing based on commit diffs
    - Full repository indexing for new repositories
    """

    def __init__(self, index_name: str, config: dict[str, Any], rag_client: KAITORAGClient, autoindexer_client: AutoIndexerK8sClient, credentials: str | None = None):
        """Initialize the Git data source handler."""
        self.index_name = index_name
        self.config = config
        self.credentials = credentials
        self.rag_client = rag_client
        self.autoindexer_client = autoindexer_client
        
        # Initialize content handler factory
        self.content_handler_factory = ContentHandlerFactory(config)
        
        # Validate required config
        if not self.config.get("autoindexer_name"):
            raise DataSourceError("Git data source configuration missing 'autoindexer_name' value")
        if not self.config.get("repository"):
            raise DataSourceError("Git data source configuration missing 'repository' value")

        self.autoindexer_name = self.config.get("autoindexer_name")
        self.repository = self.config.get("repository")
        self.branch = self.config.get("branch", "main")
        self.commit = self.config.get("commit")
        self.paths = self.config.get("paths", [])
        self.exclude_paths = self.config.get("excludePaths", [])
        self.exclude_matcher = None
        self.include_matcher = None
        self.last_indexed_commit = self.config.get("lastIndexedCommit", "")
        
        factory = get_factory(MatcherImplementation.PURE_PYTHON)
        if self.exclude_paths:
            exclude_patterns = DenyPatternSourceImpl(patterns=self.exclude_paths)
            self.exclude_matcher = factory.pattern2matcher(exclude_patterns)
        
        if self.paths:
            # Must use deny patterns but it works for inclusion as well
            include_patterns = DenyPatternSourceImpl(patterns=self.paths)
            self.include_matcher = factory.pattern2matcher(include_patterns)
        
        # Working directory for repository operations
        self.work_dir = None
        self.repo = None
        self.errors = []
        self.total_time = None
        self.current_commit_hash = None

        logger.info(f"Initialized git data source handler for repository: {self.repository}")

    def update_index(self) -> list[str]:
        """
        Update the index with documents from the Git repository.

        Returns:
            list[str]: List of error messages, if any
        """
        self.errors = []
        start_time = datetime.now(UTC)
        
        try:
            # Create temporary directory for repository operations
            self.work_dir = tempfile.mkdtemp(prefix="autoindexer_git_")
            logger.info(f"Created working directory: {self.work_dir}")
            
            # Clone or fetch repository
            self._setup_repository()
            
            # Determine indexing strategy based on configuration
            if self.commit:
                # Specific commit requested - index all files at that commit
                logger.info(f"Indexing specific commit: {self.commit}")
                self._index_all_files()
            elif self.last_indexed_commit:
                # Incremental indexing - process diff since last indexed commit
                logger.info(f"Incremental indexing since commit: {self.last_indexed_commit}")
                self._index_diff_files()
            else:
                # Full repository indexing - index all files in current branch/commit
                logger.info("Full repository indexing")
                self._index_all_files()
                
        except DataSourceError as e:
            error_msg = f"Data source error: {e}"
            logger.error(error_msg)
            self.errors.append(error_msg)
        except Exception as e:
            error_msg = f"Unexpected error during Git indexing: {e}"
            logger.error(error_msg, exc_info=True)
            self.errors.append(error_msg)
        finally:
            # Cleanup working directory
            if self.work_dir and os.path.exists(self.work_dir):
                try:
                    shutil.rmtree(self.work_dir)
                    logger.info(f"Cleaned up working directory: {self.work_dir}")
                except Exception as e:
                    logger.warning(f"Failed to cleanup working directory: {e}")
        
        self.total_time = datetime.now(UTC) - start_time
        return self.errors

    def _setup_repository(self):
        """Clone and setup the Git repository."""
        try:
            # Build clone URL with credentials if available
            clone_url = self.repository
            if self.credentials and "https://" in clone_url:
                # Insert credentials into HTTPS URL
                clone_url = clone_url.replace("https://", f"https://{self.credentials}@")
            
            logger.info(f"Cloning repository from {self.repository}")
            self.repo = git.Repo.clone_from(clone_url, self.work_dir)
            
            # Checkout specific branch if specified
            if self.branch != "main" and self.branch not in ["master", "HEAD"]:
                try:
                    self.repo.git.checkout(self.branch)
                    logger.info(f"Checked out branch: {self.branch}")
                except git.exc.GitCommandError as e:
                    logger.warning(f"Failed to checkout branch {self.branch}: {e}")
                    # Continue with default branch
            
            # Checkout specific commit if specified
            if self.commit:
                try:
                    self.repo.git.checkout(self.commit)
                    logger.info(f"Checked out commit: {self.commit}")
                except git.exc.GitCommandError as e:
                    raise DataSourceError(f"Failed to checkout commit {self.commit}: {e}")
            
            self.repo.remotes.origin.fetch()
            self.repo.git.pull()
            # Capture the current commit hash for later use
            self.current_commit_hash = self.repo.head.commit.hexsha
            logger.info(f"Current commit hash: {self.current_commit_hash}")
                    
        except git.exc.GitCommandError as e:
            raise DataSourceError(f"Failed to clone repository {self.repository}: {e}")
        except Exception as e:
            raise DataSourceError(f"Repository setup failed: {e}")

    def _index_all_files(self):
        """Index all files in the repository (full indexing)."""
        try:
            files = self._get_repository_files()
            documents = []
            
            for file_path in files:
                try:
                    content = self._read_file_content(file_path)
                    if content:
                        doc = self._create_document(file_path, content, "full")
                        documents.append(doc)
                        
                        # Batch index documents
                        if len(documents) >= 10:
                            self._index_documents_batch(documents)
                            documents = []
                except Exception as e:
                    logger.warning(f"Failed to process file {file_path}: {e}")
                    continue
            
            # Index remaining documents
            if documents:
                self._index_documents_batch(documents)
                
        except Exception as e:
            raise DataSourceError(f"Failed to index all files: {e}")

    def _index_diff_files(self):
        """Index files based on diff since last indexed commit."""
        try:
            # Get the current commit
            current_commit = self.repo.head.commit.hexsha
            
            # Get diff between last indexed commit and current commit
            try:
                diff_index = self.repo.commit(self.last_indexed_commit).diff(current_commit)
            except git.exc.BadName:
                logger.warning(f"Last indexed commit {self.last_indexed_commit} not found, performing full indexing")
                self._index_all_files()
                return
            
            create_docs = []
            update_docs = []
            delete_doc_ids = []
            
            for diff_item in diff_index:
                file_path = diff_item.b_path or diff_item.a_path
                
                if not self._should_index_file(file_path):
                    continue
                
                # Handle different change types
                if diff_item.change_type == 'A':  # Added
                    content = self._read_file_content(file_path)
                    if content:
                        doc = self._create_document(file_path, content, "added")
                        create_docs.append(doc)
                        
                elif diff_item.change_type == 'M':  # Modified
                    content = self._read_file_content(file_path)
                    if content:
                        doc = self._create_document(file_path, content, "modified")
                        update_docs.append(doc)
                        
                elif diff_item.change_type == 'D':  # Deleted
                    doc_id = self._generate_document_id(file_path)
                    delete_doc_ids.append(doc_id)
                    
                elif diff_item.change_type == 'R':  # Renamed
                    # Delete old document and create new one
                    if diff_item.a_path:
                        old_doc_id = self._generate_document_id(diff_item.a_path)
                        delete_doc_ids.append(old_doc_id)
                    
                    content = self._read_file_content(file_path)
                    if content:
                        doc = self._create_document(file_path, content, "renamed")
                        create_docs.append(doc)
            
            # Process all changes
            if create_docs:
                self._index_documents_batch(create_docs)
            if update_docs:
                self._index_documents_batch(update_docs)
            if delete_doc_ids:
                self._delete_documents_batch(delete_doc_ids)
                
            logger.info(f"Processed diff: {len(create_docs)} created, {len(update_docs)} updated, {len(delete_doc_ids)} deleted")
            
        except Exception as e:
            raise DataSourceError(f"Failed to index diff files: {e}")

    def _get_repository_files(self) -> list[str]:
        """Get all files in the repository."""
        files = []
        
        for root, _, filenames in os.walk(self.work_dir):
            # Skip .git directory
            if '.git' in root:
                continue
            for filename in filenames:
                rel_path = os.path.relpath(os.path.join(root, filename), self.work_dir)
                if self._should_index_file(rel_path):
                    files.append(rel_path)
        logger.info(f"Found {len(files)} files in repository for indexing")
        return files

    def _should_index_file(self, file_path: str) -> bool:
        """Determine if a file should be indexed using gitignore-like pattern matching."""
        # Check file extension first
        logger.debug(f"Checking if file should be indexed: {file_path}")
        
        # Skip hidden files and directories (starting with '.')
        if any(part.startswith('.') for part in file_path.split('/')):
            return False
        
        # Skip common binary file extensions
        binary_extensions = {'.exe', '.so', '.dll', '.bin', '.obj', '.o', '.a', '.lib', '.dylib'}
        _, ext = os.path.splitext(file_path)
        if ext.lower() in binary_extensions:
            return False
        
        if not self.paths and not self.exclude_paths:
            return True
        
        if self.exclude_paths and self.exclude_matcher:
            res = self.exclude_matcher.match(file_path)
            if res.matches:
                return False
        
        if self.paths and self.include_matcher:
            res = self.include_matcher.match(file_path)
            if res.matches:
                return True
            else:
                return False
        
        return True

    def _read_file_content(self, file_path: str) -> str | None:
        """Read content from a file."""
        try:
            full_path = os.path.join(self.work_dir, file_path)
            with open(full_path, 'rb') as f:
                raw_content = f.read()
            
            # Use content handler factory to extract text
            content_type = self._get_content_type(file_path)
            return self.content_handler_factory.extract_text(raw_content, content_type)
            
        except Exception as e:
            logger.warning(f"Failed to read file {file_path}: {e}")
            return None

    def _get_content_type(self, file_path: str) -> str:
        """Get content type based on file extension."""
        _, ext = os.path.splitext(file_path)
        ext = ext.lower()
        
        content_type_map = {
            '.py': 'text/x-python',
            '.go': 'text/x-go',
            '.js': 'application/javascript',
            '.ts': 'application/typescript',
            '.java': 'text/x-java',
            '.cpp': 'text/x-c++',
            '.c': 'text/x-c',
            '.h': 'text/x-c',
            '.hpp': 'text/x-c++',
            '.md': 'text/markdown',
            '.txt': 'text/plain',
            '.rst': 'text/x-rst',
            '.yaml': 'application/x-yaml',
            '.yml': 'application/x-yaml',
            '.json': 'application/json',
            '.toml': 'application/toml',
            '.xml': 'application/xml',
            '.html': 'text/html',
            '.css': 'text/css',
            '.sql': 'application/sql',
            '.sh': 'application/x-sh',
            '.bash': 'application/x-sh',
        }
        
        return content_type_map.get(ext, 'text/plain')

    def _create_document(self, file_path: str, content: str, change_type: str) -> Document:
        """Create a document for indexing."""
        metadata = {
            "autoindexer": self.autoindexer_name,
            "source_type": "git",
            "repository": self.repository,
            "branch": self.branch,
            "file_path": file_path,
            "change_type": change_type,
            "timestamp": self._get_current_timestamp(),
            "commit": self.repo.head.commit.hexsha if self.repo else None,
        }
        
        # Add language metadata for code files
        language = get_file_extension_language(os.path.splitext(file_path)[1])
        if language:
            metadata["language"] = language
            metadata["split_type"] = "code"
        
        doc = Document(
            text=content,
            metadata=metadata
        )
        
        return doc

    def _generate_document_id(self, file_path: str) -> str:
        """Generate a unique document ID for a file."""
        return f"{self.autoindexer_name}:{self.repository}:{file_path}".replace("/", "_").replace(":", "_")

    def _index_documents_batch(self, documents: list[Document]):
        """Index a batch of documents."""
        try:
            logger.info(f"Indexing batch of {len(documents)} documents into index '{self.index_name}'")
            self.rag_client.index_documents(index_name=self.index_name, documents=documents)
        except Exception as e:
            error_msg = f"Failed to index document batch: {e}"
            logger.error(error_msg)
            self.errors.append(error_msg)

    def _delete_documents_batch(self, document_ids: list[str]):
        """Delete a batch of documents."""
        try:
            logger.info(f"Deleting batch of {len(document_ids)} documents from index '{self.index_name}'")
            for doc_id in document_ids:
                self.rag_client.delete_document(index_name=self.index_name, document_id=doc_id)
        except Exception as e:
            error_msg = f"Failed to delete document batch: {e}"
            logger.error(error_msg)
            self.errors.append(error_msg)

    def _get_current_timestamp(self) -> str:
        """Get current timestamp in ISO format."""
        return datetime.now(UTC).isoformat().replace('+00:00', 'Z')
    
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

        if self.current_commit_hash:
            status_update["lastIndexedCommit"] = self.current_commit_hash

        if not self.autoindexer_client.update_autoindexer_status(status_update, update_success_or_failure=True):
            logger.error("Failed to update AutoIndexer status")