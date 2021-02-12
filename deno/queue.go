// Copyright 2020-2021 William Perron. All rights reserved. MIT License.
package deno

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strconv"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/aws/aws-sdk-go-v2/service/sqs/types"
)

// Queue interface for putting and getting messages. The interface doesn make
// any guarantees about message ordering, this concern must be managed by the
// interface implementation.
type Queue interface {
	Put(Module) error
	Get() (Module, error)
	isOpened() bool
}

// Enqueue serves as a passthrough for channels of deno.Module. It puts the
// incoming messages in a Queue and consumes it to send messages down the output
// channel as well. It serves as an intermediary steps where an implementation
// of a Queue that uses a persistent back end like SQS or Kafka can be used.
// This is necessary to be able to start and stop the crawler arbitrarily and
// pick up where it left off
func Enqueue(mods chan Module, q Queue) (chan Module, chan error) {
	out := make(chan Module)
	e := make(chan error)
	go func() {
		for m := range mods {
			if err := q.Put(m); err != nil {
				e <- err
			}
		}
		close(out)
	}()

	go func() {
		for q.isOpened() {
			m, err := q.Get()
			if err != nil {
				e <- err
			}
			out <- m
		}
	}()
	return out, e
}

// ChanQueue is an in-memory queue that uses channels under the hood. If the
// channel is unbuffered, Put and Get are blocking operations
type ChanQueue struct {
	mods   chan Module
	closed bool
}

// NewChanQueue returns a new ChanQueue instance
func NewChanQueue(buf int) ChanQueue {
	return ChanQueue{
		mods: make(chan Module, buf),
	}
}

// Put sends a message to the underlying channel
func (q *ChanQueue) Put(m Module) error {
	q.mods <- m
	return nil
}

// Get gets the next message from the underlying channel
func (q *ChanQueue) Get() (Module, error) {
	m, ok := <-q.mods
	if !ok {
		q.closed = true
	}
	return m, nil
}

func (q *ChanQueue) isOpened() bool {
	return !q.closed
}

// SQSQueue is a simple abstraction over the standard sqs.Client struct that
// implements the Queue interface
type SQSQueue struct {
	queue    *sqs.Client
	queueURL *string
	buf      chan Module
	closed   bool
}

// NewSQSQueue instantiates a new SQS Client with the given config
func NewSQSQueue(c aws.Config, url string, buf int) *SQSQueue {
	client := sqs.NewFromConfig(c)
	q := &SQSQueue{
		queue:    client,
		queueURL: aws.String(url),
		buf:      make(chan Module),
	}

	// start polling the queue asynchronously
	go func() {
		for {
			out, err := client.ReceiveMessage(context.TODO(), &sqs.ReceiveMessageInput{
				QueueUrl:          q.queueURL,
				VisibilityTimeout: 10800, // 3 hours (60 * 60 * 3)
			})

			if err != nil {
				log.Printf("error consuming SQS: %s\n", err)
				continue
			}

			for _, m := range out.Messages {
				var mod Module
				err := json.Unmarshal([]byte(*m.Body), &mod)
				if err != nil {
					log.Printf("error unmarshalling message from SQS: %s\n", err)
				}
				q.buf <- mod
			}
		}
	}()

	return q
}

// Put sends a message to SQS and returns any error encountered by the aws client
func (s *SQSQueue) Put(m Module) error {
	bs, err := json.Marshal(m)
	if err != nil {
		return err
	}

	_, err = s.queue.SendMessage(context.TODO(), &sqs.SendMessageInput{
		QueueUrl:    s.queueURL,
		MessageBody: aws.String(string(bs)),
	})
	return err
}

// Get returns a single message either from the internal buffer queue or from
// the SQS queue
func (s *SQSQueue) Get() (Module, error) {
	return <-s.buf, nil
}

// Approx returns the approximate total number of messages in the queue, visible,
// delayed or not visible.
func (s *SQSQueue) Approx() (int, error) {
	out, err := s.queue.GetQueueAttributes(context.TODO(), &sqs.GetQueueAttributesInput{
		QueueUrl: s.queueURL,
		AttributeNames: []types.QueueAttributeName{
			"ApproximateNumberOfMessages",
			"ApproximateNumberOfMessagesDelayed",
			"ApproximateNumberOfMessagesNotVisible",
		},
	})

	if err != nil {
		return -1, fmt.Errorf("failed to get queue attributes: %s", err)
	}

	total := 0
	for _, v := range out.Attributes {
		i, err := strconv.Atoi(v)
		if err != nil {
			log.Printf("couldn't convert value '%s' to an int\n", v)
		}

		total += i
	}
	return total, nil
}

func (s *SQSQueue) isOpened() bool {
	return !s.closed
}
