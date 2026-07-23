package main

import (
	"testing"

	"github.com/specgate/doc-registry/internal/knowledge"
	"github.com/specgate/doc-registry/internal/storage/s3"
)

func TestKnowledgeObjectStoreFor(t *testing.T) {
	t.Parallel()

	if _, ok := knowledgeObjectStoreFor("local", nil).(knowledge.NullObjectStore); !ok {
		t.Fatal("local storage must use knowledge.NullObjectStore")
	}

	s3Client := &s3.Client{}
	if _, ok := knowledgeObjectStoreFor("local", s3Client).(knowledge.NullObjectStore); !ok {
		t.Fatal("local storage must ignore an optional S3 client")
	}
	if got := knowledgeObjectStoreFor("s3", s3Client); got != s3Client {
		t.Fatal("s3 storage must use configured S3 client")
	}
}
