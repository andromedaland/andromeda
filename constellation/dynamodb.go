// Copyright 2020-2021 William Perron. All rights reserved. MIT License.
package constellation

import (
	"fmt"
	"log"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/dynamodb"
	"github.com/aws/aws-sdk-go/service/dynamodb/dynamodbattribute"
)

var svc *dynamodb.DynamoDB

const (
	table = "andromeda-test-4"
)

func init() {
	sess := session.Must(session.NewSession())
	svc = dynamodb.New(sess, &aws.Config{Credentials: sess.Config.Credentials, Region: aws.String("us-east-1")})
}

type Item struct {
	Specifier string `json:"specifier"`
	Uid       string `json:"uid,omitempty"`
}

func PutEntry(item Item) error {
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
				log.Printf("%s already exists, nothing to do.", item.Specifier)
				return nil
			default:
				fmt.Println(aerr.Error())
			}
		} else {
			fmt.Println(err.Error())
		}
		return err
	}
	return nil
}

func GetEntry(specifier string) (Item, error) {
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
		return Item{}, err
	}

	var item Item
	if err := dynamodbattribute.UnmarshalMap(out.Item, &item); err != nil {
		return Item{}, err
	}

	return item, nil
}
