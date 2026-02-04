package mailbox

import (
	"bytes"
	"fmt"
	"sync"

	"github.com/compose-network/specs/compose/proto"
)

// Queue stores and retrieves mailbox messages for coordination.
type Queue interface {
	Record(msg *proto.MailboxMessage) error
	Consume(instanceID []byte, sourceChainID uint64) (*proto.MailboxMessage, error)
	ConsumeMatching(instanceID []byte, sourceChainID uint64, label string) (*proto.MailboxMessage, error)
	GetAll(instanceID []byte) []*proto.MailboxMessage
	Clear(instanceID []byte)
}

// MemoryQueue is an in-memory implementation of Queue.
type MemoryQueue struct {
	mu       sync.Mutex
	messages map[string][]*proto.MailboxMessage
}

// NewMemoryQueue creates a new in-memory mailbox queue.
func NewMemoryQueue() *MemoryQueue {
	return &MemoryQueue{
		messages: make(map[string][]*proto.MailboxMessage),
	}
}

func (q *MemoryQueue) Record(msg *proto.MailboxMessage) error {
	q.mu.Lock()
	defer q.mu.Unlock()

	key := fmt.Sprintf("%x", msg.InstanceId)
	q.messages[key] = append(q.messages[key], msg)
	return nil
}

func (q *MemoryQueue) Consume(instanceID []byte, sourceChainID uint64) (*proto.MailboxMessage, error) {
	q.mu.Lock()
	defer q.mu.Unlock()

	key := fmt.Sprintf("%x", instanceID)
	msgs := q.messages[key]

	for i, msg := range msgs {
		if msg.SourceChain == sourceChainID {
			q.messages[key] = append(msgs[:i], msgs[i+1:]...)
			return msg, nil
		}
	}

	return nil, fmt.Errorf("no mailbox message from chain %d for instance %x", sourceChainID, instanceID)
}

func (q *MemoryQueue) ConsumeMatching(
	instanceID []byte,
	sourceChainID uint64,
	label string,
) (*proto.MailboxMessage, error) {
	q.mu.Lock()
	defer q.mu.Unlock()

	key := fmt.Sprintf("%x", instanceID)
	msgs := q.messages[key]

	for i, msg := range msgs {
		if msg.SourceChain == sourceChainID && msg.Label == label {
			q.messages[key] = append(msgs[:i], msgs[i+1:]...)
			return msg, nil
		}
	}

	return nil, fmt.Errorf("no matching mailbox message from chain %d with label %s", sourceChainID, label)
}

func (q *MemoryQueue) GetAll(instanceID []byte) []*proto.MailboxMessage {
	q.mu.Lock()
	defer q.mu.Unlock()

	key := fmt.Sprintf("%x", instanceID)
	msgs := q.messages[key]
	result := make([]*proto.MailboxMessage, len(msgs))
	copy(result, msgs)
	return result
}

func (q *MemoryQueue) Clear(instanceID []byte) {
	q.mu.Lock()
	defer q.mu.Unlock()

	key := fmt.Sprintf("%x", instanceID)
	delete(q.messages, key)
}

// MatchesHeader checks if a mailbox message matches a dependency header.
func MatchesHeader(msg *proto.MailboxMessage, sourceChain uint64, sender, receiver []byte, label string) bool {
	if msg.SourceChain != sourceChain {
		return false
	}
	if label != "" && msg.Label != label {
		return false
	}
	if len(sender) > 0 && !bytes.Equal(msg.Source, sender) {
		return false
	}
	if len(receiver) > 0 && !bytes.Equal(msg.Receiver, receiver) {
		return false
	}
	return true
}
