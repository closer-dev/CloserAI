import type { ReactNode } from "react";
import { Navigate, useLocation } from "react-router";
import { useMe } from "../api/auth";

// Пускает дальше только вошедших пользователей с подтверждённым email.
// Не вошедших отправляет на /login, неподтверждённых — на /verify.
export default function RequireAuth({ children }: { children: ReactNode }) {
  const me = useMe();
  const location = useLocation();

  if (me.isPending) {
    return <div className="p-6 text-sm text-gray-500">Загрузка…</div>;
  }
  if (me.isError) {
    return <div className="p-6 text-sm text-red-600">Не удалось связаться с сервером: {me.error.message}</div>;
  }
  if (!me.data) {
    return <Navigate to="/login" replace state={{ from: location.pathname }} />;
  }
  if (!me.data.email_verified) {
    return <Navigate to="/verify" replace state={{ from: location.pathname }} />;
  }
  return children;
}
