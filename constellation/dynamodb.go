// Copyright 2020-2021 William Perron. All rights reserved. MIT License.
package constellation

import (
	"fmt"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/dynamodb"
	"github.com/aws/aws-sdk-go/service/dynamodb/dynamodbattribute"
)

var svc *dynamodb.DynamoDB

const (
	table = "andromeda-test-1"
)

func init() {
	sess := session.Must(session.NewSession())
	svc = dynamodb.New(sess, &aws.Config{Credentials: sess.Config.Credentials})
}

type Entry struct {
	Specifier string `json:"specifier"`
	Module    string `json:"module"`
	Uid       string `json:"uid,omitempty"`
}

func PutEntry(entry Entry) error {
	_, err := svc.PutItem(&dynamodb.PutItemInput{
		Item: map[string]*dynamodb.AttributeValue{
			"specifier": {
				S: aws.String(entry.Specifier),
			},
			"Module": {
				S: aws.String(entry.Module),
			},
			"Uid": {
				S: aws.String(entry.Uid),
			},
		},
		ReturnConsumedCapacity: aws.String("TOTAL"),
		TableName:              aws.String(table),
	})

	if err != nil {
		if aerr, ok := err.(awserr.Error); ok {
			switch aerr.Code() {
			case dynamodb.ErrCodeConditionalCheckFailedException:
				fmt.Println(dynamodb.ErrCodeConditionalCheckFailedException, aerr.Error())
			case dynamodb.ErrCodeProvisionedThroughputExceededException:
				fmt.Println(dynamodb.ErrCodeProvisionedThroughputExceededException, aerr.Error())
			case dynamodb.ErrCodeResourceNotFoundException:
				fmt.Println(dynamodb.ErrCodeResourceNotFoundException, aerr.Error())
			case dynamodb.ErrCodeItemCollectionSizeLimitExceededException:
				fmt.Println(dynamodb.ErrCodeItemCollectionSizeLimitExceededException, aerr.Error())
			case dynamodb.ErrCodeTransactionConflictException:
				fmt.Println(dynamodb.ErrCodeTransactionConflictException, aerr.Error())
			case dynamodb.ErrCodeRequestLimitExceeded:
				fmt.Println(dynamodb.ErrCodeRequestLimitExceeded, aerr.Error())
			case dynamodb.ErrCodeInternalServerError:
				fmt.Println(dynamodb.ErrCodeInternalServerError, aerr.Error())
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

func GetSpecifierUid(specifier string) ([]Entry, error) {
	out, err := svc.Query(&dynamodb.QueryInput{
		TableName:              aws.String(table),
		IndexName:              aws.String("specifier-uid-index"),
		KeyConditionExpression: aws.String("specifier = :specifier"),
		ExpressionAttributeValues: map[string]*dynamodb.AttributeValue{
			":specifier": {
				S: aws.String(specifier),
			},
		},
		Select: aws.String("ALL_PROJECTED_ATTRIBUTES"),
	})

	if err != nil {
		return []Entry{}, nil
	}
	var entries []Entry
	if err := dynamodbattribute.UnmarshalListOfMaps(out.Items, &entries); err != nil {
		return []Entry{}, nil
	}

	return entries, nil
}

func GetDependencies(deps []string) ([]Entry, error) {
	var keys []map[string]*dynamodb.AttributeValue
	for _, dep := range deps {
		keys = append(keys, map[string]*dynamodb.AttributeValue{
			"specifier": {
				S: aws.String(dep),
			},
		})
	}

	out, err := svc.BatchGetItem(&dynamodb.BatchGetItemInput{
		RequestItems: map[string]*dynamodb.KeysAndAttributes{
			table: {
				Keys: keys,
			},
		},
	})

	if err != nil {
		return []Entry{}, err
	}

	items := make([]map[string]*dynamodb.AttributeValue, len(out.Responses))
	for _, resp := range out.Responses {
		for _, item := range resp {
			items = append(items, item)
		}
	}

	var entries []Entry
	if err := dynamodbattribute.UnmarshalListOfMaps(items, &entries); err != nil {
		return []Entry{}, err
	}
	return entries, nil
}
