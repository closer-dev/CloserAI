import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api, ApiError, post } from "./client";

export type Role = "owner" | "operator";

export interface User {
  id: string;
  tenant_id: string;
  email: string;
  name: string;
  role: Role;
  email_verified: boolean;
  created_at: string;
}

export interface Tenant {
  id: string;
  name: string;
  plan: string;
  created_at: string;
}

export interface LoginInput {
  email: string;
  password: string;
}

export interface RegisterInput extends LoginInput {
  company_name: string;
  name: string;
}

export interface CreateUserInput extends LoginInput {
  name: string;
  role: Role;
}

const meKey = ["me"];

// useMe возвращает текущего пользователя или null, если он не вошёл.
export function useMe() {
  return useQuery({
    queryKey: meKey,
    queryFn: async () => {
      try {
        return await api<User>("/me");
      } catch (e) {
        if (e instanceof ApiError && e.status === 401) return null;
        throw e;
      }
    },
    staleTime: 5 * 60_000,
  });
}

export function useTenant() {
  return useQuery({ queryKey: ["tenant"], queryFn: () => api<Tenant>("/tenant") });
}

function useSessionMutation<I>(path: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: I) => post<User>(path, input),
    onSuccess: (user) => {
      qc.clear();
      qc.setQueryData(meKey, user);
    },
  });
}

export const useLogin = () => useSessionMutation<LoginInput>("/auth/login");
export const useRegister = () => useSessionMutation<RegisterInput>("/auth/register");

export function useLogout() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: () => post<void>("/auth/logout"),
    onSuccess: () => {
      qc.clear();
      qc.setQueryData(meKey, null);
    },
  });
}

export function useVerifyEmail() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (code: string) => post<User>("/auth/verify", { code }),
    onSuccess: (user) => qc.setQueryData(meKey, user),
  });
}

export function useResendCode() {
  return useMutation({ mutationFn: () => post<void>("/auth/resend") });
}

export function useUsers() {
  return useQuery({ queryKey: ["users"], queryFn: () => api<User[]>("/users") });
}

export function useCreateUser() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: CreateUserInput) => post<User>("/users", input),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["users"] }),
  });
}
