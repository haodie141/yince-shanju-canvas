import { http } from "@/services/api/request";

export type PromptModerationSetting = {
    enabled: boolean;
    channelId: string;
    modelKey: string;
    policy: string;
    strictness: "standard" | "strict";
    maxTokens: number;
    timeoutSeconds: number;
    blockedTerms: string[];
    reviewEnabled: boolean;
    reviewChannelId: string;
    reviewModelKey: string;
    reviewPolicy: string;
};

export const getPromptModeration = () => http.get<{ setting: PromptModerationSetting }>("/admin/settings/prompt-moderation");
export const savePromptModeration = (setting: PromptModerationSetting) => http.patch<{ setting: PromptModerationSetting }>("/admin/settings/prompt-moderation", setting);
export type PromptModerationDetails = { initial?: { action: string; reason?: string }; review?: { action: string; reason?: string }; final?: { action: string; reason?: string }; initialModel?: string; reviewModel?: string; initialDurationMs?: number; reviewDurationMs?: number; stage?: string; failure?: string };
export const testPromptModeration = (setting: PromptModerationSetting, prompt: string) => http.post<{ action: string; reason?: string; details?: PromptModerationDetails }>("/admin/settings/prompt-moderation/test", { setting, prompt });

export type PromptModerationLog = { id: string; userId: string; username: string; displayName: string; capability: string; errorCode: string; error: string; responseBody?: string; createdAt: string };
export const getPromptModerationLogs = (page: number) => http.get<{ logs: PromptModerationLog[]; total: number; page: number; pageSize: number }>("/admin/settings/prompt-moderation/logs", { params: { page, pageSize: 30 } });
