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

import contextlib
import json
import logging
from typing import Any

from .base import BaseContentHandler, ContentHandlingError

logger = logging.getLogger(__name__)


class TextContentHandler(BaseContentHandler):
    """
    Handler for text-based content extraction.
    
    This handler supports:
    - Various text encodings (UTF-8, Latin1, etc.) with automatic detection
    - JSON content with intelligent text extraction
    - Plain text files (.txt, .md, .rst, etc.)
    - Configuration files (.json, .yaml, .toml, etc.)
    - Source code files (.py, .go, .js, etc.)
    - HTML and XML content
    - Fallback encoding handling with error recovery
    """
    
    def __init__(self, config: dict[str, Any] | None = None):
        """
        Initialize the text content handler.
        
        Args:
            config: Optional configuration dictionary
        """
        super().__init__(config)
        
    def can_handle(self, raw_content: bytes, content_type: str) -> bool:
        """
        Determine if this handler can process text-based content.
        
        This handler serves as a fallback for any content that's not handled
        by more specific handlers (like PDF).
        
        Args:
            raw_content: Raw bytes content
            content_type: HTTP content type header
            
        Returns:
            bool: True if the content appears to be text-based
        """
        # Check for common text content types
        text_content_types = [
            'text/', 'application/json', 'application/xml', 'application/yaml',
            'application/javascript', 'application/x-yaml', 'application/toml'
        ]
        
        content_type_lower = content_type.lower()
        for text_type in text_content_types:
            if text_type in content_type_lower:
                return True
        
        # Try to detect if content is likely text by checking for common text patterns
        try:
            # Sample first 1KB to check for text characteristics
            sample = raw_content[:1024]
            
            # Check for null bytes (binary indicator)
            if b'\x00' in sample:
                return False
            
            # Try basic UTF-8 decode on sample
            sample.decode('utf-8', errors='strict')
            return True
        except UnicodeDecodeError:
            # Try with chardet if available
            try:
                import chardet
                detected = chardet.detect(sample)
                if detected and detected.get('confidence', 0) > 0.7:
                    return True
            except ImportError:
                pass
        
        # Default to True as this is the fallback handler
        return False
    
    def extract_text(self, raw_content: bytes, content_type: str) -> str:
        """
        Extract text content from raw bytes, handling various encodings.
        
        Args:
            raw_content: Raw bytes content
            content_type: HTTP content type header
            
        Returns:
            str: Decoded text content
            
        Raises:
            ContentHandlingError: If extraction fails
        """
        # If content is empty
        if not raw_content:
            logger.warning("Empty content received")
            return ""
        
        # Try to determine encoding from content-type header
        encoding = None
        if 'charset=' in content_type:
            with contextlib.suppress(Exception):
                encoding = content_type.split('charset=')[1].split(';')[0].strip()
        
        # List of encodings to try in order
        encodings_to_try = []
        
        if encoding:
            encodings_to_try.append(encoding)
        
        # Add common encodings
        encodings_to_try.extend(['utf-8', 'utf-8-sig', 'latin1', 'cp1252', 'iso-8859-1'])
        
        # Try chardet detection if available
        try:
            import chardet
            detected = chardet.detect(raw_content)
            if detected and detected.get('encoding') and detected.get('confidence', 0) > 0.7:
                detected_encoding = detected['encoding']
                if detected_encoding not in encodings_to_try:
                    encodings_to_try.insert(1, detected_encoding)
        except ImportError:
            logger.debug("chardet not available for encoding detection")
        except Exception as e:
            logger.debug(f"Chardet detection failed: {e}")
        
        # Try each encoding
        for enc in encodings_to_try:
            try:
                decoded_content = raw_content.decode(enc)
                logger.debug(f"Successfully decoded content using encoding: {enc}")
                
                # Handle specific content types
                if 'application/json' in content_type:
                    try:
                        json_data = json.loads(decoded_content)
                        # Try to extract text content from JSON
                        if isinstance(json_data, dict):
                            if 'content' in json_data:
                                return str(json_data['content'])
                            elif 'text' in json_data:
                                return str(json_data['text'])
                            elif 'body' in json_data:
                                return str(json_data['body'])
                        # Return formatted JSON as string
                        return json.dumps(json_data, indent=2, ensure_ascii=False)
                    except json.JSONDecodeError:
                        # If JSON parsing fails, return as plain text
                        pass
                
                return decoded_content
                
            except UnicodeDecodeError:
                continue
            except Exception as e:
                logger.debug(f"Failed to decode with {enc}: {e}")
                continue
        
        # If all encodings fail, try with error handling
        try:
            content = raw_content.decode('utf-8', errors='replace')
            logger.warning("Used UTF-8 with error replacement")
            return content
        except Exception as e:
            logger.error(f"Failed to decode content: {e}")
            raise ContentHandlingError(f"Unable to decode content: {e}")
    
    def get_priority(self) -> int:
        """
        Get the priority of this handler. Text handler has low priority as it's a fallback.
        
        Returns:
            int: Priority value (1 for text handler)
        """
        return 1