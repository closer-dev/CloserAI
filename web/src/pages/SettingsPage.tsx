import { useState, type Ref, type TextareaHTMLAttributes } from "react";
import { useForm } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import { z } from "zod";
import { useMe } from "../api/auth";
import { type Bot, useBots, useConnectBot, useDeleteBot, useUpdateBot } from "../api/bots";
import Field from "../components/Field";
import SubmitButton from "../components/SubmitButton";

// Те же ограничения, что в bot/domain: длины в символах, порог 0..1.
const token = z
  .string()
  .trim()
  .regex(/^[0-9]{5,20}:[A-Za-z0-9_-]{30,}$/, "Скопируйте токен из @BotFather целиком");

const settings = z.object({
  system_prompt: z.string().max(4000, "Не больше 4000 символов"),
  greeting: z.string().max(1000, "Не больше 1000 символов"),
  handoff_threshold: z
    .number("Введите число от 0 до 1")
    .min(0, "Не меньше 0")
    .max(1, "Не больше 1"),
});

export default function SettingsPage() {
  const me = useMe();
  const bots = useBots();
  const isOwner = me.data?.role === "owner";

  return (
    <div className="max-w-3xl">
      <h1 className="text-2xl font-semibold">Настройки бота</h1>
      <p className="mt-1 text-sm text-gray-500">
        Telegram-бот компании отвечает клиентам по базе знаний и передаёт сложные вопросы операторам.
      </p>

      {bots.isPending && <p className="mt-6 text-sm text-gray-500">Загрузка…</p>}
      {bots.error && <p className="mt-6 text-sm text-red-600">{bots.error.message}</p>}

      {bots.data?.map((b) => <BotCard key={b.id} bot={b} editable={isOwner} />)}

      {bots.data && bots.data.length === 0 && !isOwner && (
        <p className="mt-6 rounded-lg border bg-white p-4 text-sm text-gray-600">
          Бот ещё не подключён. Подключить его может владелец компании.
        </p>
      )}

      {bots.data && isOwner && <ConnectForm first={bots.data.length === 0} />}
    </div>
  );
}

// ---------- подключение ----------

function ConnectForm({ first }: { first: boolean }) {
  const connect = useConnectBot();
  const [open, setOpen] = useState(first);
  const { register, handleSubmit, formState, reset } = useForm({
    resolver: zodResolver(z.object({ token })),
  });

  if (!open) {
    return (
      <button
        type="button"
        onClick={() => setOpen(true)}
        className="mt-6 text-sm font-medium text-gray-700 hover:text-gray-900"
      >
        + Подключить ещё одного бота
      </button>
    );
  }

  const onSubmit = handleSubmit((data) =>
    connect.mutate(data, {
      onSuccess: () => {
        reset();
        setOpen(false);
      },
    }),
  );

  return (
    <form onSubmit={onSubmit} className="mt-6 rounded-lg border bg-white p-4" noValidate>
      <h2 className="font-medium">Подключить бота</h2>
      <ol className="mt-2 mb-4 list-decimal space-y-1 pl-5 text-sm text-gray-600">
        <li>
          Откройте{" "}
          <a href="https://t.me/BotFather" target="_blank" rel="noreferrer" className="underline">
            @BotFather
          </a>{" "}
          в Telegram и создайте бота командой <code>/newbot</code>.
        </li>
        <li>Скопируйте токен, который пришлёт BotFather, и вставьте его сюда.</li>
      </ol>
      <Field
        label="Токен бота"
        placeholder="123456789:AA…"
        autoComplete="off"
        spellCheck={false}
        {...register("token")}
        error={formState.errors.token?.message}
      />
      {connect.error && <p className="mt-3 text-sm text-red-600">{connect.error.message}</p>}
      <div className="mt-4 flex items-center gap-3">
        <SubmitButton pending={connect.isPending}>Подключить</SubmitButton>
        {!first && (
          <button type="button" onClick={() => setOpen(false)} className="text-sm text-gray-500 hover:text-gray-900">
            Отмена
          </button>
        )}
      </div>
    </form>
  );
}

// ---------- карточка бота ----------

function BotCard({ bot, editable }: { bot: Bot; editable: boolean }) {
  return (
    <section className="mt-6 rounded-lg border bg-white">
      <header className="flex flex-wrap items-center justify-between gap-3 border-b p-4">
        <div>
          <a href={bot.link} target="_blank" rel="noreferrer" className="font-medium hover:underline">
            @{bot.username}
          </a>
          <div className="text-xs text-gray-500">
            подключён {new Date(bot.created_at).toLocaleDateString("ru-RU")}
          </div>
        </div>
        <div className="flex items-center gap-3">
          <StatusBadge active={bot.is_active} />
          {editable && <ToggleActive bot={bot} />}
        </div>
      </header>
      <SettingsForm bot={bot} editable={editable} />
      {editable && (
        <footer className="flex flex-wrap items-start justify-between gap-4 border-t p-4">
          <ReplaceToken bot={bot} />
          <DeleteBot bot={bot} />
        </footer>
      )}
    </section>
  );
}

function StatusBadge({ active }: { active: boolean }) {
  return (
    <span
      className={`rounded-full px-2 py-0.5 text-xs font-medium ${
        active ? "bg-green-100 text-green-800" : "bg-gray-100 text-gray-600"
      }`}
    >
      {active ? "Отвечает клиентам" : "Выключен"}
    </span>
  );
}

function ToggleActive({ bot }: { bot: Bot }) {
  const update = useUpdateBot(bot.id);
  return (
    <>
      <button
        type="button"
        disabled={update.isPending}
        onClick={() => update.mutate({ is_active: !bot.is_active })}
        className="rounded border px-3 py-1 text-sm hover:bg-gray-50 disabled:opacity-50"
      >
        {bot.is_active ? "Выключить" : "Включить"}
      </button>
      {update.error && <span className="text-xs text-red-600">{update.error.message}</span>}
    </>
  );
}

function SettingsForm({ bot, editable }: { bot: Bot; editable: boolean }) {
  const update = useUpdateBot(bot.id);
  const { register, handleSubmit, formState, reset } = useForm({
    resolver: zodResolver(settings),
    values: {
      system_prompt: bot.system_prompt,
      greeting: bot.greeting,
      handoff_threshold: bot.handoff_threshold,
    },
  });
  const e = formState.errors;

  const onSubmit = handleSubmit((data) => update.mutate(data, { onSuccess: (b) => reset(b) }));

  return (
    <form onSubmit={onSubmit} className="space-y-4 p-4" noValidate>
      <fieldset disabled={!editable} className="space-y-4">
        <TextArea
          label="Приветствие"
          hint="Отправляется клиенту, когда он открывает бота (/start). Пусто — стандартное приветствие."
          rows={2}
          {...register("greeting")}
          error={e.greeting?.message}
        />
        <TextArea
          label="Инструкция для бота"
          hint="Как боту общаться с клиентами: тон, о чём говорить, чего избегать."
          rows={5}
          {...register("system_prompt")}
          error={e.system_prompt?.message}
        />
        <div className="max-w-xs">
          <Field
            label="Порог передачи оператору"
            type="number"
            step="0.05"
            min="0"
            max="1"
            {...register("handoff_threshold", { valueAsNumber: true })}
            error={e.handoff_threshold?.message}
          />
          <p className="mt-1 text-xs text-gray-500">
            От 0 до 1. Чем выше, тем чаще бот передаёт вопрос оператору, если не уверен в ответе.
          </p>
        </div>
      </fieldset>
      {editable && (
        <div className="flex items-center gap-3">
          <SubmitButton pending={update.isPending}>Сохранить</SubmitButton>
          {update.error && <span className="text-sm text-red-600">{update.error.message}</span>}
          {update.isSuccess && !formState.isDirty && <span className="text-sm text-green-700">Сохранено</span>}
        </div>
      )}
    </form>
  );
}

function ReplaceToken({ bot }: { bot: Bot }) {
  const update = useUpdateBot(bot.id);
  const [open, setOpen] = useState(false);
  const { register, handleSubmit, formState, reset } = useForm({
    resolver: zodResolver(z.object({ token })),
  });

  if (!open) {
    return (
      <button type="button" onClick={() => setOpen(true)} className="text-sm text-gray-600 hover:text-gray-900">
        Сменить токен
      </button>
    );
  }

  const onSubmit = handleSubmit((data) =>
    update.mutate(data, {
      onSuccess: () => {
        reset();
        setOpen(false);
      },
    }),
  );

  return (
    <form onSubmit={onSubmit} className="w-full max-w-md space-y-2" noValidate>
      <p className="text-xs text-gray-500">
        Если токен отозван командой <code>/revoke</code> в @BotFather, вставьте новый токен этого же бота.
      </p>
      <Field
        label="Новый токен"
        autoComplete="off"
        spellCheck={false}
        {...register("token")}
        error={formState.errors.token?.message}
      />
      {update.error && <p className="text-sm text-red-600">{update.error.message}</p>}
      <div className="flex items-center gap-3">
        <SubmitButton pending={update.isPending}>Сменить</SubmitButton>
        <button type="button" onClick={() => setOpen(false)} className="text-sm text-gray-500 hover:text-gray-900">
          Отмена
        </button>
      </div>
    </form>
  );
}

function DeleteBot({ bot }: { bot: Bot }) {
  const remove = useDeleteBot(bot.id);
  const onClick = () => {
    if (window.confirm(`Отключить @${bot.username}? Все диалоги этого бота будут удалены.`)) {
      remove.mutate();
    }
  };
  return (
    <div className="text-right">
      <button
        type="button"
        onClick={onClick}
        disabled={remove.isPending}
        className="text-sm text-red-600 hover:text-red-800 disabled:opacity-50"
      >
        Отключить бота
      </button>
      {remove.error && <p className="text-xs text-red-600">{remove.error.message}</p>}
    </div>
  );
}

// ---------- поле ввода ----------

interface TextAreaProps extends TextareaHTMLAttributes<HTMLTextAreaElement> {
  label: string;
  hint?: string;
  error?: string;
  ref?: Ref<HTMLTextAreaElement>;
}

function TextArea({ label, hint, error, ...input }: TextAreaProps) {
  return (
    <label className="flex flex-col gap-1 text-sm">
      <span className="font-medium text-gray-700">{label}</span>
      {hint && <span className="text-xs text-gray-500">{hint}</span>}
      <textarea
        {...input}
        className={`rounded border px-3 py-2 outline-none focus:ring-2 focus:ring-gray-900/20 disabled:bg-gray-50 ${
          error ? "border-red-500" : "border-gray-300"
        }`}
      />
      {error && <span className="text-xs text-red-600">{error}</span>}
    </label>
  );
}
