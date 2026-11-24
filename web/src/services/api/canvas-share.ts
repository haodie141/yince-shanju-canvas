import type { CanvasProject } from "@/stores/canvas/use-canvas-store";
import { http } from "@/services/api/request";


export type CanvasShareStatus = {
    enabled: boolean;
    allowCopy?: boolean;
    token?: string;
    expiresAt?: string;
    createdAt?: string;
    snapshotAt?: string;
};

export type PublicCanvasShare = {
    project: CanvasProject;
    allowCopy?: boolean;
    copyIssues?: string[];
    expiresAt?: string;
    snapshotId?: string;
};

export function getCanvasShare(projectId: string) {
    return http.get<{ share: CanvasShareStatus }>(`/canvas-projects/${encodeURIComponent(projectId)}/share`);
}

export function createCanvasShare(projectId: string, params: { expiresDays: number; rotate?: boolean; allowCopy?: boolean; preserveExpiry?: boolean }) {
    return http.post<{ share: CanvasShareStatus }>(`/canvas-projects/${encodeURIComponent(projectId)}/share`, params);
}

export function setCanvasShareCopyPermission(projectId: string, allowCopy: boolean) {
    return http.patch<{ share: CanvasShareStatus }>(`/canvas-projects/${encodeURIComponent(projectId)}/share/copy-permission`, { allowCopy });
}

export function deleteCanvasShare(projectId: string) {
    return http.delete<{ id: string }>(`/canvas-projects/${encodeURIComponent(projectId)}/share`);
}

export function getPublicCanvasShare(token: string, snapshot?: string) {
    return http.get<PublicCanvasShare>(`/public/canvas-shares/${encodeURIComponent(token)}`, { params: snapshot ? { snapshot } : undefined });
}

export function cloneCanvasShare(token: string, snapshotId: string, requestId: string, signal?: AbortSignal) {
    return http.post<{ canvasId: string }>(`/public/canvas-shares/${encodeURIComponent(token)}/clone`, { snapshotId, requestId }, { signal });
}
