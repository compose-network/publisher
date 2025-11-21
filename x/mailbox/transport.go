package mailbox

import (
	"context"
	"fmt"

	rollupv1 "github.com/compose-network/publisher/proto/rollup/v1"
	spconsensus "github.com/compose-network/publisher/x/consensus"
	"github.com/compose-network/publisher/x/transport"
)

type transportSender struct {
	clients map[string]transport.Client
}

func newTransportSender(clients map[string]transport.Client) MessageSender {
	clientCopy := make(map[string]transport.Client, len(clients))
	for k, v := range clients {
		clientCopy[k] = v
	}

	return &transportSender{
		clients: clientCopy,
	}
}

func (t *transportSender) Send(ctx context.Context, destChainID uint64, msg *rollupv1.Message) error {
	destChainKey := spconsensus.ChainKeyUint64(destChainID)
	client := t.clients[destChainKey]
	if client == nil {
		return fmt.Errorf("no client for destination chain %s", destChainKey)
	}

	return client.Send(ctx, msg)
}
