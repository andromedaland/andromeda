// Copyright 2020-2021 William Perron. All rights reserved. MIT License.
package deno

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
