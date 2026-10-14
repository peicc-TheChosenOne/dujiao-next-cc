package consumer

import (
	"context"
	"github.com/hibiken/asynq"
)

func (c *Consumer) handleResellerDomainProvisioning(ctx context.Context, _ *asynq.Task) error {
	if c == nil || c.ResellerDomainProvisioning == nil {
		return nil
	}
	return c.ResellerDomainProvisioning.SyncDueDomains(ctx)
}
