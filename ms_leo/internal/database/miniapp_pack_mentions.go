package database

import (
	"fmt"
	"strings"

	"github.com/lib/pq"
)

// LookupActivePackMembersByMentionTokens — сопоставляет @-токены с активными участниками стаи
// по Telegram-нику (training_state.username) или display_name из miniapp_user_profile.
// tokens — без «@», в нижнем регистре.
func (d *Database) LookupActivePackMembersByMentionTokens(packChatID int64, tokens []string) (map[string]int64, error) {
	out := make(map[string]int64)
	if d == nil || packChatID == 0 || len(tokens) == 0 {
		return out, nil
	}
	seenTok := map[string]struct{}{}
	var uniq []string
	for _, t := range tokens {
		t = strings.TrimSpace(strings.ToLower(t))
		if t == "" {
			continue
		}
		if _, ok := seenTok[t]; ok {
			continue
		}
		seenTok[t] = struct{}{}
		uniq = append(uniq, t)
	}
	if len(uniq) == 0 {
		return out, nil
	}

	const q = `
		SELECT ts.user_id,
		       LOWER(BTRIM(REGEXP_REPLACE(COALESCE(ts.username, ''), '^@+', ''))) AS uname_norm,
		       LOWER(BTRIM(COALESCE(p.display_name, ''))) AS display_norm
		FROM training_state ts
		LEFT JOIN miniapp_user_profile p
			ON p.user_id = ts.user_id AND p.pack_chat_id = ts.chat_id
		WHERE ts.chat_id = $1
		  AND ts.is_deleted = FALSE
		  AND (
		        LOWER(BTRIM(REGEXP_REPLACE(COALESCE(ts.username, ''), '^@+', ''))) = ANY($2)
		     OR LOWER(BTRIM(COALESCE(p.display_name, ''))) = ANY($2)
		  )
	`
	rows, err := d.db.Query(q, packChatID, pq.Array(uniq))
	if err != nil {
		return nil, fmt.Errorf("lookup pack mention tokens: %w", err)
	}
	defer rows.Close()

	want := map[string]struct{}{}
	for _, t := range uniq {
		want[t] = struct{}{}
	}
	for rows.Next() {
		var userID int64
		var unameNorm, displayNorm string
		if err := rows.Scan(&userID, &unameNorm, &displayNorm); err != nil {
			return nil, err
		}
		if userID == 0 {
			continue
		}
		for tok := range want {
			if _, ok := out[tok]; ok {
				continue
			}
			if unameNorm != "" && unameNorm == tok {
				out[tok] = userID
				continue
			}
			if displayNorm != "" && displayNorm == tok {
				out[tok] = userID
			}
		}
	}
	return out, rows.Err()
}
