import { NavLink, Route, Routes } from "react-router";
import ConversationsPage from "./pages/ConversationsPage";
import KnowledgePage from "./pages/KnowledgePage";
import SettingsPage from "./pages/SettingsPage";
import LoginPage from "./pages/LoginPage";

const nav = [
  { to: "/", label: "Диалоги" },
  { to: "/knowledge", label: "База знаний" },
  { to: "/settings", label: "Настройки" },
];

export default function App() {
  return (
    <Routes>
      <Route path="/login" element={<LoginPage />} />
      <Route
        path="*"
        element={
          <div className="flex min-h-screen bg-gray-50 text-gray-900">
            <aside className="w-56 border-r bg-white p-4">
              <div className="mb-6 text-lg font-semibold">CloserAI</div>
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
            </aside>
            <main className="flex-1 p-6">
              <Routes>
                <Route path="/" element={<ConversationsPage />} />
                <Route path="/knowledge" element={<KnowledgePage />} />
                <Route path="/settings" element={<SettingsPage />} />
              </Routes>
            </main>
          </div>
        }
      />
    </Routes>
  );
}
