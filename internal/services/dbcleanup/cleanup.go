package dbcleanup

import (
	"context"
	"log"
	"snmpflapd/internal/repository"
	"time"
)

func RunDBCleanUp(ctx context.Context, repo repository.Connector, period time.Duration) {
	for {
		select {
		case <-ctx.Done():
			log.Println("closed due context")
			return
		case <-time.After(period):
			if err := repo.CleanUp(ctx); err != nil {
				log.Println(err)
			}
		}
	}
}
