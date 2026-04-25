import { useCallback, useState } from "react";
import { useTelegramWebApp } from "./hooks/useTelegramWebApp";
import { BottomNav } from "./components/BottomNav";
import { ChatScreen } from "./components/ChatScreen";
import { FeedScreen } from "./components/FeedScreen";
import { ProfileScreen } from "./components/ProfileScreen";
import { NewWorkoutScreen } from "./components/NewWorkoutScreen";
import "./App.css";

type Tab = "chat" | "feed" | "profile";

export function App() {
  const { name, streak, setStreak, initData, inTelegram, tg } = useTelegramWebApp();
  const showAlert = useCallback((m: string) => {
    if (tg?.showAlert) void tg.showAlert(m);
    else window.alert(m);
  }, [tg]);
  const [tab, setTab] = useState<Tab>("feed");
  const [workoutOpen, setWorkoutOpen] = useState(false);
  const [workouts, setWorkouts] = useState(1);

  return (
    <div className="app">
      {tab === "chat" && <ChatScreen name={name} initData={initData} inTelegram={inTelegram} showAlert={showAlert} />}
      {tab === "feed" && <FeedScreen name={name} streak={streak} initData={initData} inTelegram={inTelegram} />}
      {tab === "profile" && <ProfileScreen name={name} streak={streak} workouts={workouts} />}

      <BottomNav
        active={tab}
        onChat={() => setTab("chat")}
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
