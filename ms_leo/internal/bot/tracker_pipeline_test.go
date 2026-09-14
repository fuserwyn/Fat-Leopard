package bot

import (
	"testing"

	"leo-bot/internal/database"
)

func TestTrackerTaskInPipelineIgnoresWaitingAndFailedDoing(t *testing.T) {
	cases := []struct {
		name string
		task database.TrackerTask
		want bool
	}{
		{
			name: "в работе ждёт очередь",
			task: database.TrackerTask{DevColumn: trackerColDoing, Status: "running",
				Steps: []string{"Взяли в работу", trackerAgentWaitingStep}},
			want: false,
		},
		{
			name: "в работе агент не стартовал",
			task: database.TrackerTask{DevColumn: trackerColDoing, Status: "running",
				Error: "Агент не стартовал: clone", Steps: []string{"Агент: запустили", "Агент не стартовал"}},
			want: false,
		},
		{
			name: "в работе с ошибкой",
			task: database.TrackerTask{DevColumn: trackerColDoing, Status: "error"},
			want: false,
		},
		{
			name: "в работе живой агент",
			task: database.TrackerTask{DevColumn: trackerColDoing, Status: "running",
				Steps: []string{"Агент: запустили", "агент:#709"}},
			want: true,
		},
		{
			name: "ревью",
			task: database.TrackerTask{DevColumn: trackerColReview, Status: "running",
				Steps: []string{"Агент сдал результат"}},
			want: true,
		},
		{
			name: "сборка выполнена",
			task: database.TrackerTask{DevColumn: trackerColDeploy, Status: "done"},
			want: false,
		},
	}
	for _, c := range cases {
		if got := trackerTaskInPipeline(c.task); got != c.want {
			t.Errorf("%s: trackerTaskInPipeline = %v, want %v", c.name, got, c.want)
		}
	}
}
