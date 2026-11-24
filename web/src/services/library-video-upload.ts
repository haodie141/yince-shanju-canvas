import { canvasVideoAssetPreviewUrl } from "@/lib/canvas/canvas-media-preview";
import type { AssetCategory } from "@/lib/asset-category";
import { uploadMediaFile } from "@/services/file-storage";
import { useAssetStore } from "@/stores/use-asset-store";

export async function uploadLibraryVideo(file: File, options: { category?: AssetCategory; folderId?: string; tags?: string[]; onProgress?: (uploadedBytes: number, totalBytes: number) => void } = {}) {
    const uploaded = await uploadMediaFile(file, "video", options.onProgress);
    const assetId = useAssetStore.getState().addAsset({
        kind: "video",
        title: file.name.replace(/\.[^.]+$/, ""),
        category: options.category || "material",
        folderId: options.folderId,
        coverUrl: canvasVideoAssetPreviewUrl(uploaded.url, uploaded.preview?.url),
        tags: options.tags || [],
        source: "本地上传",
        metadata: { source: "manual-upload", originalFileName: file.name },
        data: {
            url: uploaded.url,
            storageKey: uploaded.storageKey,
            width: uploaded.width || 0,
            height: uploaded.height || 0,
            durationMs: uploaded.durationMs,
            hasAudio: uploaded.hasAudio,
            bytes: uploaded.bytes,
            mimeType: uploaded.mimeType,
        },
    });
    return { assetId, pendingRemoteUpload: Boolean(uploaded.pendingRemoteUpload) };
}
