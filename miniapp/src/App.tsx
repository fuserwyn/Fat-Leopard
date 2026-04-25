import { useState } from "react";
import { useTelegramWebApp } from "./hooks/useTelegramWebApp";
import { BottomNav } from "./components/BottomNav";
import { FeedScreen } from "./components/FeedScreen";
import { ProfileScreen } from "./components/ProfileScreen";
import { NewWorkoutScreen } from "./components/NewWorkoutScreen";
import "./App.css";

type Tab = "feed" | "profile";

export function App() {
  const { name, streak, setStreak, tg } = useTelegramWebApp();
  const [tab, setTab] = useState<Tab>("feed");
  const [workoutOpen, setWorkoutOpen] = useState(false);
  const [workouts, setWorkouts] = useState(1);

  return (
    <div className="app">
      {tab === "feed" && <FeedScreen name={name} streak={streak} />}
      {tab === "profile" && <ProfileScreen name={name} streak={streak} workouts={workouts} />}

      <BottomNav
        active={tab}
        onFeed={() => setTab("feed")}
        onWorkout={() => setWorkoutOpen(true)}
        onProfile={() => setTab("profile")}
      />

      {workoutOpen && (
        <NewWorkoutScreen
          onClose={() => setWorkoutOpen(false)}
          onSave={({ type: _t, min, intensity: _i }) => {
            setWorkouts((c) => c + 1);
            setStreak((s) => s + 1);
            if (tg?.showAlert) {
              void tg.showAlert(`Сохранено: ${min} мин.`);
            }
          }}
        />
      )}
    </div>
  );
}
