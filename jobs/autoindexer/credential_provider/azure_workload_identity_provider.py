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
import os

from azure.identity import WorkloadIdentityCredential
from azure.core.exceptions import ClientAuthenticationError

from jobs.autoindexer.credential_provider.credential_provider import CredentialProvider

logger = logging.getLogger(__name__)


class AzureWorkloadIdentityProvider(CredentialProvider):
    """Credential provider that retrieves tokens using Azure Workload Identity."""

    def __init__(self, client_id: str = None, tenant_id: str = None, token_file_path: str = None):
        self.client_id = client_id or os.getenv("AZURE_CLIENT_ID")
        self.tenant_id = tenant_id or os.getenv("AZURE_TENANT_ID")
        self.token_file_path = token_file_path or os.getenv("AZURE_FEDERATED_TOKEN_FILE")
        self.credential = None

    def get_token(self, scopes: str = None) -> str:
        """
        Retrieve a token for authentication using Azure Workload Identity.

        Args:
            scopes: The OAuth 2.0 scopes to request the token for. If not provided, defaults to the instance's scopes.

        Returns:
            str: A token string for authentication
        """
        try:
            logger.info("Retrieving Azure AD token using Workload Identity")
            
            # Initialize credential if not already done
            if self.credential is None:
                self.credential = WorkloadIdentityCredential(
                    client_id=self.client_id,
                    tenant_id=self.tenant_id,
                    token_file_path=self.token_file_path,
                )
            
            # Get the access token
            token = self.credential.get_token(scopes=scopes)
            
            logger.info("Successfully retrieved Azure AD token")
            return token.token
            
        except ClientAuthenticationError as e:
            logger.error(f"Authentication failed: {e}")
            raise Exception(f"Failed to authenticate with Azure: {e}")
        except Exception as e:
            logger.error(f"Unexpected error retrieving token: {e}")
            raise Exception(f"Failed to retrieve Azure AD token: {e}")