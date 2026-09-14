package worker

import (
	"fmt"
	"log"
	"strings"
	"time"

	"leo-tracker/internal/agent"
	"leo-tracker/internal/config"
	"leo-tracker/internal/notify"
	"leo-tracker/internal/store"
)

// Прогон агента ограничен 20 минутами, плюс clone и push. Всё, что висит
// в running дольше часа, уже никто не доделает.
const (
	staleAfter = time.Hour
	staleTick  = 10 * time.Minute
)

func Loop(cfg config.Config, st *store.Store, stop <-chan struct{}) {
	tick := time.NewTicker(10 * time.Second)
	defer tick.Stop()
	stale := time.NewTicker(staleTick)
	defer stale.Stop()
	// Процесс только поднялся: running из базы остались от прошлого
	// контейнера, их горутины умерли вместе с ним.
	sweepStale(cfg, st, time.Now())
	runOnce(cfg, st)
	for {
		select {
		case <-stop:
			return
		case <-tick.C:
			runOnce(cfg, st)
		case <-stale.C:
			sweepStale(cfg, st, time.Now().Add(-staleAfter))
		}
	}
}

func sweepStale(cfg config.Config, st *store.Store, cutoff time.Time) {
	jobs, err := st.FailStale(cutoff, "Агент завис: задача висела в работе без исполнителя")
	if err != nil {
		log.Printf("трекер: не снять зависшие задачи: %v", err)
		return
	}
	for _, job := range jobs {
		log.Printf("трекер: задача #%d зависла, сняли (source=%d)", job.ID, job.SourceTaskID)
		text := fmt.Sprintf("⚠️ %s: агент завис, задача снята с ошибкой. Доска запустит агента заново.", jobNotifyLabel(job))
		if err := notify.JobDone(cfg, job, text); err != nil {
			log.Printf("трекер: не уведомить #%d: %v", job.ID, err)
		}
	}
}

func runOnce(cfg config.Config, st *store.Store) {
	due, err := st.ClaimDue(time.Now(), 1)
	if err != nil {
		log.Printf("трекер: не забрать задачи: %v", err)
		return
	}
	for _, job := range due {
		go finish(cfg, st, job)
	}
}

func finish(cfg config.Config, st *store.Store, job store.Job) {
	store.AppendStep(&job, "Агент: запустили")
	if job.Branch == "" && job.SourceTaskID > 0 {
		job.Branch = st.SourceBranch(job.SourceTaskID)
	}
	_ = st.Save(job)

	res, err := agent.Run(cfg, job)
	if err != nil {
		job.Status = "error"
		job.Error = err.Error()
		if !strings.Contains(job.Error, "нет правок") {
			job.Error = "Агент не стартовал: " + job.Error
		}
		store.AppendStep(&job, "Агент не стартовал")
		_ = st.Save(job)
		_ = notify.JobDone(cfg, job, fmt.Sprintf("⚠️ %s: агент не стартовал.\n%s", jobNotifyLabel(job), job.Error))
		return
	}
	hasCode := res.HasImpl
	note := strings.TrimSpace(res.Note)
	if verdict := noCodeVerdict(job.Phase, hasCode); verdict != "" {
		note = verdict
	}
	job.Error = ""
	job.Result = note
	if res.Committed {
		job.Branch = res.Branch
	}
	job.Status = "done"
	store.AppendStep(&job, "Агент сдал результат")
	if res.Committed {
		label := "выполнение"
		switch strings.ToLower(strings.TrimSpace(job.Phase)) {
		case "review":
			label = "ревью"
		case "test":
			label = "тест"
		}
		store.AppendStep(&job, "коммит "+res.Commit+" "+label)
		store.AppendStep(&job, "ветка "+res.Branch)
	}
	if !hasCode {
		store.AppendStep(&job, "кода в репозитории нет")
	}
	_ = st.Save(job)

	if err := notify.JobDone(cfg, job, notifyText(job, note, res.Branch, res.Commit, hasCode)); err != nil {
		log.Printf("трекер: не уведомить #%d: %v", job.ID, err)
	}
}
