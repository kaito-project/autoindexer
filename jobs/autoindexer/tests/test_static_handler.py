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


import json
from unittest.mock import Mock, patch

import pytest
from requests.exceptions import RequestException

from autoindexer.data_source_handler.handler import DataSourceError
from autoindexer.data_source_handler.static_handler import StaticDataSourceHandler
from autoindexer.k8s.k8s_client import AutoIndexerK8sClient
from autoindexer.rag.rag_client import KAITORAGClient


class TestStaticDataSourceHandler:
    """Test class for StaticDataSourceHandler."""

    @pytest.fixture
    def valid_config(self):
        """Fixture providing a valid configuration."""
        return {
            "autoindexer_name": "test-autoindexer",
            "urls": ["https://example.com/document.md"],
            "timeout": 30,
            "max_file_size": 5 * 1024 * 1024  # 5MB
        }

    @pytest.fixture
    def credentials(self):
        """Fixture providing test credentials."""
        return "test-bearer-token"

    @pytest.fixture
    def mock_rag_client(self):
        """Fixture providing a mock RAG client."""
        client = Mock(spec=KAITORAGClient)
        client.index_documents.return_value = {"success": True, "indexed": 1}
        return client

    @pytest.fixture
    def mock_autoindexer_client(self):
        """Fixture providing a mock AutoIndexer K8s client."""
        client = Mock(spec=AutoIndexerK8sClient)
        client.update_indexing_progress.return_value = None
        return client

    def test_init_success(self, valid_config, mock_rag_client, mock_autoindexer_client):
        """Test successful initialization."""
        handler = StaticDataSourceHandler(
            index_name="test-index",
            config=valid_config,
            rag_client=mock_rag_client,
            autoindexer_client=mock_autoindexer_client
        )
        
        assert handler.config == valid_config
        assert handler.credentials is None
        assert handler.autoindexer_name == "test-autoindexer"

    def test_init_with_credentials(self, valid_config, credentials, mock_rag_client, mock_autoindexer_client):
        """Test initialization with credentials."""
        handler = StaticDataSourceHandler(
            index_name="test-index",
            config=valid_config,
            rag_client=mock_rag_client,
            autoindexer_client=mock_autoindexer_client,
            credentials=credentials
        )
        
        assert handler.credentials == credentials

    def test_init_missing_autoindexer_name(self, mock_rag_client, mock_autoindexer_client):
        """Test initialization failure with missing autoindexer_name."""
        config = {
            "static": {
                "urls": ["https://example.com/document.md"]
            }
        }
        
        with pytest.raises(DataSourceError, match="missing 'autoindexer_name' value"):
            StaticDataSourceHandler(
                index_name="test-index",
                config=config,
                rag_client=mock_rag_client,
                autoindexer_client=mock_autoindexer_client
            )

    def test_init_missing_static_section(self, mock_rag_client, mock_autoindexer_client):
        """Test initialization failure with missing autoindexer_name."""
        config = {}
        
        with pytest.raises(DataSourceError, match="missing 'autoindexer_name' value"):
            StaticDataSourceHandler(
                index_name="test-index",
                config=config,
                rag_client=mock_rag_client,
                autoindexer_client=mock_autoindexer_client
            )

    @patch('autoindexer.data_source_handler.static_handler.requests.get')
    def test_update_index_single_url_success(self, mock_get, valid_config, mock_rag_client, mock_autoindexer_client):
        """Test successful index update with a single URL."""
        # Setup mock response
        mock_response = Mock()
        mock_response.raise_for_status.return_value = None
        mock_response.headers = {'content-type': 'text/plain'}
        mock_response.iter_content.return_value = [b'Test content from URL']
        mock_get.return_value.__enter__.return_value = mock_response
        
        handler = StaticDataSourceHandler(
            index_name="test-index",
            config=valid_config,
            rag_client=mock_rag_client,
            autoindexer_client=mock_autoindexer_client
        )
        errors = handler.update_index()
        
        assert errors == []
        mock_rag_client.index_documents.assert_called_once()
        
        call_args = mock_rag_client.index_documents.call_args
        assert call_args[1]["index_name"] == "test-index"
        documents = call_args[1]["documents"]
        assert len(documents) == 1
        assert documents[0].text == "Test content from URL"
        assert documents[0].metadata["source_type"] == "url"
        assert documents[0].metadata["source_url"] == "https://example.com/document.md"
        assert documents[0].metadata["autoindexer"] == "test-autoindexer"

    @patch('autoindexer.data_source_handler.static_handler.requests.get')
    def test_update_index_multiple_urls(self, mock_get, mock_rag_client, mock_autoindexer_client):
        """Test index update with multiple URLs."""
        config = {
            "autoindexer_name": "test-autoindexer",
            "urls": [
                "https://example.com/doc1.md",
                "https://example.com/doc2.txt"
            ]
        }
        
        # Setup mock responses
        mock_response = Mock()
        mock_response.raise_for_status.return_value = None
        mock_response.headers = {'content-type': 'text/plain'}
        mock_response.iter_content.side_effect = [
            [b'Content from doc1'],
            [b'Content from doc2']
        ]
        mock_get.return_value.__enter__.return_value = mock_response
        
        handler = StaticDataSourceHandler(
            index_name="test-index",
            config=config,
            rag_client=mock_rag_client,
            autoindexer_client=mock_autoindexer_client
        )
        errors = handler.update_index()
        
        assert errors == []
        assert mock_rag_client.index_documents.call_count == 1
        
        call_args = mock_rag_client.index_documents.call_args
        documents = call_args[1]["documents"]
        assert len(documents) == 2

    @patch('autoindexer.data_source_handler.static_handler.requests.get')
    def test_update_index_batch_processing(self, mock_get, mock_rag_client, mock_autoindexer_client):
        """Test batch processing when document count exceeds batch size."""
        # Create config with 12 URLs to trigger batch processing (batch size is 10)
        urls = [f"https://example.com/doc{i}.md" for i in range(12)]
        config = {
            "autoindexer_name": "test-autoindexer",
            "urls": urls
        }
        
        mock_response = Mock()
        mock_response.raise_for_status.return_value = None
        mock_response.headers = {'content-type': 'text/plain'}
        mock_response.iter_content.return_value = [b'Test content']
        mock_get.return_value.__enter__.return_value = mock_response
        
        handler = StaticDataSourceHandler("test-index", config, mock_rag_client, mock_autoindexer_client)
        errors = handler.update_index()
        
        assert errors == []
        # Should be called twice: once for first 10, once for remaining 2
        assert mock_rag_client.index_documents.call_count == 2

    @patch('autoindexer.data_source_handler.static_handler.requests.get')
    def test_update_index_http_error(self, mock_get, valid_config, mock_rag_client, mock_autoindexer_client):
        """Test handling of HTTP errors during index update."""
        mock_get.return_value.__enter__.side_effect = RequestException("Network error")
        
        handler = StaticDataSourceHandler("test-index", valid_config, mock_rag_client, mock_autoindexer_client)
        
        # The method catches exceptions and returns them as errors instead of raising
        errors = handler.update_index()
        
        assert len(errors) >= 1  # May return multiple error entries for the same failure
        assert any("Failed to fetch content" in error for error in errors)

    def test_update_index_no_urls(self, mock_rag_client, mock_autoindexer_client):
        """Test handling when no URLs are configured."""
        config = {
            "autoindexer_name": "test-autoindexer"
        }
        
        handler = StaticDataSourceHandler("test-index", config, mock_rag_client, mock_autoindexer_client)
        errors = handler.update_index()
        
        assert len(errors) == 1
        assert "No documents fetched" in errors[0]

    @patch('autoindexer.data_source_handler.static_handler.requests.get')
    def test_fetch_content_from_url_success(self, mock_get, valid_config, mock_rag_client, mock_autoindexer_client):
        """Test successful content fetching from URL."""
        mock_response = Mock()
        mock_response.raise_for_status.return_value = None
        mock_response.headers = {'content-type': 'text/plain; charset=utf-8'}
        mock_response.iter_content.return_value = [b'Test content']
        mock_get.return_value.__enter__.return_value = mock_response
        
        handler = StaticDataSourceHandler("test-index", valid_config, mock_rag_client, mock_autoindexer_client)
        content = handler._fetch_content_from_url("https://example.com/test.txt")
        
        assert content == "Test content"

    @patch('autoindexer.data_source_handler.static_handler.requests.get')
    def test_fetch_content_invalid_url(self, mock_get, valid_config, mock_rag_client, mock_autoindexer_client):
        """Test handling of invalid URLs."""
        handler = StaticDataSourceHandler("test-index", valid_config, mock_rag_client, mock_autoindexer_client)
        
        with pytest.raises(DataSourceError, match="Invalid URL format"):
            handler._fetch_content_from_url("not-a-valid-url")

    @patch('autoindexer.data_source_handler.static_handler.requests.get')
    def test_fetch_content_file_too_large_header(self, mock_get, valid_config, mock_rag_client, mock_autoindexer_client):
        """Test handling of files that are too large based on Content-Length header."""
        mock_response = Mock()
        mock_response.headers = {'content-length': str(20 * 1024 * 1024)}  # 20MB
        mock_get.return_value.__enter__.return_value = mock_response
        
        handler = StaticDataSourceHandler("test-index", valid_config, mock_rag_client, mock_autoindexer_client)
        
        with pytest.raises(DataSourceError, match="File too large"):
            handler._fetch_content_from_url("https://example.com/large-file.txt")

    @patch('autoindexer.data_source_handler.static_handler.requests.get')
    def test_fetch_content_file_too_large_streaming(self, mock_get, valid_config, mock_rag_client, mock_autoindexer_client):
        """Test handling of files that are too large during streaming."""
        mock_response = Mock()
        mock_response.raise_for_status.return_value = None
        mock_response.headers = {'content-type': 'text/plain'}
        # Create a large chunk that exceeds the limit
        large_chunk = b'x' * (6 * 1024 * 1024)  # 6MB chunk
        mock_response.iter_content.return_value = [large_chunk]
        mock_get.return_value.__enter__.return_value = mock_response
        
        handler = StaticDataSourceHandler("test-index", valid_config, mock_rag_client, mock_autoindexer_client)
        
        with pytest.raises(DataSourceError, match="File too large"):
            handler._fetch_content_from_url("https://example.com/large-file.txt")

    @patch('autoindexer.data_source_handler.static_handler.requests.get')
    def test_fetch_content_with_authentication(self, mock_get, valid_config, credentials, mock_rag_client, mock_autoindexer_client):
        """Test content fetching with Bearer token authentication."""
        mock_response = Mock()
        mock_response.raise_for_status.return_value = None
        mock_response.headers = {'content-type': 'text/plain'}
        mock_response.iter_content.return_value = [b'Authenticated content']
        mock_get.return_value.__enter__.return_value = mock_response
        
        handler = StaticDataSourceHandler("test-index", valid_config, mock_rag_client, mock_autoindexer_client, credentials)
        handler._fetch_content_from_url("https://example.com/secure.txt")
        
        # Verify Bearer token authentication was used
        mock_get.assert_called_once()
        call_kwargs = mock_get.call_args[1]
        assert 'Authorization' in call_kwargs['headers']
        assert call_kwargs['headers']['Authorization'] == 'Bearer test-bearer-token'

    @patch('autoindexer.data_source_handler.static_handler.requests.get')
    def test_fetch_content_json_processing(self, mock_get, valid_config, mock_rag_client, mock_autoindexer_client):
        """Test handling of JSON content with special processing."""
        json_data = {"content": "This is the main content", "other": "ignored"}
        mock_response = Mock()
        mock_response.raise_for_status.return_value = None
        mock_response.headers = {'content-type': 'application/json'}
        mock_response.iter_content.return_value = [json.dumps(json_data).encode('utf-8')]
        mock_get.return_value.__enter__.return_value = mock_response
        
        handler = StaticDataSourceHandler("test-index", valid_config, mock_rag_client, mock_autoindexer_client)
        content = handler._fetch_content_from_url("https://example.com/data.json")
        
        assert content == "This is the main content"

    def test_is_pdf_content_content_type(self, valid_config, mock_rag_client, mock_autoindexer_client):
        """Test PDF detection based on content type."""
        from autoindexer.content_handler.pdf_handler import PDFContentHandler
        
        handler = PDFContentHandler()
        
        assert handler.can_handle(b'', 'application/pdf') is True
        assert handler.can_handle(b'', 'text/plain') is False

    def test_is_pdf_content_url_extension(self, valid_config, mock_rag_client, mock_autoindexer_client):
        """Test PDF detection no longer uses URL extension (content-based detection only)."""
        from autoindexer.content_handler.pdf_handler import PDFContentHandler
        
        handler = PDFContentHandler()
        
        # Without content type or magic bytes, extension doesn't matter anymore
        assert handler.can_handle(b'', '') is False
        assert handler.can_handle(b'', '') is False

    def test_is_pdf_content_magic_bytes(self, valid_config, mock_rag_client, mock_autoindexer_client):
        """Test PDF detection based on magic bytes."""
        from autoindexer.content_handler.pdf_handler import PDFContentHandler
        
        handler = PDFContentHandler()
        
        pdf_content = b'%PDF-1.4\n...'
        text_content = b'This is plain text'
        
        assert handler.can_handle(pdf_content, '') is True
        assert handler.can_handle(text_content, '') is False

    def test_extract_pdf_text_pypdf2_success(self, valid_config, mock_rag_client, mock_autoindexer_client):
        """Test successful PDF text extraction using PyPDF2."""
        from autoindexer.content_handler.pdf_handler import PDFContentHandler
        
        with patch('autoindexer.content_handler.pdf_handler.PyPDF2') as mock_pypdf2:
            # Mock PyPDF2 module
            mock_page = Mock()
            mock_page.extract_text.return_value = "Page content"
            
            mock_reader = Mock()
            mock_reader.pages = [mock_page]
            
            mock_pypdf2.PdfReader.return_value = mock_reader
            
            handler = PDFContentHandler()
            result = handler.extract_text(b'%PDF-1.4...', 'application/pdf')
            
            assert "Page content" in result
            assert "--- Page 1 ---" in result

    def test_extract_pdf_text_pdfplumber_fallback(self, valid_config, mock_rag_client, mock_autoindexer_client):
        """Test PDF text extraction fallback to pdfplumber."""
        from autoindexer.content_handler.pdf_handler import PDFContentHandler
        
        with patch('builtins.__import__') as mock_import:
            # Mock pdfplumber module and PyPDF2 import error
            mock_pdfplumber = Mock()
            mock_page = Mock()
            mock_page.extract_text.return_value = "Page content from pdfplumber"
            mock_page.extract_tables.return_value = []
            
            mock_pdf = Mock()
            mock_pdf.pages = [mock_page]
            mock_pdf.__enter__ = Mock(return_value=mock_pdf)
            mock_pdf.__exit__ = Mock(return_value=None)
            
            mock_pdfplumber.open.return_value = mock_pdf
            
            def import_side_effect(name, *args, **kwargs):
                if name == 'PyPDF2':
                    raise ImportError("PyPDF2 not available")
                elif name == 'pdfplumber':
                    return mock_pdfplumber
                return __import__(name, *args, **kwargs)
            
            mock_import.side_effect = import_side_effect
            
            handler = PDFContentHandler()
            result = handler.extract_text(b'%PDF-1.4...', 'application/pdf')
            
            assert "Page content from pdfplumber" in result

    def test_extract_pdf_text_no_libraries(self, valid_config, mock_rag_client, mock_autoindexer_client):
        """Test PDF text extraction failure when no libraries are available."""
        from autoindexer.content_handler.pdf_handler import PDFContentHandler
        from autoindexer.content_handler.base import ContentHandlingError
        
        with patch('builtins.__import__') as mock_import:
            def import_side_effect(name, *args, **kwargs):
                if name in ['PyPDF2', 'pdfplumber']:
                    raise ImportError(f"{name} not available")
                return __import__(name, *args, **kwargs)
            
            mock_import.side_effect = import_side_effect
            
            handler = PDFContentHandler()
            
            with pytest.raises(ContentHandlingError):
                handler.extract_text(b'%PDF-1.4...', 'application/pdf')

    def test_table_to_text_success(self, valid_config, mock_rag_client, mock_autoindexer_client):
        """Test successful table conversion to text using PDF handler."""
        from autoindexer.content_handler.pdf_handler import PDFContentHandler
        
        handler = PDFContentHandler()
        
        table = [
            ['Header1', 'Header2', 'Header3'],
            ['Row1Col1', 'Row1Col2', 'Row1Col3'],
            ['Row2Col1', 'Row2Col2', 'Row2Col3']
        ]
        
        result = handler._table_to_text(table)
        
        assert "Header1 | Header2 | Header3" in result
        assert "Row1Col1 | Row1Col2 | Row1Col3" in result

    def test_table_to_text_empty_table(self, valid_config, mock_rag_client, mock_autoindexer_client):
        """Test table conversion with empty table using PDF handler."""
        from autoindexer.content_handler.pdf_handler import PDFContentHandler
        
        handler = PDFContentHandler()
        
        assert handler._table_to_text([]) == ""

    def test_table_to_text_with_none_values(self, valid_config, mock_rag_client, mock_autoindexer_client):
        """Test table conversion with None values using PDF handler."""
        from autoindexer.content_handler.pdf_handler import PDFContentHandler
        
        handler = PDFContentHandler()
        
        table = [['A', None, 'C'], [None, 'B', None]]
        result = handler._table_to_text(table)
        
        assert "A |  | C" in result
        assert " | B | " in result

    def test_decode_content_encoding_detection(self, valid_config, mock_rag_client, mock_autoindexer_client):
        """Test content decoding with encoding detection using text handler."""
        from autoindexer.content_handler.text_handler import TextContentHandler
        
        with patch('chardet.detect') as mock_detect:
            # Mock chardet to return a specific encoding
            mock_detect.return_value = {'encoding': 'iso-8859-1', 'confidence': 0.8}
            
            handler = TextContentHandler()
            content = "Content with special chars: àéî".encode('iso-8859-1')
            
            result = handler.extract_text(content, 'text/plain')
            
            assert result == "Content with special chars: àéî"
            mock_detect.assert_called_once()

    def test_decode_content_content_type_encoding(self, valid_config, mock_rag_client, mock_autoindexer_client):
        """Test content decoding using encoding from content-type header."""
        from autoindexer.content_handler.text_handler import TextContentHandler
        
        handler = TextContentHandler()
        content = "Test content"
        encoded_content = content.encode('latin1')
        
        result = handler.extract_text(
            encoded_content, 
            'text/plain; charset=latin1'
        )
        
        assert result == content

    def test_decode_content_empty_content(self, valid_config, mock_rag_client, mock_autoindexer_client):
        """Test decoding of empty content."""
        from autoindexer.content_handler.text_handler import TextContentHandler
        
        handler = TextContentHandler()
        
        result = handler.extract_text(b'', 'text/plain')
        
        assert result == ""

    def test_decode_content_fallback_with_errors(self, valid_config, mock_rag_client, mock_autoindexer_client):
        """Test content decoding fallback when all encodings fail."""
        from autoindexer.content_handler.text_handler import TextContentHandler
        
        handler = TextContentHandler()
        # Use bytes that can't be decoded properly
        invalid_content = b'\xff\xfe\x00\x00invalid'
        
        result = handler.extract_text(invalid_content, 'text/plain')
        
        # Should use UTF-8 with error replacement
        assert isinstance(result, str)

    def test_get_current_timestamp(self, valid_config, mock_rag_client, mock_autoindexer_client):
        """Test timestamp generation."""
        handler = StaticDataSourceHandler("test-index", valid_config, mock_rag_client, mock_autoindexer_client)
        
        timestamp = handler._get_current_timestamp()
        
        assert isinstance(timestamp, str)
        assert timestamp.endswith('Z')
        assert 'T' in timestamp  # ISO format should contain 'T'

    @patch('autoindexer.data_source_handler.static_handler.requests.get')
    def test_fetch_content_with_code_language_detection(self, mock_get, mock_rag_client, mock_autoindexer_client):
        """Test language detection for code files during index update."""
        config = {
            "autoindexer_name": "test-autoindexer",
            "urls": ["https://example.com/script.py"]
        }
        
        mock_response = Mock()
        mock_response.raise_for_status.return_value = None
        mock_response.headers = {'content-type': 'text/plain'}
        mock_response.iter_content.return_value = [b'print("Hello, World!")']
        mock_get.return_value.__enter__.return_value = mock_response
        
        handler = StaticDataSourceHandler("test-index", config, mock_rag_client, mock_autoindexer_client)
        errors = handler.update_index()
        
        assert errors == []
        
        call_args = mock_rag_client.index_documents.call_args
        documents = call_args[1]["documents"]
        assert len(documents) == 1
        assert documents[0].metadata["language"] == "python"
        assert documents[0].metadata["split_type"] == "code"