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
from datetime import UTC, datetime

from autoindexer.data_source_handler.git_handler import GitDataSourceHandler
from autoindexer.data_source_handler.static_handler import StaticDataSourceHandler 
from autoindexer.data_source_handler.handler import DataSourceError
from autoindexer.k8s.k8s_client import AutoIndexerK8sClient
from autoindexer.rag.rag_client import KAITORAGClient
from autoindexer.main import AutoIndexerJob


class TestConditionsInDataSourceConfig:
    """Test cases for conditions checks within datasource_config and data_source_handlers."""

    @pytest.fixture
    def mock_rag_client(self):
        """Fixture providing a mock RAG client."""
        client = Mock(spec=KAITORAGClient)
        client.index_documents.return_value = {"success": True, "indexed": 1}
        client.list_documents.return_value = Mock(total_items=5, documents=[])
        return client

    @pytest.fixture
    def mock_autoindexer_client(self):
        """Fixture providing a mock AutoIndexer K8s client."""
        client = Mock(spec=AutoIndexerK8sClient)
        client.get_autoindexer.return_value = {
            "status": {
                "successfulIndexingCount": 0,
                "conditions": [
                    {
                        "type": "AutoIndexerSucceeded",
                        "status": "True", 
                        "reason": "IndexingCompleted",
                        "message": "Indexing completed successfully",
                        "lastTransitionTime": "2024-01-01T00:00:00Z",
                        "observedGeneration": 1
                    }
                ]
            },
            "metadata": {"generation": 1}
        }
        client._create_condition.return_value = {
            "type": "test",
            "status": "True",
            "reason": "test",
            "message": "test",
            "lastTransitionTime": "2024-01-01T00:00:00Z",
            "observedGeneration": 1
        }
        client.update_autoindexer_status.return_value = True
        client.namespace = "test-namespace"
        return client

    @pytest.fixture
    def conditions_with_error(self):
        """Fixture providing conditions that include an error."""
        return [
            {
                "type": "AutoIndexerError",
                "status": "True",
                "reason": "IndexingErrors", 
                "message": "Previous indexing failed with errors",
                "lastTransitionTime": "2024-01-01T00:00:00Z",
                "observedGeneration": 1
            },
            {
                "type": "AutoIndexerSucceeded",
                "status": "False",
                "reason": "IndexingFailed",
                "message": "Indexing was not successful",
                "lastTransitionTime": "2024-01-01T00:00:00Z",
                "observedGeneration": 1
            }
        ]

    @pytest.fixture
    def conditions_success_only(self):
        """Fixture providing conditions with successful indexing only."""
        return [
            {
                "type": "AutoIndexerSucceeded", 
                "status": "True",
                "reason": "IndexingCompleted",
                "message": "Indexing completed successfully",
                "lastTransitionTime": "2024-01-01T00:00:00Z",
                "observedGeneration": 1
            },
            {
                "type": "AutoIndexerError",
                "status": "False", 
                "reason": "IndexingCompleted",
                "message": "No errors during indexing",
                "lastTransitionTime": "2024-01-01T00:00:00Z",
                "observedGeneration": 1
            }
        ]

    def test_git_handler_config_includes_conditions(self, mock_rag_client, mock_autoindexer_client):
        """Test that GitDataSourceHandler properly stores conditions from config."""
        test_conditions = [
            {
                "type": "AutoIndexerError",
                "status": "True",
                "reason": "TestError",
                "message": "Test error message"
            }
        ]
        
        config = {
            "autoindexer_name": "test-autoindexer",
            "repository": "https://github.com/test/repo.git",
            "branch": "main",
            "conditions": test_conditions
        }
        
        handler = GitDataSourceHandler(
            index_name="test-index",
            config=config,
            rag_client=mock_rag_client,
            autoindexer_client=mock_autoindexer_client
        )
        
        assert handler.conditions == test_conditions
        assert len(handler.conditions) == 1
        assert handler.conditions[0]["type"] == "AutoIndexerError"

    def test_static_handler_config_includes_conditions(self, mock_rag_client, mock_autoindexer_client):
        """Test that StaticDataSourceHandler properly stores conditions from config."""
        test_conditions = [
            {
                "type": "AutoIndexerSucceeded", 
                "status": "True",
                "reason": "IndexingCompleted",
                "message": "Previous indexing successful"
            }
        ]
        
        config = {
            "autoindexer_name": "test-autoindexer",
            "urls": ["https://example.com/test.txt"],
            "conditions": test_conditions  
        }
        
        handler = StaticDataSourceHandler(
            index_name="test-index",
            config=config,
            rag_client=mock_rag_client,
            autoindexer_client=mock_autoindexer_client
        )
        
        # StaticDataSourceHandler doesn't explicitly store conditions yet, 
        # but they should be available in config
        assert "conditions" in handler.config
        assert handler.config["conditions"] == test_conditions

    def test_git_handler_error_condition_forces_full_index(self, conditions_with_error, mock_rag_client, mock_autoindexer_client):
        """Test that previous error conditions force full indexing instead of incremental."""
        config = {
            "autoindexer_name": "test-autoindexer",
            "repository": "https://github.com/test/repo.git",
            "branch": "main", 
            "lastIndexedCommit": "abc123",  # This would normally trigger incremental indexing
            "conditions": conditions_with_error
        }
        
        handler = GitDataSourceHandler(
            index_name="test-index",
            config=config,
            rag_client=mock_rag_client,
            autoindexer_client=mock_autoindexer_client
        )
        
        with patch.object(handler, '_setup_repository') as mock_setup, \
             patch.object(handler, '_index_all_files') as mock_index_all, \
             patch.object(handler, '_index_diff_files') as mock_index_diff, \
             patch('tempfile.mkdtemp', return_value='/tmp/test'), \
             patch('os.path.exists', return_value=True), \
             patch('shutil.rmtree'):
            
            errors = handler.update_index()
            
            # Should call full indexing, not incremental 
            mock_index_all.assert_called_once()
            mock_index_diff.assert_not_called()
            
            assert errors == []

    def test_git_handler_success_condition_allows_incremental_index(self, conditions_success_only, mock_rag_client, mock_autoindexer_client):
        """Test that successful conditions allow incremental indexing."""
        config = {
            "autoindexer_name": "test-autoindexer",
            "repository": "https://github.com/test/repo.git",
            "branch": "main",
            "lastIndexedCommit": "abc123",
            "conditions": conditions_success_only  
        }
        
        handler = GitDataSourceHandler(
            index_name="test-index",
            config=config,
            rag_client=mock_rag_client,
            autoindexer_client=mock_autoindexer_client
        )
        
        with patch.object(handler, '_setup_repository') as mock_setup, \
             patch.object(handler, '_index_all_files') as mock_index_all, \
             patch.object(handler, '_index_diff_files') as mock_index_diff, \
             patch('tempfile.mkdtemp', return_value='/tmp/test'), \
             patch('os.path.exists', return_value=True), \
             patch('shutil.rmtree'):
            
            errors = handler.update_index()
            
            # Should call incremental indexing, not full
            mock_index_diff.assert_called_once()
            mock_index_all.assert_not_called()
            
            assert errors == []

    def test_git_handler_no_conditions_defaults_to_incremental_index(self, mock_rag_client, mock_autoindexer_client):
        """Test that missing conditions default to incremental indexing when last commit exists."""
        config = {
            "autoindexer_name": "test-autoindexer", 
            "repository": "https://github.com/test/repo.git",
            "branch": "main",
            "lastIndexedCommit": "abc123"
            # No conditions provided
        }
        
        handler = GitDataSourceHandler(
            index_name="test-index",
            config=config,
            rag_client=mock_rag_client,
            autoindexer_client=mock_autoindexer_client
        )
        
        with patch.object(handler, '_setup_repository') as mock_setup, \
             patch.object(handler, '_index_all_files') as mock_index_all, \
             patch.object(handler, '_index_diff_files') as mock_index_diff, \
             patch('tempfile.mkdtemp', return_value='/tmp/test'), \
             patch('os.path.exists', return_value=True), \
             patch('shutil.rmtree'):
            
            errors = handler.update_index()
            
            # Should call incremental indexing
            mock_index_diff.assert_called_once()
            mock_index_all.assert_not_called()

    def test_git_handler_no_last_commit_forces_full_index(self, conditions_success_only, mock_rag_client, mock_autoindexer_client):
        """Test that missing lastIndexedCommit forces full indexing regardless of conditions."""
        config = {
            "autoindexer_name": "test-autoindexer",
            "repository": "https://github.com/test/repo.git", 
            "branch": "main",
            "conditions": conditions_success_only
            # No lastIndexedCommit
        }
        
        handler = GitDataSourceHandler(
            index_name="test-index",
            config=config,
            rag_client=mock_rag_client,
            autoindexer_client=mock_autoindexer_client
        )
        
        with patch.object(handler, '_setup_repository') as mock_setup, \
             patch.object(handler, '_index_all_files') as mock_index_all, \
             patch.object(handler, '_index_diff_files') as mock_index_diff, \
             patch('tempfile.mkdtemp', return_value='/tmp/test'), \
             patch('os.path.exists', return_value=True), \
             patch('shutil.rmtree'):
            
            errors = handler.update_index()
            
            # Should call full indexing
            mock_index_all.assert_called_once()
            mock_index_diff.assert_not_called()

    @patch.dict('os.environ', {
        'AUTOINDEXER_NAME': 'test-autoindexer',
        'NAMESPACE': 'test-namespace'
    })
    @patch('autoindexer.main.AutoIndexerK8sClient')
    @patch('autoindexer.main.KAITORAGClient')
    @patch('autoindexer.main.GitDataSourceHandler')
    def test_main_applies_conditions_to_git_config(self, mock_git_handler_class, mock_rag_client_class, mock_k8s_client_class, mock_rag_client, mock_autoindexer_client):
        """Test that main.py properly applies conditions from CRD to Git datasource_config."""
        crd_config = {
            "spec": {
                "indexName": "test-index",
                "ragEngine": "test-rag",
                "dataSource": {
                    "type": "Git",
                    "git": {
                        "repository": "https://github.com/test/repo.git",
                        "branch": "main"
                    }
                }
            },
            "status": {
                "lastIndexedCommit": "abc123",
                "conditions": [
                    {
                        "type": "AutoIndexerError",
                        "status": "True",
                        "reason": "IndexingErrors",
                        "message": "Previous indexing had errors"
                    }
                ]
            }
        }
        
        # Setup the mocks
        mock_k8s_client_instance = Mock()
        mock_k8s_client_instance.get_autoindexer.return_value = crd_config
        mock_k8s_client_instance.namespace = "test-namespace"
        mock_k8s_client_class.return_value = mock_k8s_client_instance
        
        mock_rag_client_instance = Mock()
        mock_rag_client_class.return_value = mock_rag_client_instance
        
        mock_git_handler_instance = Mock()
        mock_git_handler_class.return_value = mock_git_handler_instance
        
        # Create the job - should not raise any exceptions
        job = AutoIndexerJob()
        
        # Verify the Git handler was called with correct config including conditions
        mock_git_handler_class.assert_called_once()
        call_args = mock_git_handler_class.call_args
        config = call_args[1]['config']
        
        assert "conditions" in config
        assert len(config["conditions"]) == 1
        assert config["conditions"][0]["type"] == "AutoIndexerError"
        assert config["conditions"][0]["status"] == "True"

    @patch.dict('os.environ', {
        'AUTOINDEXER_NAME': 'test-autoindexer',
        'NAMESPACE': 'test-namespace'
    })
    @patch('autoindexer.main.AutoIndexerK8sClient')
    @patch('autoindexer.main.KAITORAGClient')
    @patch('autoindexer.main.StaticDataSourceHandler')
    def test_main_applies_conditions_to_static_config(self, mock_static_handler_class, mock_rag_client_class, mock_k8s_client_class, mock_rag_client, mock_autoindexer_client):
        """Test that main.py properly applies conditions from CRD to Static datasource_config."""
        crd_config = {
            "spec": {
                "indexName": "test-index",
                "ragEngine": "test-rag",
                "dataSource": {
                    "type": "Static",
                    "static": {
                        "urls": ["https://example.com/test.txt"]
                    }
                }
            },
            "status": {
                "conditions": [
                    {
                        "type": "AutoIndexerSucceeded",
                        "status": "True",
                        "reason": "IndexingCompleted", 
                        "message": "Previous indexing completed successfully"
                    }
                ]
            }
        }
        
        # Setup the mocks
        mock_k8s_client_instance = Mock()
        mock_k8s_client_instance.get_autoindexer.return_value = crd_config
        mock_k8s_client_instance.namespace = "test-namespace"
        mock_k8s_client_class.return_value = mock_k8s_client_instance
        
        mock_rag_client_instance = Mock()
        mock_rag_client_class.return_value = mock_rag_client_instance
        
        mock_static_handler_instance = Mock()
        mock_static_handler_class.return_value = mock_static_handler_instance
        
        # Create the job - should not raise any exceptions
        job = AutoIndexerJob()
        
        # Verify the Static handler was called with correct config including conditions
        mock_static_handler_class.assert_called_once()
        call_args = mock_static_handler_class.call_args
        config = call_args[1]['config']
        
        assert "conditions" in config
        assert len(config["conditions"]) == 1
        assert config["conditions"][0]["type"] == "AutoIndexerSucceeded"
        assert config["conditions"][0]["status"] == "True"

    @patch.dict('os.environ', {
        'AUTOINDEXER_NAME': 'test-autoindexer',
        'NAMESPACE': 'test-namespace'
    })
    @patch('autoindexer.main.AutoIndexerK8sClient')
    @patch('autoindexer.main.KAITORAGClient')
    @patch('autoindexer.data_source_handler.kusto_handler.KustoDataSourceHandler')
    def test_main_applies_conditions_to_database_config(self, mock_kusto_handler_class, mock_rag_client_class, mock_k8s_client_class, mock_rag_client, mock_autoindexer_client):
        """Test that main.py properly applies conditions from CRD to Database datasource_config."""
        crd_config = {
            "spec": {
                "indexName": "test-index",
                "ragEngine": "test-rag",
                "dataSource": {
                    "type": "Database",
                    "database": {
                        "language": "kql",
                        "initialQuery": "SELECT * FROM table"
                    }
                }
            },
            "status": {
                "conditions": [
                    {
                        "type": "AutoIndexerError",
                        "status": "False",
                        "reason": "IndexingCompleted",
                        "message": "No errors in previous indexing"
                    }
                ]
            }
        }
        
        # Setup the mocks
        mock_k8s_client_instance = Mock()
        mock_k8s_client_instance.get_autoindexer.return_value = crd_config
        mock_k8s_client_instance.namespace = "test-namespace"
        mock_k8s_client_class.return_value = mock_k8s_client_instance
        
        mock_rag_client_instance = Mock()
        mock_rag_client_class.return_value = mock_rag_client_instance
        
        mock_kusto_handler_instance = Mock()
        mock_kusto_handler_class.return_value = mock_kusto_handler_instance
        
        # Create the job - should not raise any exceptions
        job = AutoIndexerJob()
        
        # Verify the Kusto handler was called with correct config including conditions
        mock_kusto_handler_class.assert_called_once()
        call_args = mock_kusto_handler_class.call_args
        config = call_args[1]['config']
        
        assert "conditions" in config
        assert len(config["conditions"]) == 1
        assert config["conditions"][0]["type"] == "AutoIndexerError"
        assert config["conditions"][0]["status"] == "False"

    def test_base_handler_preserves_existing_conditions(self, mock_rag_client, mock_autoindexer_client):
        """Test that base handler preserves existing conditions when creating status updates."""
        existing_conditions = [
            {
                "type": "AutoIndexerScheduled",
                "status": "True", 
                "reason": "JobScheduled",
                "message": "Indexing job was scheduled",
                "lastTransitionTime": "2024-01-01T00:00:00Z"
            },
            {
                "type": "ResourceReady",
                "status": "True",
                "reason": "ResourcesAvailable", 
                "message": "All required resources are ready"
            }
        ]
        
        mock_autoindexer_client.get_autoindexer.return_value = {
            "status": {
                "conditions": existing_conditions
            },
            "metadata": {"generation": 1}
        }
        
        # Setup proper _create_condition mock that returns different conditions based on type
        def create_condition_side_effect(condition_type, status, reason, message, **kwargs):
            return {
                "type": condition_type,
                "status": status,
                "reason": reason,
                "message": message,
                "lastTransitionTime": "2024-01-01T00:00:00Z",
                "observedGeneration": 1
            }
        mock_autoindexer_client._create_condition.side_effect = create_condition_side_effect
        
        config = {
            "autoindexer_name": "test-autoindexer",
            "repository": "https://github.com/test/repo.git"
        }
        
        handler = GitDataSourceHandler(
            index_name="test-index", 
            config=config,
            rag_client=mock_rag_client,
            autoindexer_client=mock_autoindexer_client
        )
        
        # Create a status update
        status_update = handler._create_base_autoindexer_status_update(
            index_name="test-index",
            autoindexer_name="test-autoindexer", 
            rag_client=mock_rag_client,
            autoindexer_client=mock_autoindexer_client,
            errors=[], 
            indexing_duration_seconds=30
        )
        
        # Check that existing conditions are preserved and new ones are added
        condition_types = [condition["type"] for condition in status_update["conditions"]]
        assert "AutoIndexerScheduled" in condition_types
        assert "ResourceReady" in condition_types
        assert "AutoIndexerSucceeded" in condition_types
        assert "AutoIndexerError" in condition_types

    def test_base_handler_updates_existing_condition_type(self, mock_rag_client, mock_autoindexer_client):
        """Test that base handler updates existing condition when same type already exists."""
        existing_conditions = [
            {
                "type": "AutoIndexerError",
                "status": "True",
                "reason": "PreviousError",
                "message": "Previous indexing failed",
                "lastTransitionTime": "2024-01-01T00:00:00Z"
            }
        ]
        
        mock_autoindexer_client.get_autoindexer.return_value = {
            "status": {
                "conditions": existing_conditions
            },
            "metadata": {"generation": 1}
        }
        
        # Setup proper _create_condition mock that returns different conditions based on type
        def create_condition_side_effect(condition_type, status, reason, message, **kwargs):
            return {
                "type": condition_type,
                "status": status,
                "reason": reason,
                "message": message,
                "lastTransitionTime": "2024-01-01T00:00:00Z",
                "observedGeneration": 1
            }
        mock_autoindexer_client._create_condition.side_effect = create_condition_side_effect
        
        config = {
            "autoindexer_name": "test-autoindexer",
            "repository": "https://github.com/test/repo.git"
        }
        
        handler = GitDataSourceHandler(
            index_name="test-index",
            config=config,
            rag_client=mock_rag_client,
            autoindexer_client=mock_autoindexer_client
        )
        
        # Create status update - should update the existing AutoIndexerError condition
        status_update = handler._create_base_autoindexer_status_update(
            index_name="test-index",
            autoindexer_name="test-autoindexer",
            rag_client=mock_rag_client,
            autoindexer_client=mock_autoindexer_client,
            errors=[],  # No errors this time
            indexing_duration_seconds=30
        )
        
        # Find the AutoIndexerError condition - should be updated to False
        error_condition = None
        for condition in status_update["conditions"]:
            if condition["type"] == "AutoIndexerError":
                error_condition = condition
                break
                
        assert error_condition is not None
        assert error_condition["status"] == "False"
        assert error_condition["reason"] == "IndexingCompleted"
        assert error_condition["message"] == "No errors during indexing"

    def test_conditions_empty_list_handling(self, mock_rag_client, mock_autoindexer_client):
        """Test that handlers properly handle empty conditions list."""
        config = {
            "autoindexer_name": "test-autoindexer",
            "repository": "https://github.com/test/repo.git",
            "lastIndexedCommit": "abc123",
            "conditions": []  # Empty conditions list
        }
        
        handler = GitDataSourceHandler(
            index_name="test-index",
            config=config,
            rag_client=mock_rag_client,
            autoindexer_client=mock_autoindexer_client
        )
        
        # Empty conditions should not indicate previous errors
        assert handler.conditions == []
        
        with patch.object(handler, '_setup_repository') as mock_setup, \
             patch.object(handler, '_index_all_files') as mock_index_all, \
             patch.object(handler, '_index_diff_files') as mock_index_diff, \
             patch('tempfile.mkdtemp', return_value='/tmp/test'), \
             patch('os.path.exists', return_value=True), \
             patch('shutil.rmtree'):
            
            errors = handler.update_index()
            
            # Should do incremental indexing since no error conditions exist
            mock_index_diff.assert_called_once()
            mock_index_all.assert_not_called()