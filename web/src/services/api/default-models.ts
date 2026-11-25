import { http } from "@/services/api/request";

/** 全站默认模型，值为前端模型选项编码 channelId::modelKey。 */
export type DefaultModels = {
    imageModel: string;
    videoModel: string;
    agentTextModel: string;
    optimizerModel: string;
    audioModel: string;
};

export type PublicDefaultModels = DefaultModels & { configured: boolean; updatedAt?: string };

/** 登录用户读取；新用户首次登录用它生成初始配置。 */
export const getDefaultModels = () => http.get<{ defaults: PublicDefaultModels }>("/settings/default-models");
export const getAdminDefaultModels = () => http.get<{ defaults: PublicDefaultModels }>("/admin/settings/default-models");
export const updateAdminDefaultModels = (data: DefaultModels) => http.patch<{ defaults: PublicDefaultModels }>("/admin/settings/default-models", data);
