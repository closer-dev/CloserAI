import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api, del, patch, post } from "./client";

export interface Bot {
  id: string;
  tg_bot_id: number;
  username: string;
  link: string;
  system_prompt: string;
  greeting: string;
  handoff_threshold: number;
  is_active: boolean;
  created_at: string;
}

export interface BotSettings {
  system_prompt?: string;
  greeting?: string;
  handoff_threshold?: number;
}

export interface ConnectBotInput extends BotSettings {
  token: string;
}

export interface UpdateBotInput extends BotSettings {
  token?: string;
  is_active?: boolean;
}

const botsKey = ["bots"];

export function useBots() {
  return useQuery({ queryKey: botsKey, queryFn: () => api<Bot[]>("/bots") });
}

export function useConnectBot() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: ConnectBotInput) => post<Bot>("/bots", input),
    onSuccess: () => qc.invalidateQueries({ queryKey: botsKey }),
  });
}

export function useUpdateBot(id: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: UpdateBotInput) => patch<Bot>(`/bots/${id}`, input),
    onSuccess: () => qc.invalidateQueries({ queryKey: botsKey }),
  });
}

export function useDeleteBot(id: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: () => del(`/bots/${id}`),
    onSuccess: () => qc.invalidateQueries({ queryKey: botsKey }),
  });
}
