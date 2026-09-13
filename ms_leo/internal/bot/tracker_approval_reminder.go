package bot

import (
	"context"
	"time"
)

const trackerApprovalReminderTick = 5 * time.Minute

func (b *Bot) startTrackerApprovalReminderScheduler(ctx context.Context) {
	if b == nil {
		return
	}
	if b.logger != nil {
		b.logger.Infof("tracker approval reminder scheduler: started, tick=%s", trackerApprovalReminderTick)
	}
	ticker := time.NewTicker(trackerApprovalReminderTick)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			if b.logger != nil {
				b.logger.Info("tracker approval reminder scheduler: stopped")
			}
			return
		case <-ticker.C:
			b.runTrackerApprovalReminderSweep()
		}
	}
}

func (b *Bot) runTrackerApprovalReminderSweep() {
	if b == nil || b.db == nil {
		return
	}
	tasks, err := b.db.ListTrackerTasksAwaitingApprovalReminder()
	if err != nil {
		if b.logger != nil {
			b.logger.Errorf("tracker approval reminder sweep: list: %v", err)
		}
		return
	}
	sent := 0
	now := time.Now()
	for _, t := range tasks {
		if !trackerApprovalReminderDue(t, now) {
			continue
		}
		b.sendTrackerApprovalReminder(t)
		sent++
	}
	if sent > 0 && b.logger != nil {
		b.logger.Infof("tracker approval reminder sweep: sent=%d (of %d candidates)", sent, len(tasks))
	}
}
