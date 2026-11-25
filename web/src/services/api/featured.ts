import { http } from "@/services/api/request";

export type FeaturedVideoSource = "upload" | "share";
export type FeaturedCoverSource = "upload" | "share";

export type FeaturedCanvas = {
    id: string;
    title: string;
    description?: string;
    category?: string;
    videoUrl?: string;
    coverUrl?: string;
    coverAvailable?: boolean;
    canvasUrl?: string;
    // 管理员编辑用源分享链接；持久副本就绪时 canvasUrl 指向副本链接。
    sourceCanvasUrl?: string;
    videoResourceId?: string;
    videoSource?: FeaturedVideoSource | "";
    coverResourceId?: string;
    coverSource?: FeaturedCoverSource | "";
    coverError?: string;
    snapshotId?: string;
    copySnapshotId?: string;
    persistCopy?: boolean;
    copyStatus?: "none" | "ready" | "failed";
    copySavedAt?: string;
    copyError?: string;
    sourceShareStatus?: "ok" | "deleted" | "revoked" | "expired" | "copy_forbidden" | "missing";
    consentAt?: string;
    enabled: boolean;
    sortOrder: number;
    createdAt?: string;
    updatedAt?: string;
    snapshotAt?: string;
    shareEnabled?: boolean;
    allowCopy?: boolean;
    shareExpiresAt?: string;
    videoAvailable?: boolean;
    status?: "ok" | "share_unavailable" | "video_unavailable";
    statusMessage?: string;
};

export type FeaturedCanvasInput = {
    title: string;
    description: string;
    category: string;
    videoResourceId: string;
    videoSource?: FeaturedVideoSource;
    coverResourceId?: string;
    coverSource?: FeaturedCoverSource;
    shareToken: string;
    snapshotId?: string;
    enabled: boolean;
    persistCopy?: boolean;
    consentConfirmed?: boolean;
};

export type FeaturedShareAsset = {
    resourceId?: string;
    title: string;
    available: boolean;
    reason?: string;
    url?: string;
};

export type FeaturedShareVideo = FeaturedShareAsset;
export type FeaturedShareImage = FeaturedShareAsset;

export type FeaturedSharePreview = {
    status: "ok" | "invalid" | "share_disabled" | "expired" | "copy_forbidden" | "snapshot_missing";
    message?: string;
    title?: string;
    shareId?: string;
    snapshotId?: string;
    snapshotAt?: string;
    currentSnapshotId?: string;
    currentSnapshotAt?: string;
    allowCopy?: boolean;
    expiresAt?: string;
    videos?: FeaturedShareVideo[];
    images?: FeaturedShareImage[];
};

export type FeaturedSharePreviewInput = {
    shareToken: string;
    snapshotId?: string;
};

export type FeaturedReorderInput = {
    id: string;
    updatedAt: string;
};

export const getFeaturedCanvases = () => http.get<{ items: FeaturedCanvas[] }>("/public/featured");
export const getAdminFeaturedCanvases = () => http.get<{ items: FeaturedCanvas[] }>("/admin/featured");
export const previewFeaturedShare = (data: FeaturedSharePreviewInput) => http.post<FeaturedSharePreview>("/admin/featured/share-preview", data);
export const saveFeaturedCanvas = (id: string | undefined, data: FeaturedCanvasInput) => (id ? http.put<{ item: FeaturedCanvas }>(`/admin/featured/${encodeURIComponent(id)}`, data) : http.post<{ item: FeaturedCanvas }>("/admin/featured", data));
export const deleteFeaturedCanvas = (id: string) => http.delete(`/admin/featured/${encodeURIComponent(id)}`);
export const reorderFeaturedCanvases = (items: FeaturedReorderInput[]) => http.post<{ items: FeaturedCanvas[] }>("/admin/featured/reorder", { items });
export const setFeaturedCanvasEnabled = (id: string, enabled: boolean) => http.patch<{ item: FeaturedCanvas }>(`/admin/featured/${encodeURIComponent(id)}/enabled`, { enabled });
