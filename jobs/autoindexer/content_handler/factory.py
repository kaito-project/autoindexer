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
from typing import Any

from .base import BaseContentHandler, ContentHandlingError
from .pdf_handler import PDFContentHandler
from .text_handler import TextContentHandler

logger = logging.getLogger(__name__)


class ContentHandlerFactory:
    """
    Factory class for creating and managing content handlers.
    
    This factory automatically selects the appropriate content handler
    based on the content type, URL, and content analysis.
    """
    
    def __init__(self, config: dict[str, Any] | None = None):
        """
        Initialize the content handler factory.
        
        Args:
            config: Optional configuration dictionary
        """
        self.config = config or {}
        self._handlers = []
        self._initialize_handlers()
    
    def _initialize_handlers(self):
        """Initialize all available content handlers."""
        # Register handlers in order of specificity (most specific first)
        handler_classes = [
            PDFContentHandler,
            TextContentHandler,  # Keep text handler last as it's the fallback
        ]
        
        for handler_class in handler_classes:
            try:
                handler = handler_class(self.config)
                self._handlers.append(handler)
                logger.debug(f"Registered content handler: {handler_class.__name__}")
            except Exception as e:
                logger.error(f"Failed to initialize handler {handler_class.__name__}: {e}")
        
        # Sort handlers by priority (higher priority first)
        self._handlers.sort(key=lambda h: h.get_priority(), reverse=True)
        logger.info(f"Initialized {len(self._handlers)} content handlers")
    
    def get_handler(self, raw_content: bytes, content_type: str) -> BaseContentHandler:
        """
        Get the most appropriate content handler for the given content.
        
        Args:
            raw_content: Raw bytes content
            content_type: HTTP content type header
            
        Returns:
            BaseContentHandler: The best handler for this content
            
        Raises:
            ContentHandlingError: If no suitable handler is found
        """
        for handler in self._handlers:
            try:
                if handler.can_handle(raw_content, content_type):
                    logger.debug(f"Selected handler: {handler.__class__.__name__}")
                    return handler
            except Exception as e:
                logger.warning(f"Handler {handler.__class__.__name__} failed can_handle check: {e}")
                continue
        
        # This should not happen since TextContentHandler is a fallback
        raise ContentHandlingError("No suitable content handler found")
    
    def extract_text(self, raw_content: bytes, content_type: str) -> str:
        """
        Extract text content using the appropriate handler.
        
        Args:
            raw_content: Raw bytes content
            content_type: HTTP content type header
            
        Returns:
            str: Extracted text content
            
        Raises:
            ContentHandlingError: If extraction fails
        """
        handler = self.get_handler(raw_content, content_type)
        return handler.extract_text(raw_content, content_type)
    
    def get_available_handlers(self) -> list[str]:
        """
        Get a list of available handler names.
        
        Returns:
            list[str]: List of handler class names
        """
        return [handler.__class__.__name__ for handler in self._handlers]