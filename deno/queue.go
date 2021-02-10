// Copyright 2020-2021 William Perron. All rights reserved. MIT License.
package deno

import (
	"context"
	"encoding/json"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
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
func NewSQSQueue(c *aws.Config, url string, buf int) *SQSQueue {
	client := sqs.New(*c)
	return &SQSQueue{
		queue:    client,
		queueURL: aws.String(url),
		buf:      make(chan Module),
	}
}

// Put sends a message to SQS and returns any error encountered by the aws client
func (s *SQSQueue) Put(m Module) error {
	ctx := context.Background()
	bs, err := json.Marshal(m)
	if err != nil {
		return err
	}

	_, err = s.queue.SendMessageRequest(&sqs.SendMessageInput{
		QueueUrl:    s.queueURL,
		MessageBody: aws.String(string(bs)),
	}).Send(ctx)
	return err
}

// Get returns a single message either from the internal buffer queue or from
// the SQS queue
func (s *SQSQueue) Get() (Module, error) {
	select {
	case m, ok := <-s.buf:
		if !ok {
			s.closed = true
		}
		return m, nil
	default:
		ctx := context.Background()
		s.queue.ReceiveMessageRequest(&sqs.ReceiveMessageInput{
			QueueUrl:          s.queueURL,
			VisibilityTimeout: aws.Int64(10800), // 3 hours (60 * 60 * 3)
		}).Send(ctx)
		return Module{}, nil
	}
}

func (s *SQSQueue) isOpened() bool {
	return !s.closed
}
