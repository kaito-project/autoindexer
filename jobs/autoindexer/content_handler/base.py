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

from abc import ABC, abstractmethod
from typing import Any


class ContentHandlingError(Exception):
    """Exception raised when content handling fails."""
    pass


class BaseContentHandler(ABC):
    """
    Abstract base class for content handlers.
    
    Content handlers are responsible for extracting text content from various
    file types and data formats.
    """
    
    def __init__(self, config: dict[str, Any] | None = None):
        """
        Initialize the content handler.
        
        Args:
            config: Optional configuration dictionary
        """
        self.config = config or {}
    
    @abstractmethod
    def can_handle(self, raw_content: bytes, content_type: str) -> bool:
        """
        Determine if this handler can process the given content.
        
        Args:
            raw_content: Raw bytes content
            content_type: HTTP content type header
            
        Returns:
            bool: True if this handler can process the content
        """
        pass
    
    @abstractmethod
    def extract_text(self, raw_content: bytes, content_type: str) -> str:
        """
        Extract text content from the raw bytes.
        
        Args:
            raw_content: Raw bytes content
            content_type: HTTP content type header
            
        Returns:
            str: Extracted text content
            
        Raises:
            ContentHandlingError: If extraction fails
        """
        pass
    
    def get_priority(self) -> int:
        """
        Get the priority of this handler. Higher values are tried first.
        
        Returns:
            int: Priority value (default: 0)
        """
        return 0