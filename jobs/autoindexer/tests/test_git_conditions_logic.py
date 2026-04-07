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
from unittest.mock import Mock, patch
from datetime import UTC, datetime

from autoindexer.data_source_handler.git_handler import GitDataSourceHandler
from autoindexer.k8s.k8s_client import AutoIndexerK8sClient
from autoindexer.rag.rag_client import KAITORAGClient


class TestGitHandlerConditionsLogic:
    """Specific tests for how GitDataSourceHandler processes conditions to make indexing decisions."""

    @pytest.fixture
    def mock_rag_client(self):
        """Mock RAG client."""
        client = Mock(spec=KAITORAGClient)
        client.list_documents.return_value = Mock(total_items=5, documents=[])
        return client

    @pytest.fixture
    def mock_autoindexer_client(self):
        """Mock AutoIndexer K8s client."""
        client = Mock(spec=AutoIndexerK8sClient)
        client.get_autoindexer.return_value = {
            "status": {"conditions": []},
            "metadata": {"generation": 1}
        }
        client._create_condition.return_value = {
            "type": "test", "status": "True", "reason": "test", "message": "test"
        }
        client.update_autoindexer_status.return_value = True
        client.namespace = "test"
        return client

    def test_condition_evaluation_logic_with_error_true(self, mock_rag_client, mock_autoindexer_client):
        """Test the specific condition evaluation logic for AutoIndexerError=True."""
        config = {
            "autoindexer_name": "test-autoindexer",
            "repository": "https://github.com/test/repo.git",
            "lastIndexedCommit": "abc123",
            "conditions": [
                {
                    "type": "AutoIndexerError",
                    "status": "True",
                    "reason": "IndexingErrors",
                    "message": "Network timeout during indexing"
                },
                {
                    "type": "AutoIndexerSucceeded", 
                    "status": "False",
                    "reason": "IndexingFailed",
                    "message": "Indexing was not successful"
                }
            ]
        }
        
        handler = GitDataSourceHandler(
            index_name="test-index",
            config=config,
            rag_client=mock_rag_client,
            autoindexer_client=mock_autoindexer_client
        )
        
        # Test the actual condition evaluation logic from the code
        last_indexing_had_errors = any(
            condition.get("type") == "AutoIndexerError" and condition.get("status") == "True"
            for condition in handler.conditions
        )
        
        assert last_indexing_had_errors is True

    def test_condition_evaluation_logic_with_error_false(self, mock_rag_client, mock_autoindexer_client):
        """Test the condition evaluation logic for AutoIndexerError=False."""
        config = {
            "autoindexer_name": "test-autoindexer", 
            "repository": "https://github.com/test/repo.git",
            "lastIndexedCommit": "abc123",
            "conditions": [
                {
                    "type": "AutoIndexerError",
                    "status": "False",
                    "reason": "IndexingCompleted", 
                    "message": "No errors during indexing"
                },
                {
                    "type": "AutoIndexerSucceeded",
                    "status": "True",
                    "reason": "IndexingCompleted",
                    "message": "Indexing completed successfully"
                }
            ]
        }
        
        handler = GitDataSourceHandler(
            index_name="test-index",
            config=config,
            rag_client=mock_rag_client,
            autoindexer_client=mock_autoindexer_client
        )
        
        # Test the condition evaluation logic
        last_indexing_had_errors = any(
            condition.get("type") == "AutoIndexerError" and condition.get("status") == "True"
            for condition in handler.conditions
        )
        
        assert last_indexing_had_errors is False

    def test_condition_evaluation_with_multiple_error_conditions(self, mock_rag_client, mock_autoindexer_client):
        """Test condition evaluation when multiple AutoIndexerError conditions exist."""
        config = {
            "autoindexer_name": "test-autoindexer",
            "repository": "https://github.com/test/repo.git", 
            "lastIndexedCommit": "abc123",
            "conditions": [
                {
                    "type": "AutoIndexerError",
                    "status": "False",
                    "reason": "IndexingCompleted",
                    "message": "Previous error resolved"
                },
                {
                    "type": "AutoIndexerError", 
                    "status": "True",
                    "reason": "IndexingErrors",
                    "message": "Recent error occurred"
                }
            ]
        }
        
        handler = GitDataSourceHandler(
            index_name="test-index",
            config=config,
            rag_client=mock_rag_client,
            autoindexer_client=mock_autoindexer_client
        )
        
        # Should detect errors if ANY AutoIndexerError condition has status=True
        last_indexing_had_errors = any(
            condition.get("type") == "AutoIndexerError" and condition.get("status") == "True"
            for condition in handler.conditions
        )
        
        assert last_indexing_had_errors is True

    def test_condition_evaluation_with_no_error_conditions(self, mock_rag_client, mock_autoindexer_client):
        """Test condition evaluation when no AutoIndexerError conditions exist."""
        config = {
            "autoindexer_name": "test-autoindexer",
            "repository": "https://github.com/test/repo.git",
            "lastIndexedCommit": "abc123", 
            "conditions": [
                {
                    "type": "AutoIndexerSucceeded",
                    "status": "True",
                    "reason": "IndexingCompleted",
                    "message": "Indexing completed successfully"
                },
                {
                    "type": "ResourceReady",
                    "status": "True", 
                    "reason": "ResourcesAvailable",
                    "message": "All required resources are ready"
                }
            ]
        }
        
        handler = GitDataSourceHandler(
            index_name="test-index",
            config=config,
            rag_client=mock_rag_client,
            autoindexer_client=mock_autoindexer_client
        )
        
        # Should not detect errors when no AutoIndexerError conditions exist
        last_indexing_had_errors = any(
            condition.get("type") == "AutoIndexerError" and condition.get("status") == "True"
            for condition in handler.conditions
        )
        
        assert last_indexing_had_errors is False

    def test_condition_evaluation_case_sensitivity(self, mock_rag_client, mock_autoindexer_client):
        """Test that condition evaluation is case-sensitive as expected."""
        config = {
            "autoindexer_name": "test-autoindexer",
            "repository": "https://github.com/test/repo.git",
            "lastIndexedCommit": "abc123",
            "conditions": [
                {
                    "type": "autoindexererror",  # lowercase
                    "status": "True",
                    "reason": "IndexingErrors",
                    "message": "Error condition"
                }
            ]
        }
        
        handler = GitDataSourceHandler(
            index_name="test-index",
            config=config,
            rag_client=mock_rag_client,
            autoindexer_client=mock_autoindexer_client
        )
        
        # Should NOT detect errors because the type doesn't match exactly
        last_indexing_had_errors = any(
            condition.get("type") == "AutoIndexerError" and condition.get("status") == "True"
            for condition in handler.conditions
        )
        
        assert last_indexing_had_errors is False

    def test_condition_status_case_sensitivity(self, mock_rag_client, mock_autoindexer_client):
        """Test that condition status evaluation is case-sensitive."""
        config = {
            "autoindexer_name": "test-autoindexer",
            "repository": "https://github.com/test/repo.git",
            "lastIndexedCommit": "abc123",
            "conditions": [
                {
                    "type": "AutoIndexerError",
                    "status": "true",  # lowercase
                    "reason": "IndexingErrors",
                    "message": "Error condition"
                }
            ]
        }
        
        handler = GitDataSourceHandler(
            index_name="test-index",
            config=config,
            rag_client=mock_rag_client,
            autoindexer_client=mock_autoindexer_client
        )
        
        # Should NOT detect errors because status doesn't match "True" exactly
        last_indexing_had_errors = any(
            condition.get("type") == "AutoIndexerError" and condition.get("status") == "True"
            for condition in handler.conditions
        )
        
        assert last_indexing_had_errors is False

    def test_condition_malformed_structure_handling(self, mock_rag_client, mock_autoindexer_client):
        """Test that malformed condition structures are handled gracefully."""
        config = {
            "autoindexer_name": "test-autoindexer",
            "repository": "https://github.com/test/repo.git",
            "lastIndexedCommit": "abc123",
            "conditions": [
                {
                    # Missing 'type' field
                    "status": "True",
                    "reason": "IndexingErrors"
                },
                {
                    "type": "AutoIndexerError",
                    # Missing 'status' field
                    "reason": "IndexingErrors"
                },
                {
                    "type": "AutoIndexerError",
                    "status": None,  # Null status
                    "reason": "IndexingErrors"
                }
            ]
        }
        
        handler = GitDataSourceHandler(
            index_name="test-index",
            config=config,
            rag_client=mock_rag_client,
            autoindexer_client=mock_autoindexer_client
        )
        
        # Should handle malformed conditions gracefully and not detect errors
        last_indexing_had_errors = any(
            condition.get("type") == "AutoIndexerError" and condition.get("status") == "True"
            for condition in handler.conditions
        )
        
        assert last_indexing_had_errors is False

    @patch('tempfile.mkdtemp', return_value='/tmp/test')
    @patch('os.path.exists', return_value=True)
    @patch('shutil.rmtree')
    def test_full_workflow_error_condition_forces_full_indexing(self, mock_rmtree, mock_exists, mock_mkdtemp, 
                                                               mock_rag_client, mock_autoindexer_client):
        """Integration test: Error condition in realistic scenario forces full indexing."""
        # Simulate previous indexing that failed due to network issues
        config = {
            "autoindexer_name": "production-docs",
            "repository": "https://github.com/company/docs.git",
            "branch": "main",
            "lastIndexedCommit": "abc123def456",  # This commit exists and indexing was attempted
            "conditions": [
                {
                    "type": "AutoIndexerError",
                    "status": "True", 
                    "reason": "NetworkTimeout",
                    "message": "Failed to clone repository: connection timeout after 30s",
                    "lastTransitionTime": "2024-01-01T10:30:00Z",
                    "observedGeneration": 2
                },
                {
                    "type": "AutoIndexerSucceeded",
                    "status": "False",
                    "reason": "IndexingFailed", 
                    "message": "Indexing was interrupted by error",
                    "lastTransitionTime": "2024-01-01T10:30:00Z",
                    "observedGeneration": 2
                }
            ]
        }
        
        handler = GitDataSourceHandler(
            index_name="docs-index",
            config=config,
            rag_client=mock_rag_client,
            autoindexer_client=mock_autoindexer_client
        )
        
        with patch.object(handler, '_setup_repository') as mock_setup, \
             patch.object(handler, '_index_all_files') as mock_index_all, \
             patch.object(handler, '_index_diff_files') as mock_index_diff:
            
            # Execute the update_index method
            errors = handler.update_index()
        
        # Verify that despite having lastIndexedCommit, it chose full indexing due to error condition
        mock_index_all.assert_called_once()  # Full indexing was triggered
        mock_index_diff.assert_not_called()   # Incremental indexing was NOT triggered
        
        assert errors == []  # No errors in this run

    @patch('tempfile.mkdtemp', return_value='/tmp/test')
    @patch('os.path.exists', return_value=True) 
    @patch('shutil.rmtree')
    def test_full_workflow_success_condition_allows_incremental_indexing(self, mock_rmtree, mock_exists, mock_mkdtemp,
                                                                        mock_rag_client, mock_autoindexer_client):
        """Integration test: Success condition allows incremental indexing."""
        # Simulate previous indexing that completed successfully
        config = {
            "autoindexer_name": "production-docs",
            "repository": "https://github.com/company/docs.git",
            "branch": "main", 
            "lastIndexedCommit": "abc123def456",
            "conditions": [
                {
                    "type": "AutoIndexerSucceeded",
                    "status": "True",
                    "reason": "IndexingCompleted",
                    "message": "Successfully indexed 150 documents from 45 files",
                    "lastTransitionTime": "2024-01-01T09:00:00Z", 
                    "observedGeneration": 1
                },
                {
                    "type": "AutoIndexerError", 
                    "status": "False",
                    "reason": "IndexingCompleted",
                    "message": "No errors encountered during indexing",
                    "lastTransitionTime": "2024-01-01T09:00:00Z",
                    "observedGeneration": 1
                }
            ]
        }
        
        handler = GitDataSourceHandler(
            index_name="docs-index",
            config=config,
            rag_client=mock_rag_client,
            autoindexer_client=mock_autoindexer_client  
        )
        
        with patch.object(handler, '_setup_repository') as mock_setup, \
             patch.object(handler, '_index_all_files') as mock_index_all, \
             patch.object(handler, '_index_diff_files') as mock_index_diff:
            
            # Execute the update_index method
            errors = handler.update_index()
        
        # Verify that incremental indexing was chosen due to success conditions
        mock_index_diff.assert_called_once()   # Incremental indexing was triggered  
        mock_index_all.assert_not_called()     # Full indexing was NOT triggered
        
        assert errors == []