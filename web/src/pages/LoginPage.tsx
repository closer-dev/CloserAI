import { useState } from "react";
import { useForm } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import { z } from "zod";
import { Navigate, useLocation, useNavigate } from "react-router";
import { useLogin, useMe, useRegister } from "../api/auth";
import Field from "../components/Field";
import SubmitButton from "../components/SubmitButton";

const loginSchema = z.object({
  email: z.email("Введите корректный email"),
  password: z.string().min(1, "Введите пароль"),
});

const registerSchema = z.object({
  company_name: z.string().trim().min(1, "Укажите название компании").max(200),
  name: z.string().trim().min(1, "Укажите имя").max(200),
  email: z.email("Введите корректный email"),
  password: z.string().min(8, "Минимум 8 символов").max(72, "Максимум 72 символа"),
});

type Mode = "login" | "register";

export default function LoginPage() {
  const [mode, setMode] = useState<Mode>("login");
  const me = useMe();
  const location = useLocation();
  const from = (location.state as { from?: string } | null)?.from ?? "/";

  if (me.data) return <Navigate to={from} replace />;

  return (
    <div className="flex min-h-screen items-center justify-center bg-gray-50 p-4">
      <div className="w-full max-w-sm rounded-lg border bg-white p-6 shadow-sm">
        <div className="mb-6 text-center text-xl font-semibold">CloserAI</div>
        <div className="mb-6 grid grid-cols-2 rounded bg-gray-100 p-1 text-sm">
          {(["login", "register"] as const).map((m) => (
            <button
              key={m}
              type="button"
              onClick={() => setMode(m)}
              className={`rounded py-1.5 ${mode === m ? "bg-white shadow-sm" : "text-gray-500"}`}
            >
              {m === "login" ? "Вход" : "Регистрация"}
            </button>
          ))}
        </div>
        {mode === "login" ? <LoginForm to={from} /> : <RegisterForm to={from} />}
      </div>
    </div>
  );
}

function LoginForm({ to }: { to: string }) {
  const navigate = useNavigate();
  const login = useLogin();
  const { register, handleSubmit, formState } = useForm({ resolver: zodResolver(loginSchema) });

  const onSubmit = handleSubmit((data) => login.mutate(data, { onSuccess: () => navigate(to, { replace: true }) }));

  return (
    <form onSubmit={onSubmit} className="flex flex-col gap-4" noValidate>
      <Field label="Email" type="email" autoComplete="email" {...register("email")} error={formState.errors.email?.message} />
      <Field
        label="Пароль"
        type="password"
        autoComplete="current-password"
        {...register("password")}
        error={formState.errors.password?.message}
      />
      {login.error && <p className="text-sm text-red-600">{login.error.message}</p>}
      <SubmitButton pending={login.isPending}>Войти</SubmitButton>
    </form>
  );
}

function RegisterForm({ to }: { to: string }) {
  const navigate = useNavigate();
  const signup = useRegister();
  const { register, handleSubmit, formState } = useForm({ resolver: zodResolver(registerSchema) });
  const e = formState.errors;

  const onSubmit = handleSubmit((data) => signup.mutate(data, { onSuccess: () => navigate(to, { replace: true }) }));

  return (
    <form onSubmit={onSubmit} className="flex flex-col gap-4" noValidate>
      <Field label="Компания" {...register("company_name")} error={e.company_name?.message} />
      <Field label="Ваше имя" autoComplete="name" {...register("name")} error={e.name?.message} />
      <Field label="Email" type="email" autoComplete="email" {...register("email")} error={e.email?.message} />
      <Field
        label="Пароль"
        type="password"
        autoComplete="new-password"
        {...register("password")}
        error={e.password?.message}
      />
      {signup.error && <p className="text-sm text-red-600">{signup.error.message}</p>}
      <SubmitButton pending={signup.isPending}>Создать компанию</SubmitButton>
    </form>
  );
}
