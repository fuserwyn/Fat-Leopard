package database

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/lib/pq"
)

// Челленджи: участник берёт N дней подряд с тренировкой (миграция 88).

const (
	ChallengeStatusActive    = "active"
	ChallengeStatusCompleted = "completed"
	ChallengeStatusFailed    = "failed"
)

// ErrChallengeAlreadyActive — у участника уже есть активный челлендж.
var ErrChallengeAlreadyActive = errors.New("challenge already active")

// ErrChallengeCodeTaken — код ссылки уже занят (при создании своего челленджа).
var ErrChallengeCodeTaken = errors.New("challenge code taken")

// Challenge — челлендж. AuthorUserID = 0 у стандартных.
type Challenge struct {
	ID           int64
	Code         string
	Title        string
	LengthDays   int
	AuthorUserID int64
	CreatedAt    time.Time
}

// ChallengeParticipant — участие в челлендже. Даты — YYYY-MM-DD в локальном TZ участника;
// LastCountedDate пустая, пока не засчитан ни один день.
type ChallengeParticipant struct {
	ID              int64
	ChallengeID     int64
	UserID          int64
	PackChatID      int64
	StartDate       string
	Status          string
	DaysDone        int
	LastCountedDate string
	FinishedAt      *time.Time
	CreatedAt       time.Time
	Challenge       Challenge
}

const challengeColumns = `c.id, c.code, c.title, c.length_days, COALESCE(c.author_user_id, 0), c.created_at`

func scanChallenge(sc interface{ Scan(...any) error }) (Challenge, error) {
	var c Challenge
	err := sc.Scan(&c.ID, &c.Code, &c.Title, &c.LengthDays, &c.AuthorUserID, &c.CreatedAt)
	return c, err
}

func (d *Database) queryChallenges(q string, args ...any) ([]Challenge, error) {
	rows, err := d.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Challenge
	for rows.Next() {
		c, err := scanChallenge(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// ListStandardChallenges — стандартные челленджи по возрастанию длины.
func (d *Database) ListStandardChallenges() ([]Challenge, error) {
	return d.queryChallenges(`SELECT ` + challengeColumns + ` FROM challenges c
		WHERE c.author_user_id IS NULL ORDER BY c.length_days, c.id`)
}

// ListChallengesByAuthor — свои челленджи участника, новые первыми.
func (d *Database) ListChallengesByAuthor(authorUserID int64) ([]Challenge, error) {
	return d.queryChallenges(`SELECT `+challengeColumns+` FROM challenges c
		WHERE c.author_user_id = $1 ORDER BY c.created_at DESC, c.id DESC LIMIT 50`, authorUserID)
}

// GetChallengeByCode — челлендж по коду из ссылки; nil, если нет.
func (d *Database) GetChallengeByCode(code string) (*Challenge, error) {
	code = strings.TrimSpace(code)
	if code == "" {
		return nil, nil
	}
	c, err := scanChallenge(d.db.QueryRow(`SELECT `+challengeColumns+` FROM challenges c WHERE c.code = $1`, code))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

// CreateChallenge заводит свой челлендж участника.
func (d *Database) CreateChallenge(code, title string, lengthDays int, authorUserID int64) (*Challenge, error) {
	var author any
	if authorUserID != 0 {
		author = authorUserID
	}
	c, err := scanChallenge(d.db.QueryRow(`
		INSERT INTO challenges AS c (code, title, length_days, author_user_id)
		VALUES ($1, $2, $3, $4)
		RETURNING `+challengeColumns, code, title, lengthDays, author))
	if err != nil {
		if isUniqueViolation(err) {
			return nil, ErrChallengeCodeTaken
		}
		return nil, fmt.Errorf("create challenge: %w", err)
	}
	return &c, nil
}

const participantColumns = `p.id, p.challenge_id, p.user_id, p.pack_chat_id, p.start_date::text, p.status,
	p.days_done, COALESCE(p.last_counted_date::text, ''), p.finished_at, p.created_at, ` + challengeColumns

func scanParticipant(sc interface{ Scan(...any) error }) (ChallengeParticipant, error) {
	var p ChallengeParticipant
	var finished sql.NullTime
	err := sc.Scan(&p.ID, &p.ChallengeID, &p.UserID, &p.PackChatID, &p.StartDate, &p.Status,
		&p.DaysDone, &p.LastCountedDate, &finished, &p.CreatedAt,
		&p.Challenge.ID, &p.Challenge.Code, &p.Challenge.Title, &p.Challenge.LengthDays, &p.Challenge.AuthorUserID, &p.Challenge.CreatedAt)
	if finished.Valid {
		t := finished.Time
		p.FinishedAt = &t
	}
	return p, err
}

const participantFrom = ` FROM challenge_participants p JOIN challenges c ON c.id = p.challenge_id `

// GetActiveChallengeParticipant — активный челлендж участника; nil, если нет.
func (d *Database) GetActiveChallengeParticipant(userID int64) (*ChallengeParticipant, error) {
	p, err := scanParticipant(d.db.QueryRow(`SELECT `+participantColumns+participantFrom+`
		WHERE p.user_id = $1 AND p.status = 'active'`, userID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// ListChallengeHistory — последние участия (любой статус), новые первыми.
func (d *Database) ListChallengeHistory(userID int64, limit int) ([]ChallengeParticipant, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	rows, err := d.db.Query(`SELECT `+participantColumns+participantFrom+`
		WHERE p.user_id = $1 ORDER BY p.created_at DESC, p.id DESC LIMIT $2`, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ChallengeParticipant
	for rows.Next() {
		p, err := scanParticipant(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// ListActiveChallengeParticipants — все активные участия (для ежечасной проверки пропусков).
func (d *Database) ListActiveChallengeParticipants() ([]ChallengeParticipant, error) {
	rows, err := d.db.Query(`SELECT ` + participantColumns + participantFrom + `
		WHERE p.status = 'active' ORDER BY p.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ChallengeParticipant
	for rows.Next() {
		p, err := scanParticipant(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// StartChallenge — участник принимает челлендж. daysDone/lastCountedDate — день,
// засчитанный сразу (тренировка уже была сегодня); пустая дата — ничего не засчитано.
func (d *Database) StartChallenge(challengeID, userID, packChatID int64, startDate string, daysDone int, lastCountedDate string) (*ChallengeParticipant, error) {
	var id int64
	err := d.db.QueryRow(`
		INSERT INTO challenge_participants (challenge_id, user_id, pack_chat_id, start_date, days_done, last_counted_date)
		VALUES ($1, $2, $3, $4::date, $5, NULLIF($6, '')::date)
		RETURNING id`, challengeID, userID, packChatID, startDate, daysDone, strings.TrimSpace(lastCountedDate)).Scan(&id)
	if err != nil {
		if isUniqueViolation(err) {
			return nil, ErrChallengeAlreadyActive
		}
		return nil, fmt.Errorf("start challenge: %w", err)
	}
	p, err := scanParticipant(d.db.QueryRow(`SELECT `+participantColumns+participantFrom+` WHERE p.id = $1`, id))
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// UpdateChallengeProgress пишет прогресс активного участия. Статус completed/failed
// закрывает его и ставит дату завершения. false — участие уже не активно.
func (d *Database) UpdateChallengeProgress(participantID int64, daysDone int, lastCountedDate, status string) (bool, error) {
	res, err := d.db.Exec(`
		UPDATE challenge_participants
		SET days_done = $2,
		    last_counted_date = NULLIF($3, '')::date,
		    status = $4,
		    finished_at = CASE WHEN $4 = 'active' THEN NULL ELSE NOW() END
		WHERE id = $1 AND status = 'active'`, participantID, daysDone, strings.TrimSpace(lastCountedDate), status)
	if err != nil {
		return false, fmt.Errorf("update challenge progress: %w", err)
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// HasCompletedChallengeOfLength — прошёл ли участник челлендж длиной не меньше minDays.
func (d *Database) HasCompletedChallengeOfLength(userID int64, minDays int) (bool, error) {
	var ok bool
	err := d.db.QueryRow(`
		SELECT EXISTS (
			SELECT 1`+participantFrom+`
			WHERE p.user_id = $1 AND p.status = 'completed' AND c.length_days >= $2
		)`, userID, minDays).Scan(&ok)
	return ok, err
}

// SetChallengeInvite запоминает челлендж, по ссылке на который пришёл пользователь.
func (d *Database) SetChallengeInvite(userID, challengeID int64) error {
	_, err := d.db.Exec(`
		INSERT INTO challenge_invites (user_id, challenge_id) VALUES ($1, $2)
		ON CONFLICT (user_id) DO UPDATE SET challenge_id = EXCLUDED.challenge_id, created_at = NOW()`,
		userID, challengeID)
	if err != nil {
		return fmt.Errorf("set challenge invite: %w", err)
	}
	return nil
}

// GetChallengeInvite — челлендж из ссылки, ещё не принятый; nil, если нет.
func (d *Database) GetChallengeInvite(userID int64) (*Challenge, error) {
	c, err := scanChallenge(d.db.QueryRow(`SELECT `+challengeColumns+`
		FROM challenge_invites i JOIN challenges c ON c.id = i.challenge_id
		WHERE i.user_id = $1`, userID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

// ClearChallengeInvite забывает приглашение (принято или отклонено).
func (d *Database) ClearChallengeInvite(userID int64) error {
	_, err := d.db.Exec(`DELETE FROM challenge_invites WHERE user_id = $1`, userID)
	return err
}

func isUniqueViolation(err error) bool {
	var pqErr *pq.Error
	return errors.As(err, &pqErr) && pqErr.Code == "23505"
}
