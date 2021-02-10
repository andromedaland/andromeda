// Copyright 2020-2021 William Perron. All rights reserved. MIT License.
package constellation

import (
	"fmt"
	"log"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/dynamodb"
	"github.com/aws/aws-sdk-go/service/dynamodb/dynamodbattribute"
	"github.com/prometheus/client_golang/prometheus"
)

var svc *dynamodb.DynamoDB

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
	sess := session.Must(session.NewSession())
	svc = dynamodb.New(sess, &aws.Config{Credentials: sess.Config.Credentials, Region: aws.String("us-east-1")})

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
	_, err := svc.PutItem(&dynamodb.PutItemInput{
		Item: map[string]*dynamodb.AttributeValue{
			"specifier": {
				S: aws.String(item.Specifier),
			},
			"uid": {
				S: aws.String(item.Uid),
			},
		},
		ReturnConsumedCapacity: aws.String("TOTAL"),
		ConditionExpression:    aws.String("attribute_not_exists(specifier)"),
		TableName:              aws.String(table),
	})

	if err != nil {
		if aerr, ok := err.(awserr.Error); ok {
			switch aerr.Code() {
			case dynamodb.ErrCodeConditionalCheckFailedException:
				putConditionFailedCounter.Add(1)
				log.Printf("%s already exists, nothing to do.", item.Specifier)
				ddbLatency.Observe(time.Since(start).Seconds())
				return nil
			default:
				fmt.Println(aerr.Error())
			}
		} else {
			fmt.Println(err.Error())
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
	out, err := svc.GetItem(&dynamodb.GetItemInput{
		TableName: aws.String(table),
		Key: map[string]*dynamodb.AttributeValue{
			"specifier": {
				S: aws.String(specifier),
			},
		},
		ConsistentRead: aws.Bool(true),
	})

	if err != nil {
		ddbLatency.Observe(time.Since(start).Seconds())
		return Item{}, err
	}

	var item Item
	if err := dynamodbattribute.UnmarshalMap(out.Item, &item); err != nil {
		ddbLatency.Observe(time.Since(start).Seconds())
		return Item{}, err
	}

	ddbLatency.Observe(time.Since(start).Seconds())
	return item, nil
}
