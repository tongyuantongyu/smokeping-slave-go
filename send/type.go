package send

import (
	"smokeping-slave-go/master"
	"time"
)

type Sender func(c *master.ProbeConfig, t *master.Target) []time.Duration
