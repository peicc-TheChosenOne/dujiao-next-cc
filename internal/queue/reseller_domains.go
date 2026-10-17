package queue

import (
	"errors"
	"github.com/hibiken/asynq"
	"time"
)

const TaskResellerDomainProvisioning = "reseller:domain:provisioning"

func NewResellerDomainProvisioningTask() *asynq.Task {
	return asynq.NewTask(TaskResellerDomainProvisioning, nil)
}

func (c *Client) EnqueueResellerDomainProvisioning() error {
	if !c.Enabled() {
		return nil
	}
	_, err := c.client.Enqueue(NewResellerDomainProvisioningTask(), asynq.Queue(c.defaultQueue), asynq.Unique(45*time.Second), asynq.Timeout(5*time.Minute), asynq.MaxRetry(0))
	if errors.Is(err, asynq.ErrDuplicateTask) {
		return nil
	}
	return err
}
