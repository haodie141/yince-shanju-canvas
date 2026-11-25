import { App, Button, Checkbox, Form, Input, Modal, Popconfirm, Segmented, Slider, Space, Switch, Table, Tooltip } from "antd";
import type { ColumnsType } from "antd/es/table";
import { ArrowDown, ArrowUp, Check, ExternalLink, GripVertical, RefreshCw, Search, Upload } from "lucide-react";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { AdminPageFrame } from "../components/admin-shell";
import {
    deleteFeaturedCanvas,
    getAdminFeaturedCanvases,
    previewFeaturedShare,
    reorderFeaturedCanvases,
    saveFeaturedCanvas,
    setFeaturedCanvasEnabled,
    type FeaturedCanvas,
    type FeaturedCoverSource,
    type FeaturedSharePreview,
    type FeaturedVideoSource,
} from "@/services/api/featured";
import { resourceFileUrl, uploadResourceFile } from "@/services/api/resources";
import "./featured-page.css";

type Fields = {
    title: string;
    description: string;
    category: string;
    shareLink: string;
    enabled: boolean;
    persistCopy: boolean;
    consentConfirmed: boolean;
};

type SelectedVideo = { resourceId: string; source: FeaturedVideoSource | "" };
type SelectedCover = { resourceId: string; source: FeaturedCoverSource | "" };

const defaults: Fields = { title: "", description: "", category: "", shareLink: "", enabled: true, persistCopy: true, consentConfirmed: false };

/** 只解析分享路径；不限制域名，兼容 main.shanju.media 等旧域名。令牌由服务端校验。 */
function shareToken(value: string) {
    const input = value.trim();
    if (/^[\w-]{32,128}$/.test(input)) return input;
    try {
        const url = new URL(input, window.location.origin);
        if (url.protocol !== "http:" && url.protocol !== "https:") return "";
        return url.pathname.match(/^\/share\/canvas\/([\w-]{32,128})\/?$/)?.[1] || "";
    } catch {
        return "";
    }
}

function formatTime(value?: string) {
    if (!value) return "—";
    const date = new Date(value);
    return Number.isNaN(date.getTime()) ? "—" : date.toLocaleString("zh-CN", { hour12: false });
}

/** 视频截帧时间显示，统一保留一位小数。 */
function formatFrameTime(value: number) {
    if (!Number.isFinite(value) || value < 0) return "0.0s";
    return `${value.toFixed(1)}s`;
}

const previewStatusText: Record<FeaturedSharePreview["status"], string> = {
    ok: "分享有效，可以预览和添加",
    invalid: "分享链接无效或已被删除",
    share_disabled: "分享已被分享者关闭",
    expired: "分享链接已过期",
    copy_forbidden: "分享者已禁止保存副本，不能添加为精选",
    snapshot_missing: "所选分享版本不可用",
};

function previewTone(status?: FeaturedSharePreview["status"]) {
    if (status === "ok") return "success" as const;
    if (status === "snapshot_missing" || status === "copy_forbidden") return "warning" as const;
    return "error" as const;
}

export default function FeaturedPage() {
    const { message } = App.useApp();
    const [items, setItems] = useState<FeaturedCanvas[]>([]);
    const [loading, setLoading] = useState(false);
    const [saving, setSaving] = useState(false);
    const [uploading, setUploading] = useState(false);
    const [editing, setEditing] = useState<FeaturedCanvas | null>(null);
    const [open, setOpen] = useState(false);
    const [search, setSearch] = useState("");
    const [draggingId, setDraggingId] = useState("");
    const [dropTargetId, setDropTargetId] = useState("");
    const [reordering, setReordering] = useState(false);
    const [togglingId, setTogglingId] = useState("");
    const [preview, setPreview] = useState<FeaturedSharePreview | null>(null);
    const [previewLoading, setPreviewLoading] = useState(false);
    const [videoMode, setVideoMode] = useState<"share" | "upload">("share");
    const [selectedVideo, setSelectedVideo] = useState<SelectedVideo>({ resourceId: "", source: "" });
    const [selectedCover, setSelectedCover] = useState<SelectedCover>({ resourceId: "", source: "" });
    const [coverUploading, setCoverUploading] = useState(false);
    const [coverFrameLoading, setCoverFrameLoading] = useState(false);
    const [frameTime, setFrameTime] = useState(0);
    const [frameDuration, setFrameDuration] = useState(0);
    const [selectedSnapshotId, setSelectedSnapshotId] = useState("");
    const [copyOnly, setCopyOnly] = useState(false);
    const [form] = Form.useForm<Fields>();
    const persistCopyValue = Form.useWatch("persistCopy", form);
    const persistCopyEnabled = persistCopyValue ?? true;
    const copyOnlyRef = useRef(false);
    const titleDirtyRef = useRef(false);
    const videoModeRef = useRef<"share" | "upload">("share");
    const videoModeTouchedRef = useRef(false);
    const selectedVideoRef = useRef<SelectedVideo>({ resourceId: "", source: "" });
    const selectedCoverRef = useRef<SelectedCover>({ resourceId: "", source: "" });
    const selectedSnapshotRef = useRef("");
    const previewRef = useRef<FeaturedSharePreview | null>(null);
    const previewRequestRef = useRef(0);
    const lastTokenRef = useRef("");
    const uploadInputRef = useRef<HTMLInputElement>(null);
    const coverInputRef = useRef<HTMLInputElement>(null);
    const previewVideoRef = useRef<HTMLVideoElement>(null);
    const editingRef = useRef<FeaturedCanvas | null>(null);

    videoModeRef.current = videoMode;
    selectedVideoRef.current = selectedVideo;
    selectedCoverRef.current = selectedCover;
    selectedSnapshotRef.current = selectedSnapshotId;
    previewRef.current = preview;
    editingRef.current = editing;

    const refresh = useCallback(async () => {
        setLoading(true);
        try {
            const response = await getAdminFeaturedCanvases();
            setItems(response.items || []);
        } catch (error) {
            message.error(error instanceof Error ? error.message : "读取精选失败");
        } finally {
            setLoading(false);
        }
    }, [message]);

    useEffect(() => {
        void refresh();
    }, [refresh]);

    const visibleItems = useMemo(() => {
        const keyword = search.trim().toLowerCase();
        if (!keyword) return items;
        return items.filter((item) => [item.title, item.category || "", item.description || ""].join(" ").toLowerCase().includes(keyword));
    }, [items, search]);
    const canDrag = !search.trim() && !reordering && !togglingId && visibleItems.length > 1;

    const setChosenVideo = (video: SelectedVideo) => {
        selectedVideoRef.current = video;
        setSelectedVideo(video);
    };

    const setChosenCover = (cover: SelectedCover) => {
        selectedCoverRef.current = cover;
        setSelectedCover(cover);
    };

    const setMode = (next: "share" | "upload") => {
        videoModeTouchedRef.current = true;
        videoModeRef.current = next;
        setVideoMode(next);
        const current = selectedVideoRef.current;
        if (next === "upload") {
            // 分享视频不能改称“上传视频”；切到上传时必须重新选择文件。
            setChosenVideo(current.source === "share" ? { resourceId: "", source: "upload" } : { resourceId: current.resourceId, source: "upload" });
            return;
        }
        if (current.source === "share" && current.resourceId) return;
        const first = previewRef.current?.videos?.find((video) => video.available && video.resourceId);
        setChosenVideo(first?.resourceId ? { resourceId: first.resourceId, source: "share" } : { resourceId: "", source: "share" });
    };

    const applyPreview = (result: FeaturedSharePreview, requestedToken: string) => {
        setPreview(result);
        previewRef.current = result;
        if (result.status === "ok" || result.status === "copy_forbidden") {
            selectedSnapshotRef.current = result.snapshotId || "";
            setSelectedSnapshotId(result.snapshotId || "");
            if (!titleDirtyRef.current && result.title) form.setFieldValue("title", result.title);
        } else {
            selectedSnapshotRef.current = "";
            setSelectedSnapshotId("");
        }
        if (videoModeRef.current === "share") {
            const current = selectedVideoRef.current;
            const available = result.videos?.filter((video) => video.available && video.resourceId) || [];
            const currentStillAvailable = current.source === "share" && available.some((video) => video.resourceId === current.resourceId);
            if (!currentStillAvailable) {
                if (available[0]?.resourceId) setChosenVideo({ resourceId: available[0].resourceId, source: "share" });
                else if (current.source === "share") setChosenVideo({ resourceId: "", source: "share" });
            }
        }
        lastTokenRef.current = requestedToken;
    };

    const loadPreview = async (options?: { snapshotId?: string }) => {
        const link = String(form.getFieldValue("shareLink") || "");
        const token = shareToken(link);
        if (!token) {
            setPreview(null);
            previewRef.current = null;
            if (link.trim()) message.warning("请粘贴本站画布分享链接");
            return;
        }
        const sameShare = token === lastTokenRef.current;
        const requestedSnapshot = options?.snapshotId !== undefined ? options.snapshotId : sameShare ? selectedSnapshotRef.current : "";
        const requestId = ++previewRequestRef.current;
        setPreviewLoading(true);
        try {
            const result = await previewFeaturedShare({ shareToken: token, snapshotId: requestedSnapshot || undefined });
            if (requestId !== previewRequestRef.current) return;
            lastTokenRef.current = token;
            applyPreview(result, token);
        } catch (error) {
            if (requestId !== previewRequestRef.current) return;
            setPreview(null);
            previewRef.current = null;
            message.error(error instanceof Error ? error.message : "读取分享信息失败");
        } finally {
            if (requestId === previewRequestRef.current) setPreviewLoading(false);
        }
    };

    const openEditor = (item?: FeaturedCanvas) => {
        const nextCopyOnly = Boolean(item?.persistCopy && item?.copyStatus === "ready" && item?.sourceShareStatus && item.sourceShareStatus !== "ok");
        copyOnlyRef.current = nextCopyOnly;
        setCopyOnly(nextCopyOnly);
        setEditing(item || null);
        editingRef.current = item || null;
        setOpen(true);
        setPreview(null);
        previewRef.current = null;
        setPreviewLoading(false);
        titleDirtyRef.current = Boolean(item);
        videoModeTouchedRef.current = false;
        const initialMode: "share" | "upload" = item ? (item.videoSource === "share" ? "share" : "upload") : "share";
        videoModeRef.current = initialMode;
        setVideoMode(initialMode);
        const initialVideo: SelectedVideo = { resourceId: item?.videoResourceId || "", source: item?.videoSource || "" };
        selectedVideoRef.current = initialVideo;
        setSelectedVideo(initialVideo);
        const initialCover: SelectedCover = { resourceId: item?.coverResourceId || "", source: item?.coverSource || "" };
        selectedCoverRef.current = initialCover;
        setSelectedCover(initialCover);
        const initialSnapshot = item?.snapshotId || "";
        selectedSnapshotRef.current = initialSnapshot;
        setSelectedSnapshotId(initialSnapshot);
        // 持久副本就绪时 canvasUrl 是副本分享链接，不能用于识别源分享版本。
        const shareURL = nextCopyOnly ? "" : (item?.sourceCanvasUrl || item?.canvasUrl || "");
        lastTokenRef.current = shareURL ? shareToken(shareURL) : "";
        form.resetFields();
        form.setFieldsValue(
            item
                ? {
                      title: item.title,
                      description: item.description || "",
                      category: item.category || "",
                      shareLink: shareURL,
                      enabled: item.enabled,
                      persistCopy: item.persistCopy ?? false,
                      consentConfirmed: Boolean(item.consentAt),
                  }
                : defaults,
        );
        if (shareURL) {
            void loadPreview({ snapshotId: item?.snapshotId || "" });
        }
    };

    const closeEditor = () => {
        if (uploading || saving) return;
        setOpen(false);
        setEditing(null);
        editingRef.current = null;
        setPreview(null);
        previewRef.current = null;
    };

    const persistOrder = async (next: FeaturedCanvas[]) => {
        setItems(next);
        setReordering(true);
        try {
            const response = await reorderFeaturedCanvases(next.map((item) => ({ id: item.id, updatedAt: item.updatedAt || new Date().toISOString() })));
            setItems(response.items || []);
            message.success("已保存首页展示顺序");
        } catch (error) {
            message.error(error instanceof Error ? error.message : "保存排序失败");
            await refresh();
        } finally {
            setReordering(false);
        }
    };

    const moveItem = (from: number, to: number) => {
        if (from === to || from < 0 || to < 0 || to >= visibleItems.length || !canDrag) return;
        const next = [...visibleItems];
        const [moved] = next.splice(from, 1);
        next.splice(to, 0, moved);
        void persistOrder(next);
    };

    const handleDrop = (targetId: string) => {
        const sourceId = draggingId;
        setDraggingId("");
        setDropTargetId("");
        if (!sourceId || sourceId === targetId || !canDrag) return;
        const from = visibleItems.findIndex((item) => item.id === sourceId);
        const to = visibleItems.findIndex((item) => item.id === targetId);
        moveItem(from, to);
    };

    const toggleEnabled = async (item: FeaturedCanvas, enabled: boolean) => {
        setTogglingId(item.id);
        setItems((current) => current.map((entry) => (entry.id === item.id ? { ...entry, enabled } : entry)));
        try {
            const response = await setFeaturedCanvasEnabled(item.id, enabled);
            setItems((current) => current.map((entry) => (entry.id === item.id ? response.item : entry)));
            message.success(enabled ? "已设为首页展示" : "已从首页隐藏");
        } catch (error) {
            setItems((current) => current.map((entry) => (entry.id === item.id ? item : entry)));
            message.error(error instanceof Error ? error.message : "切换展示状态失败");
        } finally {
            setTogglingId("");
        }
    };

    const submit = async () => {
        let values: Fields;
        try {
            values = await form.validateFields();
        } catch {
            return;
        }
        const existing = editingRef.current;
        if (values.persistCopy && !values.consentConfirmed && !existing?.consentAt) {
            message.error("请确认已获得作者同意将作品用于精选并保存平台副本");
            return;
        }
        if (copyOnlyRef.current && existing) {
            if (!existing.videoResourceId) {
                message.error("当前精选缺少可用视频，请让作者重新分享后再编辑");
                return;
            }
            const source = existing.videoSource === "share" ? "share" : existing.videoSource === "upload" ? "upload" : undefined;
            setSaving(true);
            try {
                await saveFeaturedCanvas(existing.id, {
                    title: values.title,
                    description: values.description || "",
                    category: values.category || "",
                    videoResourceId: existing.videoResourceId,
                    videoSource: source,
                    coverResourceId: selectedCoverRef.current.resourceId,
                    coverSource: selectedCoverRef.current.source || undefined,
                    shareToken: "",
                    enabled: values.enabled,
                    persistCopy: values.persistCopy,
                    consentConfirmed: values.consentConfirmed,
                });
                message.success("已保存精选");
                setOpen(false);
                setEditing(null);
                await refresh();
            } catch (error) {
                message.error(error instanceof Error ? error.message : "保存精选失败");
            } finally {
                setSaving(false);
            }
            return;
        }
        const token = shareToken(values.shareLink);
        if (!token) {
            message.error("请粘贴本站画布分享链接");
            return;
        }
        const currentPreview = previewRef.current;
        if (!currentPreview || currentPreview.status !== "ok") {
            message.error(currentPreview?.message || "请先识别有效的分享链接并确认可预览");
            return;
        }
        const selected = selectedVideoRef.current;
        if (!selected.resourceId) {
            message.error(videoModeRef.current === "share" ? "请选择一个分享画布内的视频" : "请先上传视频");
            return;
        }
        let source: FeaturedVideoSource | undefined;
        if (existing && selected.resourceId === existing.videoResourceId && !videoModeTouchedRef.current) {
            source = existing.videoSource || undefined;
        } else {
            source = selected.source === "share" ? "share" : selected.source === "upload" ? "upload" : undefined;
        }
        if (!source) {
            message.error("请重新选择视频来源");
            return;
        }
        setSaving(true);
        try {
            await saveFeaturedCanvas(existing?.id, {
                title: values.title,
                description: values.description || "",
                category: values.category || "",
                videoResourceId: selected.resourceId,
                videoSource: source,
                coverResourceId: selectedCoverRef.current.resourceId,
                coverSource: selectedCoverRef.current.source || undefined,
                shareToken: token,
                snapshotId: currentPreview.snapshotId || selectedSnapshotRef.current || undefined,
                enabled: values.enabled,
                persistCopy: values.persistCopy,
                consentConfirmed: values.consentConfirmed,
            });
            message.success("已保存精选");
            setOpen(false);
            setEditing(null);
            await refresh();
        } catch (error) {
            message.error(error instanceof Error ? error.message : "保存精选失败");
        } finally {
            setSaving(false);
        }
    };

    const uploadVideo = async (file: File) => {
        setUploading(true);
        try {
            const resource = await uploadResourceFile(file, "video", { fileName: file.name });
            setChosenVideo({ resourceId: resource.id, source: "upload" });
            videoModeTouchedRef.current = true;
            videoModeRef.current = "upload";
            setVideoMode("upload");
            message.success("视频上传完成");
        } catch (error) {
            message.error(error instanceof Error ? error.message : "上传失败");
        } finally {
            setUploading(false);
        }
    };

    const uploadCover = async (file: Blob, successText = "封面上传完成") => {
        setCoverUploading(true);
        try {
            const resource = await uploadResourceFile(file, "image", { fileName: file instanceof File ? file.name : "featured-cover.png" });
            setChosenCover({ resourceId: resource.id, source: "upload" });
            message.success(successText);
        } catch (error) {
            message.error(error instanceof Error ? error.message : "封面上传失败");
        } finally {
            setCoverUploading(false);
        }
    };

    /** 把预览视频 seek 到指定时间并等待画面就绪，供截帧使用。 */
    const seekPreviewVideo = (video: HTMLVideoElement, time: number) =>
        new Promise<void>((resolve) => {
            const durationLimit = Number.isFinite(video.duration) && video.duration > 0 ? Math.max(0, video.duration - 0.05) : time;
            const target = Math.max(0, Math.min(time, durationLimit));
            if (Math.abs(video.currentTime - target) < 0.05) {
                resolve();
                return;
            }
            let settled = false;
            const done = () => {
                if (settled) return;
                settled = true;
                video.removeEventListener("seeked", done);
                resolve();
            };
            video.addEventListener("seeked", done, { once: true });
            video.currentTime = target;
            // 视频未缓冲时 seeked 可能很久不触发，超时后仍尝试用当前画面。
            window.setTimeout(done, 2000);
        });

    const captureVideoFrame = async () => {
        const video = previewVideoRef.current;
        if (!video || !video.videoWidth || !video.videoHeight) {
            message.error("视频画面尚未加载，请先展开预览或稍等再试");
            return;
        }
        setCoverFrameLoading(true);
        try {
            await seekPreviewVideo(video, frameTime);
            const canvas = document.createElement("canvas");
            canvas.width = video.videoWidth;
            canvas.height = video.videoHeight;
            const context = canvas.getContext("2d");
            if (!context) throw new Error("浏览器不支持截取视频画面");
            context.drawImage(video, 0, 0, canvas.width, canvas.height);
            const blob = await new Promise<Blob | null>((resolve) => canvas.toBlob(resolve, "image/png"));
            if (!blob) throw new Error("截取视频画面失败，请重试");
            await uploadCover(new File([blob], `featured-cover-${frameTime.toFixed(1)}s.png`, { type: "image/png" }), `已截取 ${formatFrameTime(frameTime)} 画面并设为主页封面`);
        } catch (error) {
            message.error(error instanceof Error ? error.message : "截取视频画面失败");
        } finally {
            setCoverFrameLoading(false);
        }
    };

    const openSharePage = () => {
        const token = shareToken(String(form.getFieldValue("shareLink") || ""));
        if (!token || !previewRef.current?.snapshotId) return;
        window.open(`/share/canvas/${encodeURIComponent(token)}?snapshot=${encodeURIComponent(previewRef.current.snapshotId)}`, "_blank", "noopener,noreferrer");
    };

    const previewVideoURL = useMemo(() => {
        const id = selectedVideo.resourceId;
        if (!id) return "";
        const existing = editing;
        if (existing && id === existing.videoResourceId && existing.videoUrl) return existing.videoUrl;
        const shareVideo = preview?.videos?.find((video) => video.resourceId === id);
        if (shareVideo?.url) return shareVideo.url;
        return resourceFileUrl(id);
    }, [editing, preview, selectedVideo]);

    const selectedVideoTitle = useMemo(() => {
        if (!selectedVideo.resourceId) return "";
        const shareVideo = preview?.videos?.find((video) => video.resourceId === selectedVideo.resourceId);
        if (shareVideo) return shareVideo.title;
        if (editing?.videoResourceId === selectedVideo.resourceId) return editing.title || "当前精选视频";
        return "已选视频";
    }, [editing, preview, selectedVideo]);

    const selectedCoverURL = useMemo(() => {
        const id = selectedCover.resourceId;
        if (!id) return "";
        if (editing && id === editing.coverResourceId && editing.coverUrl) return editing.coverUrl;
        const shareImage = preview?.images?.find((image) => image.resourceId === id);
        if (shareImage?.url) return shareImage.url;
        return resourceFileUrl(id);
    }, [editing, preview, selectedCover]);

    const selectedCoverTitle = useMemo(() => {
        if (!selectedCover.resourceId) return "";
        if (editing?.coverResourceId === selectedCover.resourceId && editing.coverResourceId) return editing.title || "当前封面";
        return preview?.images?.find((image) => image.resourceId === selectedCover.resourceId)?.title || "已选封面";
    }, [editing, preview, selectedCover]);

    const coverOutdated = Boolean(selectedCover.source === "share" && preview?.images?.length && !preview.images.some((image) => image.resourceId === selectedCover.resourceId && image.available));

    const selectedShareVideo = preview?.videos?.find((video) => video.resourceId === selectedVideo.resourceId);
    const snapshotOutdated = Boolean(preview?.snapshotId && preview.currentSnapshotId && preview.snapshotId !== preview.currentSnapshotId);
    const currentTone = previewTone(preview?.status);
    const copySourceLabel =
        editing?.sourceShareStatus === "deleted"
            ? "来源画布已删除"
            : editing?.sourceShareStatus === "revoked"
              ? "作者已撤回分享"
              : editing?.sourceShareStatus === "expired"
                ? "来源分享已过期"
                : editing?.sourceShareStatus === "copy_forbidden"
                  ? "来源分享已禁止复制"
                  : "来源分享不可用";

    const columns: ColumnsType<FeaturedCanvas> = [
        {
            title: "排序",
            width: 108,
            render: (_, row, index) => (
                <div className={`featured-admin-sort-cell${canDrag ? "" : " is-disabled"}`}>
                    <GripVertical className="featured-admin-grip" aria-hidden="true" />
                    <span className="featured-admin-order">{index + 1}</span>
                    <Tooltip title={canDrag ? "上移一位" : "搜索状态下不能调整顺序"}>
                        <Button type="text" size="small" aria-label={`上移精选：${row.title}`} disabled={!canDrag || index === 0} icon={<ArrowUp className="size-3.5" />} onClick={() => moveItem(index, index - 1)} />
                    </Tooltip>
                    <Tooltip title={canDrag ? "下移一位" : "搜索状态下不能调整顺序"}>
                        <Button type="text" size="small" aria-label={`下移精选：${row.title}`} disabled={!canDrag || index === visibleItems.length - 1} icon={<ArrowDown className="size-3.5" />} onClick={() => moveItem(index, index + 1)} />
                    </Tooltip>
                </div>
            ),
        },
        {
            title: "标题",
            dataIndex: "title",
            render: (_, row) => (
                <div className="featured-admin-title-cell">
                    <span className="featured-admin-title">{row.title}</span>
                    <span className="featured-admin-subtitle">
                        {row.videoSource === "share" ? "分享视频" : row.videoSource === "upload" ? "上传视频" : "旧版视频"} · {row.category || "未分类"}
                    </span>
                </div>
            ),
        },
        {
            title: "持久副本",
            width: 190,
            render: (_, row) => {
                if (!row.persistCopy) {
                    return <span className="featured-admin-copy">未保存</span>;
                }
                if (row.copyStatus === "ready") {
                    return (
                        <span className="featured-admin-copy" data-tone="ok">
                            已持久保存
                            <small>{formatTime(row.copySavedAt)}</small>
                        </span>
                    );
                }
                if (row.copyStatus === "failed") {
                    return (
                        <span className="featured-admin-copy" data-tone="error">
                            保存失败
                            <small>{row.copyError || "请重新保存副本"}</small>
                        </span>
                    );
                }
                return <span className="featured-admin-copy">未完成</span>;
            },
        },
        {
            title: (
                <Tooltip title="来源分享的授权状态；作者主动关闭或禁止复制时会下架，原画布误删时持久副本继续可用">
                    <span>来源分享</span>
                </Tooltip>
            ),
            width: 190,
            render: (_, row) => {
                const source = row.sourceShareStatus;
                if (source === "revoked") {
                    return (
                        <span className="featured-admin-source" data-tone="error">
                            授权已撤回
                        </span>
                    );
                }
                if (source === "expired") {
                    return (
                        <span className="featured-admin-source" data-tone="error">
                            分享已过期
                        </span>
                    );
                }
                if (source === "copy_forbidden") {
                    return (
                        <span className="featured-admin-source" data-tone="error">
                            禁止复制
                        </span>
                    );
                }
                if (source === "deleted") {
                    return (
                        <span className="featured-admin-source" data-tone={row.copyStatus === "ready" ? "warning" : "error"}>
                            {row.copyStatus === "ready" ? "原画布已删除 · 副本可用" : "原画布已删除"}
                        </span>
                    );
                }
                if (source === "missing") {
                    return (
                        <span className="featured-admin-source" data-tone="error">
                            来源不可用
                        </span>
                    );
                }
                return (
                    <span className="featured-admin-source" data-tone="ok">
                        分享正常
                    </span>
                );
            },
        },
        {
            title: (
                <Tooltip title="分享者最后一次更新分享版本的时间；原画布继续修改不会改变这里">
                    <span>分享版本更新时间</span>
                </Tooltip>
            ),
            width: 170,
            render: (_, row) => (
                <span className="featured-admin-time">
                    {formatTime(row.snapshotAt)}
                    <small>分享快照时间</small>
                </span>
            ),
        },
        {
            title: (
                <Tooltip title="精选记录最后一次保存的时间（包含标题、视频、排序和展示开关）">
                    <span>精选更新时间</span>
                </Tooltip>
            ),
            width: 160,
            render: (_, row) => (
                <span className="featured-admin-time">
                    {formatTime(row.updatedAt)}
                    <small>精选记录时间</small>
                </span>
            ),
        },
        {
            title: "状态",
            width: 190,
            render: (_, row) => {
                const healthy = row.status === "ok";
                return (
                    <div className="featured-admin-status" data-tone={healthy ? "ok" : "error"}>
                        <span className="featured-admin-status-main">
                            <Switch checked={row.enabled} loading={togglingId === row.id} onChange={(checked) => void toggleEnabled(row, checked)} aria-label={`${row.enabled ? "隐藏" : "展示"}：${row.title}`} />
                            <span className="featured-admin-status-text">{row.enabled ? "展示中" : "已隐藏"}</span>
                        </span>
                        {!healthy ? <span className="featured-admin-status-message">{row.statusMessage || "关联分享不可用，请重新关联"}</span> : null}
                    </div>
                );
            },
        },
        {
            title: "操作",
            width: 160,
            render: (_, row) => (
                <Space size="small">
                    <Button onClick={() => openEditor(row)}>编辑</Button>
                    <Popconfirm
                        title="确定移除这条精选？"
                        description="只移除首页展示记录，不会删除原画布或分享。"
                        okText="移除"
                        cancelText="取消"
                        okButtonProps={{ danger: true }}
                        onConfirm={async () => {
                            try {
                                await deleteFeaturedCanvas(row.id);
                                message.success("已移除精选");
                                await refresh();
                            } catch (error) {
                                message.error(error instanceof Error ? error.message : "移除失败");
                            }
                        }}
                    >
                        <Button danger>移除</Button>
                    </Popconfirm>
                </Space>
            ),
        },
    ];

    return (
        <AdminPageFrame title="创作精选" description="管理首页展示的视频和对应的画布模板" scroll>
            <div className="p-4">
                <div className="featured-admin-toolbar">
                    <div className="featured-admin-toolbar-main">
                        <p>粘贴用户提供的画布分享链接并选择视频，即可发布精选。画布须允许保存副本；精选固定展示关联时的分享版本。</p>
                    </div>
                    <div className="featured-admin-toolbar-actions">
                        <Input allowClear prefix={<Search className="size-4" />} placeholder="按标题、分类或简介搜索" aria-label="搜索创作精选" value={search} onChange={(event) => setSearch(event.target.value)} className="featured-admin-search" />
                        <span className="featured-admin-count" role="status">
                            共 {items.length} 条{search.trim() ? ` · 匹配 ${visibleItems.length} 条` : ""}
                        </span>
                        <Button type="primary" onClick={() => openEditor()}>
                            添加精选
                        </Button>
                    </div>
                </div>
                {search.trim() ? <p className="featured-admin-note">搜索时不调整顺序；清空搜索后可拖动排序。排序会立即保存并同步到首页。</p> : null}
                {items.length > 1 ? <p className="featured-admin-note">精选最多 200 条，列表一次展示全部，排序始终作用于全局顺序。拖动左侧排序图标或使用上下箭头调整；多人同时排序时，后提交者会收到冲突提示并需要刷新。</p> : null}
                <Table<FeaturedCanvas>
                    rowKey="id"
                    className="admin-featured-table"
                    loading={loading}
                    dataSource={visibleItems}
                    pagination={false}
                    columns={columns}
                    scroll={{ x: 900, y: 560 }}
                    locale={{
                        emptyText: <span className="featured-admin-empty">{search.trim() ? `没有找到与“${search.trim()}”相关的精选` : "还没有精选作品，点击右上角添加。"}</span>,
                    }}
                    rowClassName={(row) => (row.id === draggingId ? "is-dragging" : row.id === dropTargetId ? "is-drop-target" : "")}
                    onRow={(row) => ({
                        draggable: canDrag,
                        onDragStart: (event) => {
                            setDraggingId(row.id);
                            event.dataTransfer.effectAllowed = "move";
                            event.dataTransfer.setData("text/plain", row.id);
                        },
                        onDragEnd: () => {
                            setDraggingId("");
                            setDropTargetId("");
                        },
                        onDragOver: (event) => {
                            if (!canDrag) return;
                            event.preventDefault();
                            setDropTargetId(row.id);
                        },
                        onDragLeave: () => setDropTargetId(""),
                        onDrop: (event) => {
                            event.preventDefault();
                            handleDrop(row.id);
                        },
                    })}
                />
            </div>
            <Modal
                title={editing ? "编辑精选" : "添加精选"}
                open={open}
                onCancel={closeEditor}
                onOk={() => void submit()}
                confirmLoading={saving}
                closable={!uploading && !saving}
                maskClosable={!uploading && !saving}
                okText="保存"
                cancelText="取消"
                okButtonProps={{ disabled: uploading || coverUploading }}
                destroyOnClose
                width={1080}
                style={{ top: 24 }}
                styles={{ body: { maxHeight: "calc(100vh - 200px)", overflowY: "auto" } }}
            >
                <Form form={form} layout="vertical" initialValues={defaults} className="featured-editor-form">
                    <div className="featured-editor-grid">
                        <div className="featured-editor-column">
                            <Form.Item label="标题" name="title" rules={[{ required: true, message: "请输入标题" }, { max: 160 }]} extra="粘贴分享链接后会自动识别画布标题；识别结果返回晚于手动修改时不会覆盖你填写的标题。">
                                <Input
                                    onChange={() => {
                                        // 同步标记，避免迟到的分享识别结果覆盖刚输入的内容。
                                        titleDirtyRef.current = true;
                                    }}
                                    placeholder="首页展示的标题"
                                />
                            </Form.Item>
                            <Form.Item label="分类" name="category" rules={[{ max: 80 }]}>
                                <Input placeholder="例如短剧、广告、教程" />
                            </Form.Item>
                            <Form.Item label="作品介绍" name="description" rules={[{ max: 500 }]}>
                                <Input.TextArea rows={2} placeholder="可选，展示在播放弹窗中" />
                            </Form.Item>
                            {copyOnly ? (
                                <Form.Item label="来源分享">
                                    <div className="featured-copy-only-note">
                                        <strong>平台持久副本</strong>
                                        <span>{copySourceLabel}。当前使用已保存的独立画布快照和素材引用；可修改标题、分类、简介和展示开关，视频与版本保持不变。</span>
                                    </div>
                                </Form.Item>
                            ) : (
                                <Form.Item label="画布分享链接" required>
                                    <div className="featured-share-row">
                                        <Form.Item name="shareLink" noStyle rules={[{ required: true, message: "请输入分享链接" }]}>
                                            <Input
                                                placeholder="粘贴本站画布分享链接（旧域名链接也可以）"
                                                onBlur={() => void loadPreview()}
                                                onPaste={() => {
                                                    window.setTimeout(() => void loadPreview(), 0);
                                                }}
                                            />
                                        </Form.Item>
                                        <Button icon={<RefreshCw className="size-4" />} loading={previewLoading} onClick={() => void loadPreview()}>
                                            识别并预览
                                        </Button>
                                    </div>
                                </Form.Item>
                            )}
                            {!copyOnly && preview ? (
                                <div className="featured-share-status" data-tone={currentTone}>
                                    <div className="featured-share-status-main">
                                        <span>{previewStatusText[preview.status]}</span>
                                        {preview.status === "ok" ? <Check className="size-4" aria-hidden="true" /> : null}
                                    </div>
                                    {preview.message ? <span className="featured-share-status-reason">{preview.message}</span> : null}
                                    {preview.status === "snapshot_missing" && preview.currentSnapshotId ? (
                                        <div className="featured-snapshot-actions">
                                            <Button size="small" onClick={() => void loadPreview({ snapshotId: preview.currentSnapshotId })}>
                                                载入当前分享版本
                                            </Button>
                                        </div>
                                    ) : null}
                                    {preview.snapshotId ? (
                                        <Button size="small" type="link" icon={<ExternalLink className="size-3.5" />} onClick={openSharePage} className="!px-0">
                                            在新窗口预览分享画布
                                        </Button>
                                    ) : null}
                                </div>
                            ) : null}
                            {!copyOnly && preview?.snapshotId ? (
                                <div className="featured-snapshot-panel">
                                    <div className="featured-snapshot-times">
                                        <span>
                                            精选绑定版本：<strong>{formatTime(preview.snapshotAt)}</strong>
                                        </span>
                                        {preview.currentSnapshotAt ? (
                                            <span>
                                                分享者当前版本：<strong>{formatTime(preview.currentSnapshotAt)}</strong>
                                            </span>
                                        ) : null}
                                    </div>
                                    {snapshotOutdated ? (
                                        <Button size="small" onClick={() => void loadPreview({ snapshotId: preview.currentSnapshotId })}>
                                            关联最新分享版本
                                        </Button>
                                    ) : (
                                        <span className="featured-modal-hint">已绑定当前分享版本</span>
                                    )}
                                </div>
                            ) : null}
                            <div className="featured-editor-toggles">
                                <Form.Item name="persistCopy" valuePropName="checked" extra="开启后由平台保存独立画布快照和素材引用，原画布被误删后仍可展示、播放和克隆；作者主动关闭分享或禁止复制时仍按授权规则下架。">
                                    <Checkbox>持久保存精选副本</Checkbox>
                                </Form.Item>
                                {persistCopyEnabled ? (
                                    <Form.Item name="consentConfirmed" valuePropName="checked">
                                        <Checkbox>已获得作者同意将作品用于首页精选并保存平台副本</Checkbox>
                                    </Form.Item>
                                ) : null}
                                <Form.Item label="首页展示" name="enabled" valuePropName="checked">
                                    <Switch />
                                </Form.Item>
                            </div>
                        </div>
                        <div className="featured-editor-column">
                            <Form.Item label="展示视频" required extra={copyOnly ? "来源分享不可用时，展示视频与绑定版本保持不变。" : "视频只能来自当前分享快照内的视频节点，或由管理员手动上传。"}>
                                <div className="featured-video-source">
                                    {copyOnly ? (
                                        <>
                                            {selectedVideo.resourceId ? (
                                                <div className="featured-selected-video">
                                                    <span className="featured-selected-video-title">
                                                        <Check className="size-3.5" aria-hidden="true" />
                                                        {selectedVideoTitle}
                                                    </span>
                                                    {previewVideoURL ? (
                                                        <video
                                                            key={previewVideoURL}
                                                            ref={previewVideoRef}
                                                            src={previewVideoURL}
                                                            controls
                                                            preload="metadata"
                                                            playsInline
                                                            onLoadedMetadata={(event) => {
                                                                const duration = event.currentTarget.duration;
                                                                setFrameDuration(Number.isFinite(duration) ? duration : 0);
                                                                setFrameTime(0);
                                                            }}
                                                            className="featured-selected-video-preview"
                                                        />
                                                    ) : (
                                                        <span className="featured-selected-video-missing">视频地址不可用，请让作者重新分享后再编辑。</span>
                                                    )}
                                                </div>
                                            ) : (
                                                <span className="featured-selected-video-missing">当前精选缺少可用视频，请让作者重新分享后再编辑。</span>
                                            )}
                                            {editing && editing.status !== "ok" ? <p className="featured-admin-status-message">当前精选不可用：{editing.statusMessage}</p> : null}
                                        </>
                                    ) : (
                                        <>
                                            <Segmented
                                                className="featured-video-source-switch"
                                                value={videoMode}
                                                onChange={(value) => setMode(value as "share" | "upload")}
                                                options={[
                                                    { label: "从分享画布选择", value: "share" },
                                                    { label: "上传视频", value: "upload" },
                                                ]}
                                            />
                                            {videoMode === "share" ? (
                                                preview?.videos?.length ? (
                                                    <div className="featured-share-videos">
                                                        {preview.videos.map((video, index) => {
                                                            const chosen = Boolean(video.resourceId && selectedVideo.resourceId === video.resourceId);
                                                            return (
                                                                <button
                                                                    key={video.resourceId || `video-${index}`}
                                                                    type="button"
                                                                    className={`featured-share-video${chosen ? " is-selected" : ""}`}
                                                                    aria-pressed={chosen}
                                                                    disabled={!video.available || !video.resourceId}
                                                                    onClick={() => {
                                                                        if (!video.resourceId) return;
                                                                        setChosenVideo({ resourceId: video.resourceId, source: "share" });
                                                                        videoModeTouchedRef.current = true;
                                                                    }}
                                                                >
                                                                    <span className="featured-share-video-media">{video.url ? <video src={video.url} preload="metadata" muted playsInline aria-hidden="true" /> : null}</span>
                                                                    <span className="featured-share-video-title">{video.title}</span>
                                                                    {video.reason ? <span className="featured-share-video-reason">{video.reason}</span> : null}
                                                                    {chosen ? <span className="featured-share-video-title">已选择</span> : null}
                                                                </button>
                                                            );
                                                        })}
                                                    </div>
                                                ) : (
                                                    <p className="featured-modal-hint">{preview ? "该分享画布没有可用的视频节点，请改用手动上传。" : "识别分享链接后可选择该画布内的视频。"}</p>
                                                )
                                            ) : (
                                                <div className="featured-upload-row">
                                                    <input
                                                        ref={uploadInputRef}
                                                        type="file"
                                                        accept="video/*"
                                                        hidden
                                                        onChange={(event) => {
                                                            const file = event.target.files?.[0];
                                                            event.target.value = "";
                                                            if (file) void uploadVideo(file);
                                                        }}
                                                    />
                                                    <Button icon={<Upload className="size-4" />} loading={uploading} onClick={() => uploadInputRef.current?.click()}>
                                                        选择视频文件
                                                    </Button>
                                                    <span className="featured-upload-hint">上传完成后可在首页悬停预览、点击播放。</span>
                                                </div>
                                            )}
                                            {selectedVideo.resourceId ? (
                                                <div className="featured-selected-video">
                                                    <span className="featured-selected-video-title">
                                                        <Check className="size-3.5" aria-hidden="true" />
                                                        {selectedVideoTitle}
                                                    </span>
                                                    {previewVideoURL ? (
                                                        <video
                                                            key={previewVideoURL}
                                                            ref={previewVideoRef}
                                                            src={previewVideoURL}
                                                            controls
                                                            preload="metadata"
                                                            playsInline
                                                            onLoadedMetadata={(event) => {
                                                                const duration = event.currentTarget.duration;
                                                                setFrameDuration(Number.isFinite(duration) ? duration : 0);
                                                                setFrameTime(0);
                                                            }}
                                                            className="featured-selected-video-preview"
                                                        />
                                                    ) : (
                                                        <span className="featured-selected-video-missing">视频地址不可用，请重新选择视频。</span>
                                                    )}
                                                </div>
                                            ) : selectedShareVideo && !selectedShareVideo.available ? (
                                                <span className="featured-selected-video-missing">当前视频不可用：{selectedShareVideo.reason || "分享中的视频无法访问"}</span>
                                            ) : null}
                                            {editing && editing.status !== "ok" ? <p className="featured-admin-status-message">当前精选不可用：{editing.statusMessage}</p> : null}
                                            {editing && !editing.videoAvailable ? <p className="featured-selected-video-missing">当前精选视频不可访问，请重新选择或上传。</p> : null}
                                        </>
                                    )}
                                </div>
                            </Form.Item>
                            <Form.Item label="视频封面" extra="首页卡片先展示封面，鼠标悬浮后才播放视频；不设置时使用视频首帧。可拖动滑块截取视频任意画面，也可上传图片或选择分享画布内的图片。">
                                <div className="featured-cover-editor">
                                    <div className="featured-cover-preview">
                                        {selectedCover.resourceId && selectedCoverURL ? <img src={selectedCoverURL} alt={`封面预览：${selectedCoverTitle}`} /> : <span className="featured-cover-placeholder">当前使用视频首帧</span>}
                                    </div>
                                    <div className="featured-cover-actions">
                                        <input
                                            ref={coverInputRef}
                                            type="file"
                                            accept="image/*"
                                            hidden
                                            onChange={(event) => {
                                                const file = event.target.files?.[0];
                                                event.target.value = "";
                                                if (file) void uploadCover(file);
                                            }}
                                        />
                                        <Button icon={<Upload className="size-4" />} loading={coverUploading} onClick={() => coverInputRef.current?.click()}>
                                            {selectedCover.resourceId ? "更换封面" : "上传封面"}
                                        </Button>
                                        <Button disabled={coverUploading || !selectedCover.resourceId} onClick={() => setChosenCover({ resourceId: "", source: "" })}>
                                            使用视频首帧
                                        </Button>
                                        {selectedCover.resourceId ? (
                                            <span className="featured-cover-name">
                                                <Check className="size-3.5" aria-hidden="true" />
                                                {selectedCoverTitle}
                                            </span>
                                        ) : null}
                                    </div>
                                    {previewVideoURL ? (
                                        <div className="featured-cover-frame">
                                            <span className="featured-cover-frame-label">从视频截取画面</span>
                                            <Slider
                                                className="featured-cover-frame-slider"
                                                min={0}
                                                max={Math.max(frameDuration, 0.1)}
                                                step={0.1}
                                                value={Math.min(frameTime, Math.max(frameDuration, 0.1))}
                                                disabled={coverUploading || coverFrameLoading || !frameDuration}
                                                tooltip={{ formatter: (value) => formatFrameTime(Number(value)) }}
                                                onChange={(value) => setFrameTime(Number(Array.isArray(value) ? value[0] : value))}
                                                onChangeComplete={(value) => {
                                                    const next = Number(Array.isArray(value) ? value[0] : value);
                                                    setFrameTime(next);
                                                    if (previewVideoRef.current) void seekPreviewVideo(previewVideoRef.current, next);
                                                }}
                                            />
                                            <span className="featured-cover-frame-time">{formatFrameTime(frameTime)}</span>
                                            <Button loading={coverFrameLoading} disabled={coverUploading || !frameDuration} onClick={() => void captureVideoFrame()}>
                                                截取此帧为封面
                                            </Button>
                                        </div>
                                    ) : null}
                                    {copyOnly ? (
                                        <p className="featured-modal-hint">来源分享不可用时只能上传新封面，或继续使用现有封面。</p>
                                    ) : preview?.images?.length ? (
                                        <div className="featured-cover-images">
                                            {preview.images.map((image, index) => {
                                                const chosen = Boolean(image.resourceId && selectedCover.resourceId === image.resourceId);
                                                return (
                                                    <button
                                                        key={image.resourceId || `cover-${index}`}
                                                        type="button"
                                                        className={`featured-cover-image${chosen ? " is-selected" : ""}`}
                                                        aria-pressed={chosen}
                                                        disabled={!image.available || !image.resourceId}
                                                        onClick={() => {
                                                            if (!image.resourceId) return;
                                                            setChosenCover({ resourceId: image.resourceId, source: "share" });
                                                        }}
                                                    >
                                                        <span className="featured-cover-image-media">{image.url ? <img src={image.url} alt="" loading="lazy" /> : null}</span>
                                                        <span className="featured-cover-image-title">{image.title}</span>
                                                        {image.reason ? <span className="featured-cover-image-reason">{image.reason}</span> : null}
                                                    </button>
                                                );
                                            })}
                                        </div>
                                    ) : (
                                        <p className="featured-modal-hint">{preview ? "该分享画布没有可用图片，可改为上传封面。" : "识别分享链接后可从画布图片中选择封面。"}</p>
                                    )}
                                    {coverOutdated ? <p className="featured-cover-warning">当前封面不在所选分享版本中，保存前请重新选择或上传封面。</p> : null}
                                    {editing?.coverError ? <p className="featured-cover-warning">{editing.coverError}</p> : null}
                                </div>
                            </Form.Item>
                        </div>
                    </div>
                </Form>
            </Modal>
        </AdminPageFrame>
    );
}
