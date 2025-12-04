package main

import (
	"context"
	"log"

	"github.com/kaito-project/autoindexer/test/e2e/suite"
)

func main() {
	ctx := context.Background()

	if err := suite.RunE2ETestSuite(ctx); err != nil {
		log.Fatalf("failed to run autoindexer suite: %v", err)
	}

	log.Println("AutoIndexer suite completed successfully")
}
