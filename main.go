package main

import (
	elastic "github.com/elastic/go-elasticsearch/v7"
	"log"
	"os"
)

var es *elastic.Client

func init() {
	var err error
	es, err = elastic.NewDefaultClient()
	if err != nil {
		log.Fatalf("unable to create ElasticSearch client: %s\n", err)
	}

	log.Printf("elastic client version: %s\n", elastic.Version)

	info, err :=  es.API.Info()
	if err != nil {
		log.Fatalf("unable to get cluster information: %s\n", err)
	}
	log.Printf("elastic cluster info: %v\n", info)
}

func main() {
	log.Println("done.")
	os.Exit(0)
}