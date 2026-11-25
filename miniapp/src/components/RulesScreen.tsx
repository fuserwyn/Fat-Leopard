import { useState, type ReactNode } from "react";
import { useI18n } from "../lib/I18nContext";
import type { UILang } from "../lib/i18n";
import { CUP_LEVEL_STARTS, MINIAPP_LEVEL_NAMES, MINIAPP_LEVEL_NAMES_RU } from "../lib/miniappLevel";
import "./RulesScreen.css";

const LEVEL_EMOJI = ["🦫", "🦌", "🦓", "🐆", "🦁", "🐘"] as const;

function levelRows(lang: UILang) {
  const names = lang === "ru" ? MINIAPP_LEVEL_NAMES_RU : MINIAPP_LEVEL_NAMES;
  return names.slice(1).map((name, i) => ({
    emoji: LEVEL_EMOJI[i] ?? "",
    name,
    cups: CUP_LEVEL_STARTS[i] ?? 0,
  }));
}

function LevelsTable({ lang }: { lang: UILang }) {
  const ru = lang === "ru";
  return (
    <table className="rules__table">
      <thead>
        <tr>
          <th>{ru ? "Уровень" : "Level"}</th>
          <th className="rules__table-num">{ru ? "Всего кубков" : "Total cups"}</th>
        </tr>
      </thead>
      <tbody>
        {levelRows(lang).map((row) => (
          <tr key={row.name}>
            <td>
              {row.emoji} {row.name}
            </td>
            <td className="rules__table-num">
              {row.cups === 0
                ? ru
                  ? "старт (0)"
                  : "start (0)"
                : row.cups.toLocaleString(ru ? "ru-RU" : "en-US")}
            </td>
          </tr>
        ))}
      </tbody>
    </table>
  );
}

type RuleSection = {
  id: string;
  title: string;
  summary: string;
  body: ReactNode;
};

function rulesSectionsEn(): RuleSection[] {
  return [
    {
      id: "why",
      title: "1 — Why this app",
      summary: "Fitness that finally fits your life",
      body: (
        <>
          <p>The pack is where movement finally fits into your life.</p>
          <p>
            You know the feeling: “I should work out” — and it slips for weeks. Not because you’re lazy. Because a
            “proper workout” needs time you don’t have and willpower that runs out. The pack and Leo help you. The
            score is consecutive days, not kilometers. What matters is that you moved today. That’s how fitness
            becomes part of your day.
          </p>
        </>
      ),
    },
    {
      id: "principles",
      title: "2 — Principles",
      summary: "Six rules everything runs on",
      body: (
        <ol className="rules__list">
          <li>
            <strong>Any movement counts.</strong> No minimum bar, no “not serious” activities.
          </li>
          <li>
            <strong>Compare yourself to yesterday-you.</strong> No leaderboards — the numbers are yours alone.
          </li>
          <li>
            <strong>Consistency beats volume.</strong> Seven 15-minute sessions beat one hour-long session.
          </li>
          <li>
            <strong>In the pack you’re not alone.</strong> Visibility is support, not surveillance. No anonymity.
          </li>
          <li>
            <strong>Burn is a nudge, not punishment.</strong> A “come back” signal, not a threat.
          </li>
          <li>
            <strong>Slipping is normal; coming back is the path.</strong> Failure is not returning — not a missed day.
          </li>
        </ol>
      ),
    },
    {
      id: "leo",
      title: "3 — Who is Leo",
      summary: "Your coach in the pack",
      body: (
        <>
          <p>
            Leo is your coach in the pack. He watches your streak, answers questions, and celebrates progress. No
            judging, no comparing you to others. Leo doesn’t give medical advice and can be wrong sometimes — if you’re
            unsure about rules, this page wins.
          </p>
          <p>Find Leo on his own tab; he also comments on every workout you log.</p>
          <p className="rules__note">Chat limit — 20 messages per day.</p>
        </>
      ),
    },
    {
      id: "pack",
      title: "4 — What the pack is",
      summary: "Community, feed, and what's allowed",
      body: (
        <>
          <p>
            The pack is people who move every day and a shared feed of their workouts. You see others; others see you.
          </p>
          <p>
            <strong>Allowed:</strong>
          </p>
          <ul className="rules__list rules__list--bullet">
            <li>Log any movement — from walking to HIIT</li>
            <li>React to others’ workouts and comment</li>
            <li>Take sick leave when you need a pause</li>
            <li>Return after removal</li>
          </ul>
          <p>
            <strong>Not allowed:</strong>
          </p>
          <ul className="rules__list rules__list--bullet">
            <li>Insulting others, toxicity</li>
            <li>Ads or external links without approval</li>
            <li>Illegal content</li>
            <li>Fake workouts — cup farming</li>
            <li>Posting others’ personal data without consent</li>
          </ul>
          <p>Violations: warning → content hidden → restrictions → ban from the pack.</p>
          <p>
            Report content — ⋯ on a workout or comment card, or Report under someone’s message in pack chat. Reports go
            to moderators.
          </p>
          <p>
            Support — the Support button in your profile (in the bot and in the miniapp). Message there if something
            broke or Leo wasn’t enough.
          </p>
        </>
      ),
    },
    {
      id: "progress",
      title: "5 — How you progress",
      summary: "Workouts, cups, levels, streaks, achievements",
      body: (
        <>
          <h3 className="rules__h3">Log activities</h3>
          <p>
            Pick a type from many options — yoga, walking, CrossFit, HIIT, and more. Plus Other for everything else.
            Duration 1–480 minutes. Intensity 1 (warm-up) to 5 (max effort). Optional text note.
          </p>
          <h3 className="rules__h3">Cups</h3>
          <p>
            Every workout earns cups. Minimum 1 cup for any session, even one minute. Longer and harder → more. Cups
            accumulate for life; they reset only if you’re removed from the pack. (Why different workouts earn different
            cups — see FAQ.)
          </p>
          <h3 className="rules__h3">Levels</h3>
          <p>Cups move you through levels. Each new level gives +1 streak save.</p>
          <LevelsTable lang="en" />
          <h3 className="rules__h3">Streaks</h3>
          <p>
            A streak is consecutive days with a workout. One miss resets it to zero (unless you use a save). Streak is
            the pack’s headline metric.
          </p>
          <h3 className="rules__h3">Achievements</h3>
          <p>Milestone achievements at 7, 14, 30, 42, 60, 90, 180, 365, 420, 500, and 1000 streak days.</p>
          <p>
            A separate showcase for total workouts: 1, 10, 20, 42, 50, 100, 200, 420, 500, and 1000. The first unlocks
            with your very first workout.
          </p>
        </>
      ),
    },
    {
      id: "streak-save",
      title: "6 — How to save a streak",
      summary: "Saves — buffer when you miss a day",
      body: (
        <>
          <p>Missing a day resets your streak. Saves help.</p>
          <p>
            Spend a save → one missed day doesn’t zero the streak. You start with 1 save, plus 1 per new level (max 7).
          </p>
          <p className="rules__note">
            Saves protect streak only. They don’t stop removal from the pack — you still need a workout for that (see
            below).
          </p>
        </>
      ),
    },
    {
      id: "feed",
      title: "7 — What the pack sees",
      summary: "Feed, reactions, visibility",
      body: (
        <>
          <p>
            A shared chronological feed of everyone’s workouts. Your entry — type, duration, streak, optional note — is
            visible to all right after you save.
          </p>
          <p>
            Reactions: one per person per card. Open comment threads under each workout; Leo may reply there. Your
            current streak shows on the card.
          </p>
          <p className="rules__note">
            Not in the feed: cups, saves, Leo chat history, settings. No anonymity — you’re on your Telegram account.
            Nicknames OK; no incognito mode.
          </p>
        </>
      ),
    },
    {
      id: "missed",
      title: "8 — If you miss workouts",
      summary: "Burn, reminders, way back",
      body: (
        <>
          <p>
            Inactivity timer runs from your last workout. 7 days without movement → removal at midnight in your timezone
            on day 8.
          </p>
          <p>
            Day 5: Leo messages in the bot — gentle first nudge. Day 6: sharper tone. Day 7: final reminder.
          </p>
          <p>
            The way back is one thing — a workout. Any kind, any length. Timer resets; you’re back in rhythm. For illness,
            use sick leave (below).
          </p>
        </>
      ),
    },
    {
      id: "removed",
      title: "9 — If you’re removed and how to return",
      summary: "What resets, what stays, how to return",
      body: (
        <>
          <p>
            Removal — midnight on day 8 without a workout. You leave the pack — chat and feed. The bot sends Leo’s goodbye
            with a return link. Data kept 365 days, then anonymized.
          </p>
          <p>
            <strong>Resets:</strong> cups → 0, streak → 0, saves → 1, level → Meerkat.
          </p>
          <p>
            <strong>Forever yours:</strong> streak record, total workouts, trophy case, workout history. These numbers
            don’t depend on pack membership.
          </p>
          <p>
            <strong>How to return.</strong> Send <code className="rules__code">/start</code> to the bot. You’re recognized
            by Telegram ID. Streak record, workout count, and trophy case return immediately. Cups, streak, level, and
            saves start fresh. The pack doesn’t ask why you were away.
          </p>
          <p className="rules__note">
            Lifetime status of the first 100 members is preserved on removal — historical badge, not a gameplay perk.
          </p>
        </>
      ),
    },
    {
      id: "sick",
      title: "10 — Sick leave",
      summary: "14-day pause on tap",
      body: (
        <>
          <p>
            If you’re sick — tap Take sick leave in profile. Removal timer pauses; streak doesn’t burn.
          </p>
          <p>
            Max 14 days per sick leave. On day 14 it ends automatically; the timer resumes where it left off.
          </p>
          <p>
            End early: I’m better in profile, or any workout — it counts immediately.
          </p>
        </>
      ),
    },
    {
      id: "faq",
      title: "11 — FAQ",
      summary: "FAQ",
      body: (
        <dl className="rules__faq">
          <dt>Why do different workouts earn different cups?</dt>
          <dd>
            Cups depend on duration, intensity (1–5), and activity type. A hard hour beats a 5-minute walk — but a walk
            still earns at least 1 cup. We don’t publish the exact formula on purpose: the point is moving, not
            optimizing numbers. Any movement counts.
          </dd>
          <dt>Can I log chores — cleaning, cooking?</dt>
          <dd>
            No. Only physical activities from the list: walk, run, yoga, strength, and more, plus Other. Chores don’t
            qualify.
          </dd>
          <dt>Can I log yesterday’s workout?</dt>
          <dd>No. Log before 23:59:59 today.</dd>
          <dt>Can I edit after saving?</dt>
          <dd>Not in the current version. You can’t delete either. Made a mistake — contact support.</dd>
          <dt>Leo sometimes says odd things. Normal?</dt>
          <dd>
            Yes — generated text can err. If he said something about rules and you’re unsure, this page wins. Found a clear
            bug — tell support.
          </dd>
          <dt>Can I hide workouts from others?</dt>
          <dd>No. Visibility is required in the pack. No incognito.</dd>
          <dt>Can I change my profile name?</dt>
          <dd>Yes — in profile settings, up to 30 characters.</dd>
          <dt>Can I delete my account myself?</dt>
          <dd>
            No. Profile deletes automatically after 7+ days without workouts. You can also ask support to delete. Data
            kept 365 days after removal, then anonymized.
          </dd>
          <dt>How does sick leave work?</dt>
          <dd>
            Tap Take sick leave in profile. Removal timer pauses. Max 14 days. End with I’m better or any workout.
          </dd>
          <dt>What are saves and where do they come from?</dt>
          <dd>
            Saves buffer your streak. One save → one missed day doesn’t reset streak. Start with 1, +1 per level (max 7).
            They protect streak, not removal.
          </dd>
          <dt>Where if something broke?</dt>
          <dd>Support button — in the bot and in profile in the miniapp.</dd>
        </dl>
      ),
    },
  ];
}

function rulesSectionsRu(): RuleSection[] {
  return [
    {
      id: "why",
      title: "1 — Зачем это приложение",
      summary: "Спорт, который наконец встраивается в твою жизнь",
      body: (
        <>
          <p>Стая — это место, где спорт наконец встраивается в твою жизнь.</p>
          <p>
            Знаешь это чувство: «надо бы заняться спортом» — и оно откладывается неделями? Не потому, что ленивый.
            Потому, что «нормальная тренировка» требует времени, которого нет, и силы воли, которая кончается. Стая и
            Лео помогут тебе. Счёт идёт на дни подряд, а не на километры. Главное — что ты сделал это сегодня. Так
            спорт станет частью твоего дня.
          </p>
        </>
      ),
    },
    {
      id: "principles",
      title: "2 — Принципы",
      summary: "Шесть правил, на которых всё держится",
      body: (
        <ol className="rules__list">
          <li>
            <strong>Любое движение считается.</strong> Нет минимального порога, нет «несерьёзных» активностей.
          </li>
          <li>
            <strong>Сравниваешь себя с собой вчерашним.</strong> Нет лидербордов, числа только твои.
          </li>
          <li>
            <strong>Регулярность важнее объёма.</strong> Семь тренировок по пятнадцать минут больше, чем одна за час.
          </li>
          <li>
            <strong>В Стае ты не один.</strong> Публичность как поддержка, не контроль. Анонимности нет.
          </li>
          <li>
            <strong>Burn — это напоминание, не наказание.</strong> Сигнал «вернись», не угроза.
          </li>
          <li>
            <strong>Срываться нормально, возвращаться — путь.</strong> Провал — это не вернуться, а не пропуск.
          </li>
        </ol>
      ),
    },
    {
      id: "leo",
      title: "3 — Кто такой Лео",
      summary: "Твой тренер в Стае",
      body: (
        <>
          <p>
            Лео — твой тренер в Стае. Он смотрит на твою серию, отвечает на вопросы и отмечает прогресс. Не судит, не
            сравнивает с другими. Лео не даёт медицинских советов и иногда может ошибаться — если сомневаешься в его
            словах про правила, эта страница приоритетнее.
          </p>
          <p>Найти Лео можно в отдельной вкладке, плюс он сам комментирует каждую твою тренировку.</p>
          <p className="rules__note">Лимит общения — 20 сообщений в день.</p>
        </>
      ),
    },
    {
      id: "pack",
      title: "4 — Что такое Стая",
      summary: "Сообщество, лента и что можно/нельзя",
      body: (
        <>
          <p>
            Стая — это сообщество людей, которые двигаются каждый день, и общая лента их тренировок. Ты видишь других,
            другие видят тебя.
          </p>
          <p>
            <strong>Что можно:</strong>
          </p>
          <ul className="rules__list rules__list--bullet">
            <li>Логировать любое движение — от ходьбы до HIIT</li>
            <li>Реагировать на тренировки других и комментировать</li>
            <li>Брать больничный, когда нужна пауза</li>
            <li>Вернуться после удаления</li>
          </ul>
          <p>
            <strong>Что нельзя:</strong>
          </p>
          <ul className="rules__list rules__list--bullet">
            <li>Оскорблять других участников, токсичить</li>
            <li>Рекламировать, оставлять ссылки на внешние ресурсы без согласования</li>
            <li>Публиковать контент, нарушающий законодательство РФ</li>
            <li>Логировать тренировки, которых не было — фарм кубков</li>
            <li>Публиковать личные данные других людей без их согласия</li>
          </ul>
          <p>За нарушения: предупреждение → скрытие контента → ограничение → бан из Стаи.</p>
          <p>
            Пожаловаться на контент — ⋯ на карточке тренировки или комментария в ленте, либо «Пожаловаться» под
            чужим сообщением в общем чате. Жалоба уходит модератору.
          </p>
          <p>
            Поддержка — кнопка «Поддержка» в твоём профиле: она есть и в боте, и в профиле внутри мини-аппа. Напиши
            туда, если что-то сломалось или Лео не справился.
          </p>
        </>
      ),
    },
    {
      id: "progress",
      title: "5 — Как ты движешься",
      summary: "Тренировки, кубки, уровни, стрики, ачивки",
      body: (
        <>
          <h3 className="rules__h3">Логируй активности</h3>
          <p>
            Выбираешь тип из множества вариантов — от йоги и ходьбы до кроссфита и HIIT. Плюс «Другое» для всего
            остального. Длительность от 1 до 480 минут. Интенсивность от 1 («разминка») до 5 («на пределе»).
            Опционально — текстовая заметка.
          </p>
          <h3 className="rules__h3">Кубки</h3>
          <p>
            Каждая тренировка приносит кубки. Минимум — 1 кубок за любую тренировку, даже за одну минуту. Чем дольше
            и интенсивнее — тем больше. Кубки накапливаются всю жизнь, сгорают только при удалении из Стаи. (Почему за
            разные тренировки начисляется разное число кубков — см. FAQ.)
          </p>
          <h3 className="rules__h3">Уровни</h3>
          <p>Кубки двигают тебя по уровням. Каждый новый уровень даёт +1 попытку для защиты стрика.</p>
          <LevelsTable lang="ru" />
          <h3 className="rules__h3">Стрики</h3>
          <p>
            Стрик — это дни с тренировкой подряд. Один пропуск — и стрик падает в ноль (если не спас попыткой). Стрик
            — главная метрика Стаи.
          </p>
          <h3 className="rules__h3">Ачивки</h3>
          <p>Milestone-ачивки на отметках 7, 14, 30, 42, 60, 90, 180, 365, 420, 500 и 1000 дней стрика.</p>
          <p>
            Отдельная витрина — за общее число тренировок: 1, 10, 20, 42, 50, 100, 200, 420, 500 и 1000. Первая
            ачивка приходит сразу за первую тренировку.
          </p>
        </>
      ),
    },
    {
      id: "streak-save",
      title: "6 — Как сохранить стрик",
      summary: "Попытки — буфер на случай пропуска",
      body: (
        <>
          <p>Стрик обнуляется при пропуске дня. Спасти его помогают попытки.</p>
          <p>
            Потратил попытку → один пропущенный день не обнуляет стрик. Стартовый запас — 1 попытка. Плюс 1 за каждый
            новый уровень (максимум 7).
          </p>
          <p className="rules__note">
            Попытки защищают только стрик. От удаления из Стаи они не спасают — для этого нужна тренировка (см.
            ниже).
          </p>
        </>
      ),
    },
    {
      id: "feed",
      title: "7 — Что видит Стая",
      summary: "Лента, реакции, публичность",
      body: (
        <>
          <p>
            Общая хронологическая лента всех тренировок всех участников. Твоя запись — тип, длительность, стрик,
            опциональная заметка — видна всем сразу после сохранения.
          </p>
          <p>
            Реакции: по одной от каждого участника на карточку. Под каждой тренировкой — открытый тред комментариев,
            Лео может ответить там же. Рядом с карточкой виден твой текущий стрик.
          </p>
          <p className="rules__note">
            Что не попадает в ленту: кубки, попытки, история чата с Лео, настройки. Анонимности нет — ты под своим
            Telegram-аккаунтом. Псевдоним возможен, инкогнито-режима нет.
          </p>
        </>
      ),
    },
    {
      id: "missed",
      title: "8 — Если ты пропустил",
      summary: "Burn, напоминания, путь назад",
      body: (
        <>
          <p>
            Таймер неактивности идёт с последней тренировки. 7 дней без движения → удаление в 00:00 твоего часового
            пояса на день 8.
          </p>
          <p>
            День 5: Лео пишет в бот. Первое напоминание — спокойно, без давления. День 6: тон острее. День 7:
            финальное напоминание.
          </p>
          <p>
            Путь назад один — тренировка. Любая, любой длины. Таймер сбрасывается, ты снова в ритме. Если нужна пауза
            по болезни — есть больничный (см. ниже).
          </p>
        </>
      ),
    },
    {
      id: "removed",
      title: "9 — Если ты удалён и как вернуться",
      summary: "Что обнуляется, что сохраняется, как вернуться",
      body: (
        <>
          <p>
            Удаление — 00:00 на день 8 без тренировки. Ты выходишь из Стаи — из чата, из ленты. Бот присылает
            прощальное сообщение от Лео со ссылкой на возврат. Данные хранятся 365 дней, затем анонимизируются.
          </p>
          <p>
            <strong>Что обнуляется:</strong> кубки → 0, стрик → 0, попытки → 1, уровень → Сурикат.
          </p>
          <p>
            <strong>Что сохраняется навсегда:</strong> рекорд стрика, общее число тренировок, trophy case и история
            тренировок. Эти числа твои — не зависят от Стаи.
          </p>
          <p>
            <strong>Как вернуться.</strong> Напиши <code className="rules__code">/start</code> боту. Система опознает
            тебя по Telegram ID. Сразу вернутся рекорд стрика, число тренировок и trophy case. С нуля начнутся кубки,
            стрик, уровень и попытки. Стая не спрашивает, почему ты пропал.
          </p>
          <p className="rules__note">
            Lifetime-статус первых 100 участников сохраняется при удалении — это историческая отметка, не привилегия
            в механике.
          </p>
        </>
      ),
    },
    {
      id: "sick",
      title: "10 — Больничный",
      summary: "Пауза на 14 дней по кнопке",
      body: (
        <>
          <p>
            Если болен — нажми кнопку «Взять больничный» в профиле. Таймер удаления останавливается, стрик не горит.
          </p>
          <p>
            Максимум — 14 дней на один больничный. На 14-й день больничный снимается автоматически, таймер
            возобновляется с того же дня.
          </p>
          <p>
            Снять больничный раньше: кнопка «Я выздоровел» в профиле или любая тренировка — она сразу засчитывается.
          </p>
        </>
      ),
    },
    {
      id: "faq",
      title: "11 — FAQ",
      summary: "Частые вопросы",
      body: (
        <dl className="rules__faq">
          <dt>Почему за тренировки начисляются разные кубки?</dt>
          <dd>
            Кубки зависят от трёх вещей: сколько ты занимался (длительность), насколько интенсивно (1–5) и какой тип
            активности выбрал. Часовая силовая на интенсивности 5 принесёт больше, чем пятиминутная прогулка — но и
            прогулка даст минимум 1 кубок. Точную формулу мы намеренно не раскрываем: смысл не в том, чтобы
            оптимизировать цифры, а в том, чтобы двигаться. Любое движение считается.
          </dd>
          <dt>Можно ли логировать домашние дела — уборку, готовку?</dt>
          <dd>
            Нет. Логируются физические активности из списка: ходьба, бег, йога, силовая и другие, плюс «Другое».
            Уборка туда не попадает.
          </dd>
          <dt>Можно ли добавить тренировку за вчера?</dt>
          <dd>Нет. Тренировку нужно занести до 23:59:59 текущего дня.</dd>
          <dt>Можно ли изменить запись после сохранения?</dt>
          <dd>В текущей версии — нет. Удалить тоже нельзя. Если ошибся — напиши в поддержку.</dd>
          <dt>Лео иногда говорит странные вещи. Это нормально?</dt>
          <dd>
            Да, он генерирует текст и иногда ошибается. Если он сказал что-то про правила и ты сомневаешься — эта
            страница приоритетнее. Нашёл явную ошибку — сообщи в поддержку.
          </dd>
          <dt>Можно ли скрыть тренировки от других?</dt>
          <dd>Нет. Публичность — условие участия в Стае. Режима инкогнито нет.</dd>
          <dt>Можно ли поменять имя в профиле?</dt>
          <dd>Да. В настройках профиля — до 30 символов.</dd>
          <dt>Можно ли удалить аккаунт самому?</dt>
          <dd>
            Нет. Твой профиль удалится автоматически, если ты не занимался более 7 дней. Также ты можешь попросить
            удалить аккаунт через поддержку. Данные хранятся 365 дней после удаления, затем анонимизируются.
          </dd>
          <dt>Как работает больничный?</dt>
          <dd>
            Нажимаешь «Взять больничный» в профиле. Таймер удаления останавливается. Максимум 14 дней. Снять: «Я
            выздоровел» или любая тренировка.
          </dd>
          <dt>Что такое попытки и откуда они берутся?</dt>
          <dd>
            Попытки — буфер для стрика. Потратил попытку → один пропущенный день не обнуляет стрик. Стартовый запас — 1
            попытка, плюс 1 за каждый новый уровень (максимум 7). Защищают стрик, не от удаления.
          </dd>
          <dt>Куда писать, если что-то сломалось?</dt>
          <dd>Кнопка «Поддержка» — она есть и в боте, и в профиле в мини-аппе.</dd>
        </dl>
      ),
    },
  ];
}

function rulesSections(lang: UILang): RuleSection[] {
  return lang === "ru" ? rulesSectionsRu() : rulesSectionsEn();
}

function RuleAccordionItem({
  section,
  open,
  onToggle,
}: {
  section: RuleSection;
  open: boolean;
  onToggle: () => void;
}) {
  return (
    <section className={`rules__accordion${open ? " rules__accordion--open" : ""}`}>
      <button
        type="button"
        className="rules__accordion-trigger"
        onClick={onToggle}
        aria-expanded={open}
        aria-controls={`rules-panel-${section.id}`}
      >
        <span className="rules__accordion-head">
          <span className="rules__accordion-title">{section.title}</span>
          <span className="rules__accordion-summary">{section.summary}</span>
        </span>
        <span className="rules__accordion-chevron" aria-hidden />
      </button>
      {open ? (
        <div id={`rules-panel-${section.id}`} className="rules__accordion-body" role="region">
          {section.body}
        </div>
      ) : null}
    </section>
  );
}

export function RulesScreen() {
  const { lang } = useI18n();
  const sections = rulesSections(lang);
  const [openId, setOpenId] = useState<string | null>(null);

  return (
    <div className="rules">
      <h1 className="rules__title">{lang === "ru" ? "Правила Стаи" : "Pack rules"}</h1>
      <div className="rules__accordion-list">
        {sections.map((section) => (
          <RuleAccordionItem
            key={section.id}
            section={section}
            open={openId === section.id}
            onToggle={() => setOpenId((cur) => (cur === section.id ? null : section.id))}
          />
        ))}
      </div>
    </div>
  );
}
