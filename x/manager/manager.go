package manager

import (
	"context"

	pb "github.com/compose-network/specs/compose/proto"
)

type PublisherManager struct {
}

func New() *PublisherManager {
	return &PublisherManager{}
}

func (m *PublisherManager) Start(ctx context.Context) error {
	return nil
}

func (m *PublisherManager) Stop(ctx context.Context) error {
	return nil
}

func (m *PublisherManager) HandleMessage(ctx context.Context, from string, msg *pb.Message) error {
	return nil
}

func (m *PublisherManager) QueueStats(ctx context.Context) (int, error) {
	return 0, nil
}
