// Copyright 2020-2021 William Perron. All rights reserved. MIT License.
package constellation

import (
	"context"
	"log"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/prometheus/client_golang/prometheus"
)

var svc *dynamodb.Client

const (
	table = "andromeda-test-4"
)

type Item struct {
	Specifier string `json:"specifier"`
	Uid       string `json:"uid,omitempty"`
}

var putItemCounter prometheus.Counter
var putConditionFailedCounter prometheus.Counter
var getItemCounter prometheus.Counter
var ddbLatency prometheus.Histogram

func init() {
	cfg, err := config.LoadDefaultConfig(context.TODO(), config.WithRegion("us-east-1"))
	if err != nil {
		log.Fatal(err)
	}
	svc = dynamodb.NewFromConfig(cfg)

	putItemCounter = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "dynamodb_put_item_total",
			Help: "A counter for transactions in DGraph",
		},
	)

	putConditionFailedCounter = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "dynamodb_put_item_condition_failed_total",
			Help: "A counter for transactions in DGraph",
		},
	)

	getItemCounter = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "dynamodb_get_item_total",
			Help: "A counter for transactions in DGraph",
		},
	)

	ddbLatency = prometheus.NewHistogram(
		prometheus.HistogramOpts{
			Name: "dynamodb_latency",
			Help: "A histogram of transaction latencies",
		},
	)

	prometheus.MustRegister(putItemCounter, putConditionFailedCounter, getItemCounter, ddbLatency)
}

func PutEntry(item Item) error {
	start := time.Now()
	putItemCounter.Add(1)
	_, err := svc.PutItem(context.TODO(), &dynamodb.PutItemInput{
		Item: map[string]types.AttributeValue{
			"specifier": &types.AttributeValueMemberS{
				Value: item.Specifier,
			},
			"uid": &types.AttributeValueMemberS{
				Value: item.Uid,
			},
		},
		ReturnConsumedCapacity: "TOTAL",
		ConditionExpression:    aws.String("attribute_not_exists(specifier)"),
		TableName:              aws.String(table),
	})

	if err != nil {
		if _, ok := err.(*types.ConditionalCheckFailedException); ok {
			putConditionFailedCounter.Inc()
			log.Printf("%s already exists, nothing to do.", item.Specifier)
			ddbLatency.Observe(time.Since(start).Seconds())
			return nil
		}
		ddbLatency.Observe(time.Since(start).Seconds())
		return err
	}
	ddbLatency.Observe(time.Since(start).Seconds())
	return nil
}

func GetEntry(specifier string) (Item, error) {
	start := time.Now()
	getItemCounter.Add(1)
	out, err := svc.GetItem(context.TODO(), &dynamodb.GetItemInput{
		TableName: aws.String(table),
		Key: map[string]types.AttributeValue{
			"specifier": &types.AttributeValueMemberS{
				Value: specifier,
			},
		},
		ConsistentRead: aws.Bool(true),
	})

	if err != nil {
		ddbLatency.Observe(time.Since(start).Seconds())
		return Item{}, err
	}

	var item Item
	if err := attributevalue.UnmarshalMap(out.Item, &item); err != nil {
		ddbLatency.Observe(time.Since(start).Seconds())
		return Item{}, err
	}

	ddbLatency.Observe(time.Since(start).Seconds())
	return item, nil
}
