package database

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
)

type OutboxEvent struct {
	ID           int64
	EventType    string
	AggregateKey string
	Payload      []byte
	Status       string
	Attempts     int
	LastError    sql.NullString
}

func (d *Database) enqueueOutboxEventTx(tx *sql.Tx, eventType, aggregateKey string, payload any) error {
	raw, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal outbox payload: %w", err)
	}
	const q = `
		INSERT INTO outbox_events (event_type, aggregate_key, payload, status, next_attempt_at)
		VALUES ($1, $2, $3::jsonb, 'pending', NOW())
	`
	if _, err := tx.Exec(q, eventType, aggregateKey, string(raw)); err != nil {
		return fmt.Errorf("insert outbox event: %w", err)
	}
	return nil
}

func (d *Database) EnqueueOutboxEvent(eventType, aggregateKey string, payload any) error {
	raw, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal outbox payload: %w", err)
	}
	const q = `
		INSERT INTO outbox_events (event_type, aggregate_key, payload, status, next_attempt_at)
		VALUES ($1, $2, $3::jsonb, 'pending', NOW())
	`
	if _, err := d.db.Exec(q, eventType, aggregateKey, string(raw)); err != nil {
		return fmt.Errorf("insert outbox event: %w", err)
	}
	return nil
}

func (d *Database) ClaimOutboxEvents(limit int) ([]OutboxEvent, error) {
	const q = `
		WITH picked AS (
			SELECT id
			FROM outbox_events
			WHERE status IN ('pending', 'retry')
			  AND next_attempt_at <= NOW()
			ORDER BY id ASC
			FOR UPDATE SKIP LOCKED
			LIMIT $1
		)
		UPDATE outbox_events o
		SET status = 'processing',
		    attempts = attempts + 1,
		    locked_at = NOW(),
		    updated_at = NOW()
		FROM picked
		WHERE o.id = picked.id
		RETURNING o.id, o.event_type, o.aggregate_key, o.payload::text, o.status, o.attempts, o.last_error
	`
	rows, err := d.db.Query(q, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var events []OutboxEvent
	for rows.Next() {
		var e OutboxEvent
		var payloadText string
		if err := rows.Scan(&e.ID, &e.EventType, &e.AggregateKey, &payloadText, &e.Status, &e.Attempts, &e.LastError); err != nil {
			return nil, err
		}
		e.Payload = []byte(payloadText)
		events = append(events, e)
	}
	return events, rows.Err()
}

func (d *Database) MarkOutboxEventDone(id int64) error {
	const q = `
		UPDATE outbox_events
		SET status = 'done',
		    last_error = NULL,
		    updated_at = NOW()
		WHERE id = $1
	`
	_, err := d.db.Exec(q, id)
	return err
}

func (d *Database) MarkOutboxEventRetry(id int64, nextAttemptAt time.Time, errText string) error {
	const q = `
		UPDATE outbox_events
		SET status = 'retry',
		    next_attempt_at = $2,
		    last_error = $3,
		    updated_at = NOW()
		WHERE id = $1
	`
	_, err := d.db.Exec(q, id, nextAttemptAt, errText)
	return err
}

func (d *Database) MarkOutboxEventDead(id int64, errText string) error {
	const q = `
		UPDATE outbox_events
		SET status = 'dead',
		    last_error = $2,
		    updated_at = NOW()
		WHERE id = $1
	`
	_, err := d.db.Exec(q, id, errText)
	return err
}
