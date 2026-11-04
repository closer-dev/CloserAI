import { useEffect, useState } from "react";
import { useForm } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import { z } from "zod";
import { Navigate, useLocation, useNavigate } from "react-router";
import { useLogout, useMe, useResendCode, useVerifyEmail } from "../api/auth";
import Field from "../components/Field";
import SubmitButton from "../components/SubmitButton";

const RESEND_COOLDOWN = 60;

const schema = z.object({
  code: z.string().trim().regex(/^\d{6}$/, "Код состоит из 6 цифр"),
});

export default function VerifyEmailPage() {
  const me = useMe();
  const navigate = useNavigate();
  const location = useLocation();
  const from = (location.state as { from?: string } | null)?.from ?? "/";

  const verify = useVerifyEmail();
  const resend = useResendCode();
  const logout = useLogout();
  const { register, handleSubmit, formState } = useForm({ resolver: zodResolver(schema) });

  // Код уже отправлен при регистрации или входе, поэтому сразу запускаем таймер.
  const [cooldown, setCooldown] = useState(RESEND_COOLDOWN);
  useEffect(() => {
    if (cooldown <= 0) return;
    const t = setTimeout(() => setCooldown((c) => c - 1), 1000);
    return () => clearTimeout(t);
  }, [cooldown]);

  if (me.isPending) return <div className="p-6 text-sm text-gray-500">Загрузка…</div>;
  if (!me.data) return <Navigate to="/login" replace />;
  if (me.data.email_verified) return <Navigate to={from} replace />;

  const onSubmit = handleSubmit(({ code }) =>
    verify.mutate(code, { onSuccess: () => navigate(from, { replace: true }) }),
  );

  return (
    <div className="flex min-h-screen items-center justify-center bg-gray-50 p-4">
      <div className="w-full max-w-sm rounded-lg border bg-white p-6 shadow-sm">
        <h1 className="mb-2 text-xl font-semibold">Подтвердите email</h1>
        <p className="mb-6 text-sm text-gray-600">
          Мы отправили 6-значный код на <span className="font-medium">{me.data.email}</span>. Код действует 15
          минут.
        </p>

        <form onSubmit={onSubmit} className="flex flex-col gap-4" noValidate>
          <Field
            label="Код из письма"
            inputMode="numeric"
            autoComplete="one-time-code"
            maxLength={6}
            autoFocus
            {...register("code")}
            error={formState.errors.code?.message}
          />
          {verify.error && <p className="text-sm text-red-600">{verify.error.message}</p>}
          <SubmitButton pending={verify.isPending}>Подтвердить</SubmitButton>
        </form>

        <div className="mt-4 flex items-center justify-between text-sm">
          <button
            type="button"
            disabled={cooldown > 0 || resend.isPending}
            onClick={() => resend.mutate(undefined, { onSuccess: () => setCooldown(RESEND_COOLDOWN) })}
            className="text-gray-700 hover:text-gray-900 disabled:text-gray-400"
          >
            {cooldown > 0 ? `Отправить снова через ${cooldown} с` : "Отправить код снова"}
          </button>
          <button
            type="button"
            onClick={() => logout.mutate(undefined, { onSuccess: () => navigate("/login", { replace: true }) })}
            className="text-gray-500 hover:text-gray-900"
          >
            Выйти
          </button>
        </div>
        {resend.error && <p className="mt-2 text-sm text-red-600">{resend.error.message}</p>}
        {resend.isSuccess && cooldown > 0 && <p className="mt-2 text-sm text-green-700">Новый код отправлен</p>}
      </div>
    </div>
  );
}
