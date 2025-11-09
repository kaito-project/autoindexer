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

from kaito_rag_engine_client import Client
from kaito_rag_engine_client.models import IndexRequest, Document, UpdateDocumentRequest, DeleteDocumentRequest
from kaito_rag_engine_client.api.index import list_indexes, create_index, delete_index, list_documents_in_index, delete_documents_in_index, update_documents_in_index, persist_index, load_index


class KAITORAGClient:
    """
    A client for interacting with the KAITO RAGEngine API.
    This client provides methods to index documents, query the engine,
    update documents, delete documents, list documents, and manage indexes.
    
    This is based on the example RAG client but optimized for the AutoIndexer use case.
    """

    def __init__(self, base_url):
        self.client = Client(base_url)

    def index_documents(self, index_name: str, documents: list[Document]):
        """
        Index documents in the RAGEngine.

        documents: list of Document objects
        """
        return create_index.sync(client=self.client, body=IndexRequest(index_name=index_name, documents=documents))

    def update_documents(self, index_name: str, documents: list[Document]):
        """
        Update a document in the RAGEngine.

        documents: list of Document objects
        """
        return update_documents_in_index.sync(client=self.client, index_name=index_name, body=UpdateDocumentRequest(documents=documents))

    def delete_documents(self, index_name: str, document_ids: list[str]):
        """
        Delete documents from the RAGEngine.

        document_ids: list of document IDs to delete
        """
        return delete_documents_in_index.sync(client=self.client, index_name=index_name, body=DeleteDocumentRequest(doc_ids=document_ids))

    def list_documents(self, index_name: str, metadata_filter: dict, limit: int = 10, offset: int = 0):
        """
        List documents in the RAGEngine.

        metadata_filter: dict to filter documents by metadata
        limit: number of documents to return
        offset: offset for pagination
        """
        return list_documents_in_index.sync(client=self.client, index_name=index_name, metadata_filter=metadata_filter, limit=limit, offset=offset)

    def list_indexes(self):
        """
        List all indexes in the RAGEngine.
        """
        return list_indexes.sync(client=self.client)

    def persist_index(self, index_name: str, path: str = "/tmp"):
        """
        Persist an index in the RAGEngine.

        index_name: name of the index to persist
        path: path to persist the index
        """
        return persist_index.sync(client=self.client, index_name=index_name, path=path)

    def load_index(self, index_name: str, path: str = "/tmp", overwrite: bool = True):
        """
        Load an index in the RAGEngine.

        index_name: name of the index to load
        path: path to load the index from
        overwrite: whether to overwrite the existing index
        """
        return load_index.sync(client=self.client, index_name=index_name, path=path, overwrite=overwrite)

    def delete_index(self, index_name: str):
        """
        Delete an index from the RAGEngine.

        index_name: name of the index to delete
        """
        return delete_index.sync(client=self.client, index_name=index_name)
