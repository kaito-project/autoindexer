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

from autoindexer.main import AutoIndexerJob


class TestMainConditionsIntegration:
    """Integration tests for conditions handling in main.py AutoIndexerJob."""

    @pytest.fixture
    def mock_k8s_client(self):
        """Mock Kubernetes client."""
        client = Mock()
        client.namespace = "test-namespace"
        return client

    @pytest.fixture  
    def mock_rag_client(self):
        """Mock RAG client."""
        client = Mock()
        return client

    @patch.dict('os.environ', {
        'AUTOINDEXER_NAME': 'test-autoindexer',
        'NAMESPACE': 'test-namespace'
    })
    @patch('autoindexer.main.AutoIndexerK8sClient')
    @patch('autoindexer.main.KAITORAGClient')
    @patch('autoindexer.main.GitDataSourceHandler')
    def test_crd_conditions_applied_to_git_config(self, mock_git_handler, mock_rag_client_class, mock_k8s_client_class):
        """Test conditions from CRD are properly applied to Git datasource config."""
        # Setup mock CRD response with conditions
        mock_k8s_client = Mock()
        mock_k8s_client.namespace = "test-namespace"
        mock_k8s_client.get_autoindexer.return_value = {
            "spec": {
                "indexName": "test-index",
                "ragEngine": "test-rag",
                "dataSource": {
                    "type": "Git", 
                    "git": {
                        "repository": "https://github.com/test/repo.git",
                        "branch": "main",
                        "paths": ["/src"]
                    }
                },
                "credentials": {
                    "type": "SecretRef"
                }
            },
            "status": {
                "lastIndexedCommit": "abc123def",
                "conditions": [
                    {
                        "type": "AutoIndexerError",
                        "status": "True",
                        "reason": "IndexingErrors", 
                        "message": "Previous indexing failed with network timeout",
                        "lastTransitionTime": "2024-01-01T10:00:00Z",
                        "observedGeneration": 2
                    },
                    {
                        "type": "AutoIndexerSucceeded", 
                        "status": "False",
                        "reason": "IndexingFailed",
                        "message": "Indexing did not complete successfully",
                        "lastTransitionTime": "2024-01-01T10:00:00Z", 
                        "observedGeneration": 2
                    }
                ]
            }
        }
        mock_k8s_client_class.return_value = mock_k8s_client
        
        # Setup RAG client mock
        mock_rag_client = Mock()
        mock_rag_client_class.return_value = mock_rag_client
        
        # Setup Git handler mock 
        mock_handler_instance = Mock()
        mock_git_handler.return_value = mock_handler_instance
        
        # Initialize AutoIndexerJob
        job = AutoIndexerJob()
        
        # Verify the Git handler was called with correct config including conditions
        mock_git_handler.assert_called_once()
        call_args = mock_git_handler.call_args
        
        # Check that config contains conditions
        config = call_args[1]['config']  # keyword arguments
        assert 'conditions' in config
        assert len(config['conditions']) == 2
        
        # Verify first condition (AutoIndexerError)
        error_condition = config['conditions'][0]
        assert error_condition['type'] == 'AutoIndexerError'
        assert error_condition['status'] == 'True'
        assert error_condition['reason'] == 'IndexingErrors'
        assert 'network timeout' in error_condition['message']
        
        # Verify second condition (AutoIndexerSucceeded) 
        success_condition = config['conditions'][1]
        assert success_condition['type'] == 'AutoIndexerSucceeded'
        assert success_condition['status'] == 'False'
        assert success_condition['reason'] == 'IndexingFailed'
        
        # Verify other expected config values were also passed through
        assert config['repository'] == 'https://github.com/test/repo.git'
        assert config['branch'] == 'main'
        assert config['paths'] == ['/src']
        assert config['lastIndexedCommit'] == 'abc123def'

    @patch.dict('os.environ', {
        'AUTOINDEXER_NAME': 'test-autoindexer',
        'NAMESPACE': 'test-namespace'
    })
    @patch('autoindexer.main.AutoIndexerK8sClient')
    @patch('autoindexer.main.KAITORAGClient')
    @patch('autoindexer.main.StaticDataSourceHandler')
    def test_crd_conditions_applied_to_static_config(self, mock_static_handler, mock_rag_client_class, mock_k8s_client_class):
        """Test conditions from CRD are properly applied to Static datasource config."""
        # Setup mock CRD response with conditions
        mock_k8s_client = Mock()
        mock_k8s_client.namespace = "test-namespace" 
        mock_k8s_client.get_autoindexer.return_value = {
            "spec": {
                "indexName": "test-index",
                "ragEngine": "test-rag", 
                "dataSource": {
                    "type": "Static",
                    "static": {
                        "urls": [
                            "https://raw.githubusercontent.com/test/repo/main/README.md",
                            "https://docs.example.com/api-guide.pdf"
                        ]
                    }
                }
            },
            "status": {
                "conditions": [
                    {
                        "type": "AutoIndexerSucceeded",
                        "status": "True",
                        "reason": "IndexingCompleted",
                        "message": "All documents indexed successfully", 
                        "lastTransitionTime": "2024-01-01T12:00:00Z",
                        "observedGeneration": 1
                    },
                    {
                        "type": "AutoIndexerError",
                        "status": "False", 
                        "reason": "IndexingCompleted",
                        "message": "No errors encountered during indexing",
                        "lastTransitionTime": "2024-01-01T12:00:00Z",
                        "observedGeneration": 1
                    }
                ] 
            }
        }
        mock_k8s_client_class.return_value = mock_k8s_client
        
        # Setup RAG client mock
        mock_rag_client = Mock()
        mock_rag_client_class.return_value = mock_rag_client
        
        # Setup Static handler mock
        mock_handler_instance = Mock()
        mock_static_handler.return_value = mock_handler_instance
        
        # Initialize AutoIndexerJob
        job = AutoIndexerJob()
        
        # Verify the Static handler was called with correct config including conditions
        mock_static_handler.assert_called_once()
        call_args = mock_static_handler.call_args
        
        # Check that config contains conditions
        config = call_args[1]['config']  # keyword arguments
        assert 'conditions' in config
        assert len(config['conditions']) == 2
        
        # Verify conditions are preserved correctly
        success_condition = next(c for c in config['conditions'] if c['type'] == 'AutoIndexerSucceeded')
        assert success_condition['status'] == 'True' 
        assert success_condition['reason'] == 'IndexingCompleted'
        assert 'successfully' in success_condition['message']
        
        error_condition = next(c for c in config['conditions'] if c['type'] == 'AutoIndexerError')
        assert error_condition['status'] == 'False'
        assert 'No errors' in error_condition['message']
        
        # Verify other config values
        assert config['urls'] == [
            "https://raw.githubusercontent.com/test/repo/main/README.md",
            "https://docs.example.com/api-guide.pdf"
        ]

    @patch.dict('os.environ', {
        'AUTOINDEXER_NAME': 'test-autoindexer', 
        'NAMESPACE': 'test-namespace'
    })
    @patch('autoindexer.main.AutoIndexerK8sClient')
    @patch('autoindexer.main.KAITORAGClient') 
    def test_crd_missing_conditions_handled_gracefully(self, mock_rag_client_class, mock_k8s_client_class):
        """Test that missing conditions in CRD are handled gracefully."""
        # Setup mock CRD response with no conditions
        mock_k8s_client = Mock()
        mock_k8s_client.namespace = "test-namespace"
        mock_k8s_client.get_autoindexer.return_value = {
            "spec": {
                "indexName": "test-index",
                "ragEngine": "test-rag",
                "dataSource": {
                    "type": "Git",
                    "git": {
                        "repository": "https://github.com/test/repo.git"
                    }
                }
            },
            "status": {
                # No conditions field
            }
        }
        mock_k8s_client_class.return_value = mock_k8s_client
        
        # Setup RAG client mock
        mock_rag_client = Mock()
        mock_rag_client_class.return_value = mock_rag_client
        
        with patch('autoindexer.main.GitDataSourceHandler') as mock_git_handler:
            mock_handler_instance = Mock()
            mock_git_handler.return_value = mock_handler_instance
            
            # Should not raise any exceptions
            job = AutoIndexerJob()
            
            # Verify handler was still called with empty conditions
            call_args = mock_git_handler.call_args
            config = call_args[1]['config']
            assert 'conditions' in config
            assert config['conditions'] == []  # Should default to empty list

    @patch.dict('os.environ', {
        'AUTOINDEXER_NAME': 'test-autoindexer',
        'NAMESPACE': 'test-namespace'
    })
    @patch('autoindexer.main.AutoIndexerK8sClient')
    @patch('autoindexer.main.KAITORAGClient')
    def test_crd_empty_status_handled_gracefully(self, mock_rag_client_class, mock_k8s_client_class):
        """Test that CRD with empty status is handled gracefully."""
        # Setup mock CRD response with empty status
        mock_k8s_client = Mock()
        mock_k8s_client.namespace = "test-namespace" 
        mock_k8s_client.get_autoindexer.return_value = {
            "spec": {
                "indexName": "test-index",
                "ragEngine": "test-rag",
                "dataSource": {
                    "type": "Static",
                    "static": {
                        "urls": ["https://example.com/test.txt"]
                    }
                }
            }
            # No status field at all
        }
        mock_k8s_client_class.return_value = mock_k8s_client
        
        # Setup RAG client mock
        mock_rag_client = Mock()
        mock_rag_client_class.return_value = mock_rag_client
        
        with patch('autoindexer.main.StaticDataSourceHandler') as mock_static_handler:
            mock_handler_instance = Mock()
            mock_static_handler.return_value = mock_handler_instance
            
            # Should not raise any exceptions
            job = AutoIndexerJob()
            
            # Verify handler was still called with empty conditions
            call_args = mock_static_handler.call_args
            config = call_args[1]['config'] 
            assert 'conditions' in config
            assert config['conditions'] == []

    @patch.dict('os.environ', {
        'AUTOINDEXER_NAME': 'test-autoindexer',
        'NAMESPACE': 'production-namespace'
    })
    @patch('autoindexer.main.AutoIndexerK8sClient')
    @patch('autoindexer.main.KAITORAGClient')
    @patch('autoindexer.main.NAMESPACE', 'production-namespace')
    @patch('autoindexer.main.AUTOINDEXER_NAME', 'test-autoindexer')
    def test_autoindexer_name_includes_namespace_in_config(self, mock_rag_client_class, mock_k8s_client_class):
        """Test that autoindexer_name in config includes namespace for uniqueness."""
        mock_k8s_client = Mock()
        mock_k8s_client.namespace = "production-namespace"
        mock_k8s_client.get_autoindexer.return_value = {
            "spec": {
                "indexName": "docs-index",
                "ragEngine": "production-rag",
                "dataSource": {
                    "type": "Git", 
                    "git": {
                        "repository": "https://github.com/company/docs.git"
                    }
                }
            },
            "status": {
                "conditions": []
            }
        }
        mock_k8s_client_class.return_value = mock_k8s_client
        
        mock_rag_client = Mock()
        mock_rag_client_class.return_value = mock_rag_client
        
        with patch('autoindexer.main.GitDataSourceHandler') as mock_git_handler:
            mock_handler_instance = Mock()
            mock_git_handler.return_value = mock_handler_instance
            
            job = AutoIndexerJob()
            
            # Verify autoindexer_name includes namespace prefix from environment variable
            call_args = mock_git_handler.call_args
            config = call_args[1]['config']
            assert config['autoindexer_name'] == 'production-namespace_test-autoindexer'