import { NavLink, Route, Routes, useNavigate } from "react-router";
import ConversationsPage from "./pages/ConversationsPage";
import KnowledgePage from "./pages/KnowledgePage";
import SettingsPage from "./pages/SettingsPage";
import LoginPage from "./pages/LoginPage";
import TeamPage from "./pages/TeamPage";
import VerifyEmailPage from "./pages/VerifyEmailPage";
import RequireAuth from "./components/RequireAuth";
import { useLogout, useMe, useTenant } from "./api/auth";

const nav = [
  { to: "/", label: "Диалоги" },
  { to: "/knowledge", label: "База знаний" },
  { to: "/settings", label: "Настройки" },
  { to: "/team", label: "Команда" },
];

export default function App() {
  return (
    <Routes>
      <Route path="/login" element={<LoginPage />} />
      <Route path="/verify" element={<VerifyEmailPage />} />
      <Route
        path="*"
        element={
          <RequireAuth>
            <Layout />
          </RequireAuth>
        }
      />
    </Routes>
  );
}

function Layout() {
  const me = useMe();
  const tenant = useTenant();
  const logout = useLogout();
  const navigate = useNavigate();

  return (
    <div className="flex min-h-screen bg-gray-50 text-gray-900">
      <aside className="flex w-56 flex-col border-r bg-white p-4">
        <div className="text-lg font-semibold">CloserAI</div>
        <div className="mb-6 truncate text-xs text-gray-500">{tenant.data?.name}</div>
        <nav className="flex flex-col gap-1">
          {nav.map((item) => (
            <NavLink
              key={item.to}
              to={item.to}
              end
              className={({ isActive }) =>
                `rounded px-3 py-2 text-sm ${isActive ? "bg-gray-900 text-white" : "hover:bg-gray-100"}`
              }
            >
              {item.label}
            </NavLink>
          ))}
        </nav>
        <div className="mt-auto border-t pt-4 text-sm">
          <div className="truncate font-medium">{me.data?.name}</div>
          <div className="mb-2 truncate text-xs text-gray-500">{me.data?.email}</div>
          <button
            type="button"
            onClick={() => logout.mutate(undefined, { onSuccess: () => navigate("/login", { replace: true }) })}
            className="text-xs text-gray-500 hover:text-gray-900"
          >
            Выйти
          </button>
        </div>
      </aside>
      <main className="flex-1 p-6">
        <Routes>
          <Route path="/" element={<ConversationsPage />} />
          <Route path="/knowledge" element={<KnowledgePage />} />
          <Route path="/settings" element={<SettingsPage />} />
          <Route path="/team" element={<TeamPage />} />
        </Routes>
      </main>
    </div>
  );
}
