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

import pytest
from unittest.mock import Mock, patch, MagicMock

from autoindexer.data_source_handler.git_handler import GitDataSourceHandler
from autoindexer.data_source_handler.handler import DataSourceError
from autoindexer.k8s.k8s_client import AutoIndexerK8sClient
from autoindexer.rag.rag_client import KAITORAGClient


class TestGitDataSourceHandler:
    """Test cases for GitDataSourceHandler."""

    @pytest.fixture
    def mock_rag_client(self):
        """Fixture providing a mock RAG client."""
        client = Mock(spec=KAITORAGClient)
        client.index_documents.return_value = {"success": True, "indexed": 1}
        client.list_documents.return_value = {"total_items": 5}
        return client

    @pytest.fixture
    def mock_autoindexer_client(self):
        """Fixture providing a mock AutoIndexer K8s client."""
        client = Mock(spec=AutoIndexerK8sClient)
        client.get_autoindexer.return_value = {
            "status": {
                "successfulIndexingCount": 0,
                "conditions": []
            }
        }
        client._create_condition.return_value = {
            "type": "test",
            "status": "True",
            "reason": "test",
            "message": "test"
        }
        client.update_autoindexer_status.return_value = True
        return client

    @pytest.fixture
    def basic_config(self):
        """Fixture providing basic configuration."""
        return {
            "autoindexer_name": "test-autoindexer",
            "repository": "https://github.com/test/repo.git",
            "branch": "main"
        }

    def test_init_success(self, basic_config, mock_rag_client, mock_autoindexer_client):
        """Test successful initialization."""
        handler = GitDataSourceHandler(
            index_name="test-index",
            config=basic_config,
            rag_client=mock_rag_client,
            autoindexer_client=mock_autoindexer_client
        )
        
        assert handler.index_name == "test-index"
        assert handler.repository == "https://github.com/test/repo.git"
        assert handler.branch == "main"
        assert handler.autoindexer_name == "test-autoindexer"

    def test_init_missing_autoindexer_name(self, mock_rag_client, mock_autoindexer_client):
        """Test initialization failure with missing autoindexer_name."""
        config = {
            "repository": "https://github.com/test/repo.git"
        }
        
        with pytest.raises(DataSourceError, match="missing 'autoindexer_name' value"):
            GitDataSourceHandler(
                index_name="test-index",
                config=config,
                rag_client=mock_rag_client,
                autoindexer_client=mock_autoindexer_client
            )

    def test_init_missing_repository(self, mock_rag_client, mock_autoindexer_client):
        """Test initialization failure with missing repository."""
        config = {
            "autoindexer_name": "test-autoindexer"
        }
        
        with pytest.raises(DataSourceError, match="missing 'repository' value"):
            GitDataSourceHandler(
                index_name="test-index",
                config=config,
                rag_client=mock_rag_client,
                autoindexer_client=mock_autoindexer_client
            )

    def test_should_index_file(self, basic_config, mock_rag_client, mock_autoindexer_client):
        """Test file filtering logic."""
        handler = GitDataSourceHandler(
            index_name="test-index",
            config=basic_config,
            rag_client=mock_rag_client,
            autoindexer_client=mock_autoindexer_client
        )
        
        # Should index these files
        assert handler._should_index_file("main.py") is True
        assert handler._should_index_file("src/handler.go") is True
        assert handler._should_index_file("README.md") is True
        
        # Should not index these files
        assert handler._should_index_file(".hidden/file.py") is False
        assert handler._should_index_file("file.exe") is False
        assert handler._should_index_file("binary.so") is False

    def test_should_index_file_with_exclude_paths(self, mock_rag_client, mock_autoindexer_client):
        """Test file filtering with exclude paths."""
        config = {
            "autoindexer_name": "test-autoindexer",
            "repository": "https://github.com/test/repo.git",
            "excludePaths": ["vendor/", "node_modules/"]
        }
        
        handler = GitDataSourceHandler(
            index_name="test-index",
            config=config,
            rag_client=mock_rag_client,
            autoindexer_client=mock_autoindexer_client
        )
        
        # Should not index excluded paths
        assert handler._should_index_file("vendor/lib.go") is False
        assert handler._should_index_file("node_modules/package.js") is False
        
        # Should index non-excluded files
        assert handler._should_index_file("src/main.go") is True

    def test_get_content_type(self, basic_config, mock_rag_client, mock_autoindexer_client):
        """Test content type detection."""
        handler = GitDataSourceHandler(
            index_name="test-index",
            config=basic_config,
            rag_client=mock_rag_client,
            autoindexer_client=mock_autoindexer_client
        )
        
        assert handler._get_content_type("main.py") == "text/x-python"
        assert handler._get_content_type("handler.go") == "text/x-go"
        assert handler._get_content_type("README.md") == "text/markdown"
        assert handler._get_content_type("unknown.xyz") == "text/plain"

    @patch('tempfile.mkdtemp')
    @patch('git.Repo.clone_from')
    @patch('shutil.rmtree')
    def test_update_index_with_commit(self, mock_rmtree, mock_clone, mock_mkdtemp, 
                                    basic_config, mock_rag_client, mock_autoindexer_client):
        """Test update_index with specific commit."""
        # Setup mocks
        test_work_dir = "/tmp/test"
        mock_mkdtemp.return_value = test_work_dir
        mock_repo = MagicMock()
        mock_repo.head.commit.hexsha = "abc123"
        mock_clone.return_value = mock_repo
        
        # Add commit to config
        config = basic_config.copy()
        config["commit"] = "abc123"
        
        handler = GitDataSourceHandler(
            index_name="test-index",
            config=config,
            rag_client=mock_rag_client,
            autoindexer_client=mock_autoindexer_client
        )
        
        with patch.object(handler, '_index_all_files') as mock_index_all:
            # Mock os.path.exists to return True for cleanup
            with patch('os.path.exists', return_value=True):
                errors = handler.update_index()
            
            assert errors == []
            mock_index_all.assert_called_once()
            mock_clone.assert_called_once()
            mock_rmtree.assert_called_once_with(test_work_dir)

    def test_create_document(self, basic_config, mock_rag_client, mock_autoindexer_client):
        """Test document creation."""
        handler = GitDataSourceHandler(
            index_name="test-index",
            config=basic_config,
            rag_client=mock_rag_client,
            autoindexer_client=mock_autoindexer_client
        )
        
        # Mock repo
        handler.repo = MagicMock()
        handler.repo.head.commit.hexsha = "abc123"
        
        doc = handler._create_document("main.py", "print('hello')", "added")
        
        assert doc.text == "print('hello')"
        assert doc.metadata["autoindexer"] == "test-autoindexer"
        assert doc.metadata["source_type"] == "git"
        assert doc.metadata["repository"] == "https://github.com/test/repo.git"
        assert doc.metadata["file_path"] == "main.py"
        assert doc.metadata["change_type"] == "added"
        assert doc.metadata["commit"] == "abc123"