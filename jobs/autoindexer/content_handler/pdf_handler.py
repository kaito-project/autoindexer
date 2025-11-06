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

import io
import logging
from typing import Any

import PyPDF2

from .base import BaseContentHandler, ContentHandlingError

logger = logging.getLogger(__name__)


class PDFContentHandler(BaseContentHandler):
    """
    Handler for PDF content extraction.
    
    This handler supports:
    - Automatic text extraction from PDF files using PyPDF2 and pdfplumber
    - Table extraction from PDFs (when using pdfplumber)
    - Multi-page document support with page markers
    - Fallback between extraction methods for better compatibility
    - Configurable file size limits for PDF processing
    """
    
    def __init__(self, config: dict[str, Any] | None = None):
        """
        Initialize the PDF content handler.
        
        Args:
            config: Optional configuration dictionary
        """
        super().__init__(config)
        
    def can_handle(self, raw_content: bytes, content_type: str) -> bool:
        """
        Determine if this handler can process PDF content.
        
        Args:
            raw_content: Raw bytes content
            content_type: HTTP content type header
            
        Returns:
            bool: True if the content appears to be a PDF file
        """
        # Check content type
        if 'application/pdf' in content_type.lower():
            return True
        
        # Check PDF magic bytes (PDF files start with %PDF)
        if raw_content.startswith(b'%PDF'):
            return True

        try:
            pdf_stream = io.BytesIO(raw_content)
            pdf_reader = PyPDF2.PdfReader(pdf_stream)

            return len(pdf_reader.pages) > 0
        except Exception as e:
            logger.warning(f"Failed to read PDF content: {e}")
        return False

    def extract_text(self, raw_content: bytes, content_type: str) -> str:
        """
        Extract text content from PDF bytes.
        
        Args:
            raw_content: Raw PDF bytes
            content_type: HTTP content type header
            
        Returns:
            str: Extracted text content from the PDF
            
        Raises:
            ContentHandlingError: If extraction fails
        """
        extracted_text = ""
        
        # Try PyPDF2 first (lighter weight)
        try:
            pdf_stream = io.BytesIO(raw_content)
            pdf_reader = PyPDF2.PdfReader(pdf_stream)
            
            logger.info(f"PDF has {len(pdf_reader.pages)} pages")
            
            text_parts = []
            for page_num, page in enumerate(pdf_reader.pages, 1):
                try:
                    page_text = page.extract_text()
                    if page_text.strip():
                        text_parts.append(f"--- Page {page_num} ---\n{page_text.strip()}")
                        logger.debug(f"Extracted {len(page_text)} characters from page {page_num}")
                except Exception as e:
                    logger.warning(f"Failed to extract text from page {page_num}: {e}")
                    continue
            
            if text_parts:
                extracted_text = "\n\n".join(text_parts)
                logger.info(f"Successfully extracted {len(extracted_text)} characters from PDF using PyPDF2")
            else:
                logger.warning("No text extracted from PDF using PyPDF2, trying alternative method")
                
        except ImportError:
            logger.debug("PyPDF2 not available")
        except Exception as e:
            logger.warning(f"PyPDF2 extraction failed: {e}, trying alternative method")
        
        # If PyPDF2 didn't work or extracted no text, try pdfplumber
        if not extracted_text.strip():
            try:
                import pdfplumber
                
                pdf_stream = io.BytesIO(raw_content)
                
                with pdfplumber.open(pdf_stream) as pdf:
                    logger.info(f"PDF has {len(pdf.pages)} pages (pdfplumber)")
                    
                    text_parts = []
                    for page_num, page in enumerate(pdf.pages, 1):
                        try:
                            page_text = page.extract_text()
                            if page_text and page_text.strip():
                                text_parts.append(f"--- Page {page_num} ---\n{page_text.strip()}")
                                logger.debug(f"Extracted {len(page_text)} characters from page {page_num} using pdfplumber")
                            
                            # Also try to extract tables if available
                            tables = page.extract_tables()
                            if tables:
                                for table_num, table in enumerate(tables, 1):
                                    try:
                                        # Convert table to text format
                                        table_text = self._table_to_text(table)
                                        if table_text.strip():
                                            text_parts.append(f"--- Page {page_num} Table {table_num} ---\n{table_text.strip()}")
                                    except Exception as e:
                                        logger.debug(f"Failed to extract table {table_num} from page {page_num}: {e}")
                                        
                        except Exception as e:
                            logger.warning(f"Failed to extract text from page {page_num} using pdfplumber: {e}")
                            continue
                    
                    if text_parts:
                        extracted_text = "\n\n".join(text_parts)
                        logger.info(f"Successfully extracted {len(extracted_text)} characters from PDF using pdfplumber")
                    
            except ImportError:
                logger.debug("pdfplumber not available")
            except Exception as e:
                logger.error(f"pdfplumber extraction failed: {e}")
        
        # Final validation
        if not extracted_text.strip():
            logger.error("Failed to extract any text from PDF")
            raise ContentHandlingError("Unable to extract text from PDF")
        
        logger.info(f"Successfully extracted {len(extracted_text)} characters from PDF")
        return extracted_text

    def _table_to_text(self, table: list) -> str:
        """
        Convert a table (list of lists) to readable text format.
        
        Args:
            table: Table data as list of lists
            
        Returns:
            str: Formatted table as text
        """
        if not table:
            return ""
        
        try:
            # Filter out empty rows
            rows = [row for row in table if row and any(cell for cell in row if cell)]
            
            if not rows:
                return ""
            
            # Convert to strings and handle None values
            text_rows = []
            for row in rows:
                text_row = []
                for cell in row:
                    if cell is None:
                        text_row.append("")
                    else:
                        text_row.append(str(cell).strip())
                text_rows.append(text_row)
            
            # Create simple text table
            if text_rows:
                # Use pipe separators for simple table format
                table_lines = []
                for row in text_rows:
                    table_lines.append(" | ".join(row))
                return "\n".join(table_lines)
            
        except Exception as e:
            logger.debug(f"Error formatting table: {e}")
        
        return ""
    
    def get_priority(self) -> int:
        """
        Get the priority of this handler. PDFs have high priority due to specific detection.
        
        Returns:
            int: Priority value (10 for PDF handler)
        """
        return 10