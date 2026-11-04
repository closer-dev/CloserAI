import { useForm } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import { z } from "zod";
import { useCreateUser, useMe, useUsers } from "../api/auth";
import Field from "../components/Field";
import SubmitButton from "../components/SubmitButton";

const roleLabel = { owner: "Владелец", operator: "Оператор" } as const;

const schema = z.object({
  name: z.string().trim().min(1, "Укажите имя").max(200),
  email: z.email("Введите корректный email"),
  password: z.string().min(8, "Минимум 8 символов").max(72, "Максимум 72 символа"),
  role: z.enum(["operator", "owner"]),
});

export default function TeamPage() {
  const me = useMe();
  const users = useUsers();

  return (
    <div className="max-w-3xl">
      <h1 className="text-2xl font-semibold">Команда</h1>

      <div className="mt-6 overflow-hidden rounded-lg border bg-white">
        <table className="w-full text-sm">
          <thead className="bg-gray-50 text-left text-gray-500">
            <tr>
              <th className="px-4 py-2 font-medium">Имя</th>
              <th className="px-4 py-2 font-medium">Email</th>
              <th className="px-4 py-2 font-medium">Роль</th>
            </tr>
          </thead>
          <tbody>
            {users.isPending && (
              <tr>
                <td colSpan={3} className="px-4 py-3 text-gray-500">
                  Загрузка…
                </td>
              </tr>
            )}
            {users.data?.map((u) => (
              <tr key={u.id} className="border-t">
                <td className="px-4 py-2">{u.name}</td>
                <td className="px-4 py-2">{u.email}</td>
                <td className="px-4 py-2">{roleLabel[u.role]}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>

      {me.data?.role === "owner" && <InviteForm />}
    </div>
  );
}

function InviteForm() {
  const create = useCreateUser();
  const { register, handleSubmit, formState, reset } = useForm({
    resolver: zodResolver(schema),
    defaultValues: { role: "operator" as const },
  });
  const e = formState.errors;

  const onSubmit = handleSubmit((data) => create.mutate(data, { onSuccess: () => reset() }));

  return (
    <form onSubmit={onSubmit} className="mt-8 rounded-lg border bg-white p-4" noValidate>
      <h2 className="mb-1 font-medium">Добавить сотрудника</h2>
      <p className="mb-4 text-xs text-gray-500">Задайте временный пароль и передайте его сотруднику.</p>
      <div className="grid gap-4 sm:grid-cols-2">
        <Field label="Имя" {...register("name")} error={e.name?.message} />
        <Field label="Email" type="email" {...register("email")} error={e.email?.message} />
        <Field label="Временный пароль" type="text" {...register("password")} error={e.password?.message} />
        <label className="flex flex-col gap-1 text-sm">
          <span className="font-medium text-gray-700">Роль</span>
          <select {...register("role")} className="rounded border border-gray-300 px-3 py-2">
            <option value="operator">Оператор</option>
            <option value="owner">Владелец</option>
          </select>
        </label>
      </div>
      {create.error && <p className="mt-3 text-sm text-red-600">{create.error.message}</p>}
      {create.isSuccess && <p className="mt-3 text-sm text-green-700">Сотрудник добавлен</p>}
      <div className="mt-4">
        <SubmitButton pending={create.isPending}>Добавить</SubmitButton>
      </div>
    </form>
  );
}
