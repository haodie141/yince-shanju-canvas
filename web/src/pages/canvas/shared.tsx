import { useCallback, useEffect, useMemo, useRef, useState, type MouseEvent as ReactMouseEvent, type ReactNode } from "react";
import { App, Button } from "antd";
import { Clapperboard, Copy, Eye, FileText, Image as ImageIcon, LockKeyhole, LogIn, Send, Share2, Video } from "lucide-react";
import { Link, useLocation, useNavigate, useParams } from "react-router";
import { nanoid } from "nanoid";
import { sha256 } from "@noble/hashes/sha256";

import { AuthLoginLink } from "@/components/auth/auth-login-link";
import { ConnectionPath } from "@/components/canvas/canvas-connections";
import { CanvasNodeToolbar, CanvasNodeInfoModal } from "@/components/canvas/canvas-node-toolbar";
import { CanvasFrameNode } from "@/components/canvas/canvas-frame-node";
import { CanvasNode } from "@/components/canvas/canvas-node";
import { CanvasBatchTableNodeContent } from "@/components/canvas/canvas-batch-table-node";
import { CanvasZoomControls } from "@/components/canvas/canvas-zoom-controls";
import { CanvasViewport } from "@/components/canvas/canvas-viewport";
import { FullScreenLoader } from "@/components/ui/aceternity/full-screen-loader";
import { WorkspaceState } from "@/components/layout/workspace-state";
import { NODE_DEFAULT_SIZE } from "@/constant/canvas";
import { canvasAppearanceBaseTheme, canvasAppearanceForTheme, DEFAULT_CANVAS_BACKGROUND_MODE, normalizeCanvasAppearance, resolveCanvasAppearance, type CanvasAppearance } from "@/lib/canvas/canvas-appearance";
import { canvasThemes } from "@/lib/canvas-theme";
import { FOLDER_COLLAPSED_HEIGHT, FOLDER_COLLAPSED_WIDTH, isCanvasFolderNode, isFrameNode, isNodeHiddenByCollapsedFrame, resolveFrameConnection } from "@/lib/canvas/canvas-frame";
import { ensureMediaNodeMinimumSize } from "@/lib/canvas/canvas-node-size";
import { getContextResourceNodes } from "@/lib/canvas/canvas-resource-references";
import { cloneCanvasShare, getPublicCanvasShare } from "@/services/api/canvas-share";
import { useCanvasThemeStore, useCanvasThemeScope } from "@/stores/canvas/use-canvas-theme-store";
import { CanvasNodeType, type CanvasNodeData, type Position, type ViewportTransform } from "@/types/canvas";
import { nodeGenerationPrompt } from "@/lib/canvas/generation-contract";
import { useThemePack } from "@/themes/use-theme-pack";
import { useAppearanceStore } from "@/stores/use-appearance-store";
import { useUserStore } from "@/stores/use-user-store";
import { useAuthDialogStore } from "@/stores/use-auth-dialog-store";
import { flushCanvasStorePersistence, useCanvasStore } from "@/stores/canvas/use-canvas-store";
import { resourceFileUrl, resourceStorageKey, uploadResourceFile } from "@/services/api/resources";
import { hasRemoteUserDataSyncSession, loadCanvasProjectForEditing, saveRemoteUserDataNow } from "@/services/user-data-sync";
type ContextMenu = { x: number; y: number; world: Position; nodeId?: string };
type DragState = { primaryId: string; nodeIds: string[]; startX: number; startY: number; origins: Map<string, Position>; moved: boolean };
async function shareCopyIntent(token: string) {
    return Array.from(sha256(new TextEncoder().encode(token)), (byte) => byte.toString(16).padStart(2, "0")).join("");
}

export default function SharedCanvasPage() {
    useCanvasThemeScope();
    const { token = "" } = useParams();
    const requestedSnapshot = new URLSearchParams(useLocation().search).get("snapshot") || "";
    const navigate = useNavigate();
    const { message, modal } = App.useApp();
    const colorTheme = useCanvasThemeStore((state) => state.theme);
    const theme = canvasThemes[colorTheme];
    const containerRef = useRef<HTMLDivElement>(null);
    const viewportRef = useRef<ViewportTransform>({ x: 0, y: 0, k: 1 });
    const dragRef = useRef<DragState | null>(null);
    const copyAbortRef = useRef<AbortController | null>(null);
    const toolbarTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
    const [title, setTitle] = useState("共享画布");
    const [nodes, setNodes] = useState<CanvasNodeData[]>([]);
    const [connections, setConnections] = useState<Awaited<ReturnType<typeof getPublicCanvasShare>>["project"]["connections"]>([]);
    const [appearance, setAppearance] = useState<CanvasAppearance>(() => canvasAppearanceForTheme(colorTheme));
    const [backgroundMode, setBackgroundMode] = useState<"lines" | "dots" | "blank">("dots");
    const [viewport, setViewport] = useState<ViewportTransform>({ x: 0, y: 0, k: 1 });
    const [selectedNodeId, setSelectedNodeId] = useState<string | null>(null);
    const [playingVideoId, setPlayingVideoId] = useState<string | null>(null);
    const [infoNodeId, setInfoNodeId] = useState<string | null>(null);
    const [toolbarNodeId, setToolbarNodeId] = useState<string | null>(null);
    const [dragOffset, setDragOffset] = useState<Position | null>(null);
    const [contextMenu, setContextMenu] = useState<ContextMenu | null>(null);
    const [loading, setLoading] = useState(true);
    const [loadError, setLoadError] = useState("");
    const [allowCopy, setAllowCopy] = useState(false);
    const [snapshotId, setSnapshotId] = useState("");
    const [copying, setCopying] = useState(false);
    const [copyProgress, setCopyProgress] = useState("");
    const pack = useThemePack();
    const brandName = useAppearanceStore((state) => state.appearance.brandName);
    const user = useUserStore((state) => state.user);

    const unauthorized = useCallback(() => message.warning("未授权：分享画布仅供查看，该操作不会执行。"), [message]);
    const infoNode = nodes.find((node) => node.id === infoNodeId) || null;
    const nodeById = useMemo(() => new Map(nodes.map((node) => [node.id, node])), [nodes]);
    const frameChildrenById = useMemo(() => {
        const result = new Map<string, CanvasNodeData[]>();
        nodes.forEach((node) => {
            if (!node.parentId) return;
            const children = result.get(node.parentId) || [];
            children.push(node);
            result.set(node.parentId, children);
        });
        return result;
    }, [nodes]);
    const visibleNodes = useMemo(() => nodes.filter((node) => !isNodeHiddenByCollapsedFrame(node, nodes)), [nodes]);
    const visibleConnections = useMemo(
        () =>
            connections.flatMap((connection) => {
                const resolved = resolveFrameConnection(connection, nodes);
                return resolved ? [{ connection, ...resolved }] : [];
            }),
        [connections, nodes],
    );
    const connectionBounds = useMemo(() => {
        if (!nodes.length) return { left: -1, top: -1, width: 2, height: 2 };
        const padding = 320;
        const left = Math.min(...nodes.map((node) => node.position.x)) - padding;
        const top = Math.min(...nodes.map((node) => node.position.y)) - padding;
        const right = Math.max(...nodes.map((node) => node.position.x + node.width)) + padding;
        const bottom = Math.max(...nodes.map((node) => node.position.y + node.height)) + padding;
        return { left, top, width: right - left, height: bottom - top };
    }, [nodes]);

    useEffect(() => {
        let active = true;
        const themeBeforeShare = useCanvasThemeStore.getState().theme;
        let appliedShareTheme: typeof themeBeforeShare | null = null;
        let themeChangedAfterApply = false;
        const unsubscribeTheme = useCanvasThemeStore.subscribe((state, previous) => {
            if (appliedShareTheme && state.theme !== previous.theme && state.theme !== appliedShareTheme) themeChangedAfterApply = true;
        });
        setLoading(true);
        getPublicCanvasShare(token, requestedSnapshot)
            .then(({ project, allowCopy: permitted, snapshotId: version }) => {
                if (!active) return;
                setAllowCopy(Boolean(permitted));
                setSnapshotId(version || "");
                setTitle(project.title || "共享画布");
                setNodes((project.nodes || []).map(ensureMediaNodeMinimumSize));
                setConnections(project.connections || []);
                const nextAppearance = project.appearance ? normalizeCanvasAppearance(project.appearance, themeBeforeShare) : canvasAppearanceForTheme(themeBeforeShare);
                setAppearance(nextAppearance);
                appliedShareTheme = canvasAppearanceBaseTheme(nextAppearance, themeBeforeShare);
                useCanvasThemeStore.getState().setTheme(appliedShareTheme);
                setBackgroundMode(project.backgroundMode || DEFAULT_CANVAS_BACKGROUND_MODE);
                const initial = project.viewport || { x: 0, y: 0, k: 1 };
                viewportRef.current = initial;
                setViewport(initial);
            })
            .catch((error) => {
                if (active) setLoadError(error instanceof Error ? error.message : "分享链接无效或已失效");
            })
            .finally(() => {
                if (active) setLoading(false);
            });
        return () => {
            active = false;
            unsubscribeTheme();
            if (appliedShareTheme && !themeChangedAfterApply && useCanvasThemeStore.getState().theme === appliedShareTheme) {
                useCanvasThemeStore.getState().setTheme(themeBeforeShare);
            }
        };
    }, [token, requestedSnapshot]);

    const copyCanvas = async () => {
        if (copying || copyAbortRef.current) return;
        if (!user) {
            sessionStorage.setItem("canvas-share-copy-after-login", await shareCopyIntent(token));
            useAuthDialogStore.getState().openDialog("login", null);
            message.info("登录后即可保存画布副本");
            return;
        }
        setCopying(true);
        const controller = new AbortController();
        copyAbortRef.current = controller;
        let uploaded = 0;
        try {
            if (!useCanvasStore.getState().hydrated || !hasRemoteUserDataSyncSession()) throw new Error("账号画布正在同步，请稍后重试");
            const shared = await getPublicCanvasShare(token, requestedSnapshot);
            if (!shared.allowCopy) throw new Error("分享者未允许保存副本或已关闭该权限");
            if (shared.copyIssues?.length) {
                modal.warning({ title: "部分内容暂不能复制", content: <div><p>为避免保存不完整的画布，请联系分享者处理以下内容后重试：</p><ul>{shared.copyIssues.map((issue, index) => <li key={index}>{issue}</li>)}</ul></div> });
                return;
            }
            if ((shared.snapshotId || "") !== snapshotId) throw new Error("分享内容已更新，请刷新页面后再保存");
            if (snapshotId) {
                const key = `canvas-share-clone:${await shareCopyIntent(token)}:${user.id}`;
                let pending: { snapshotId?: string; requestId?: string } = {};
                try { pending = JSON.parse(sessionStorage.getItem(key) || "{}"); } catch { /* 重置损坏的请求记录 */ }
                const requestId = pending.snapshotId === snapshotId && pending.requestId ? pending.requestId : nanoid();
                sessionStorage.setItem(key, JSON.stringify({ snapshotId, requestId }));
                setCopyProgress("正在保存画布");
                const { canvasId } = await cloneCanvasShare(token, snapshotId, requestId, controller.signal);
                sessionStorage.removeItem(key);
                if (useUserStore.getState().user?.id !== user.id) throw new Error("副本已保存，请切回原账号查看");
                try { await loadCanvasProjectForEditing(canvasId); } catch { message.warning("副本已保存，打开画布时请稍候重试"); }
                message.success("已保存到我的画布");
                navigate(`/canvas/${encodeURIComponent(canvasId)}`);
                return;
            }
            const copied = new Map<string, { storageKey: string; url: string }>();
            const resourcePrefix = `/api/public/canvas-shares/${token}/resources/`;
            const urls = new Map<string, "image" | "video" | "audio">();
            const collect = (value: unknown, kind: "image" | "video" | "audio", key = "") => {
                if (typeof value === "string" && value.startsWith(resourcePrefix)) {
                    if (!urls.has(value)) urls.set(value, kind);
                } else if (Array.isArray(value)) value.forEach((child) => collect(child, kind, key));
                else if (value && typeof value === "object") Object.entries(value).forEach(([name, child]) => collect(child, name === "videoPreview" || name === "references" ? "image" : kind, name));
            };
            shared.project.nodes.forEach((node) => collect(node.metadata, node.type === CanvasNodeType.Video ? "video" : node.type === CanvasNodeType.Audio ? "audio" : "image"));
            let completed = 0;
            for (const [url, fallbackKind] of urls) {
                if (controller.signal.aborted || useUserStore.getState().user?.id !== user.id) throw new Error("操作已停止或登录账号已切换");
                if (!copied.has(url)) {
                    setCopyProgress(`正在复制素材 ${completed + 1}/${urls.size}`);
                    const response = await fetch(url, { credentials: "same-origin", signal: controller.signal });
                    if (!response.ok) throw new Error(`素材读取失败（${response.status}），请联系分享者检查素材`);
                    const blob = await response.blob();
                    if (!blob.size || /^(text\/html|application\/json)/i.test(blob.type)) throw new Error("素材不可用，无法保存");
                    const kind = blob.type.startsWith("video/") ? "video" : blob.type.startsWith("audio/") ? "audio" : blob.type.startsWith("image/") ? "image" : fallbackKind;
                    const resource = await uploadResourceFile(blob, kind, { fileName: kind });
                    uploaded++;
                    copied.set(url, { storageKey: resourceStorageKey(resource.id), url: resourceFileUrl(resource.id) });
                }
                completed++;
            }
            const latest = await getPublicCanvasShare(token);
            if (!latest.allowCopy) throw new Error("分享者已关闭保存副本权限");
            if (JSON.stringify(latest.project) !== JSON.stringify(shared.project)) throw new Error("分享画布在复制期间发生变化，请重新保存副本");
            if (controller.signal.aborted || useUserStore.getState().user?.id !== user.id) throw new Error("操作已停止或登录账号已切换");
            const rewrite = (value: unknown, key = ""): unknown => {
                if (typeof value === "string") {
                    const media = copied.get(value);
                    return media ? (key === "content" || key === "previewContent" ? media.url : media.storageKey) : value;
                }
                if (Array.isArray(value)) return value.map((item) => rewrite(item, key));
                if (!value || typeof value !== "object") return value;
                const source = value as Record<string, unknown>;
                const result = Object.fromEntries(Object.entries(source).map(([name, child]) => [name, rewrite(child, name)]));
                const media = typeof source.content === "string" ? copied.get(source.content) : undefined;
                if (media) result.storageKey = media.storageKey;
                return result;
            };
            const nodes = shared.project.nodes.map((node) => {
                const metadata = rewrite(node.metadata) as CanvasNodeData["metadata"];
                return metadata?.status === "loading" ? { ...node, metadata: { ...metadata, status: "idle" as const } } : { ...node, metadata };
            });
            setCopyProgress("正在保存画布");
            const id = useCanvasStore.getState().importProject({ ...shared.project, id: undefined, projectId: undefined, title: `${shared.project.title || "共享画布"}（副本）`, nodes, chatSessions: [], activeChatId: null });
            await flushCanvasStorePersistence();
            try {
                await saveRemoteUserDataNow(id);
            } catch {
                if (useUserStore.getState().user?.id !== user.id) throw new Error("登录账号已切换，请切回原账号查看本机副本");
                message.warning("副本已保存在本机，但云端同步未完成；请在画布列表检查同步状态，避免重复保存。");
                navigate(`/canvas/${encodeURIComponent(id)}`);
                return;
            }
            message.success("已保存到我的画布");
            navigate(`/canvas/${encodeURIComponent(id)}`);
        } catch (error) {
            const detail = error instanceof Error ? error.message : "保存副本失败，请稍后重试";
            modal.error({ title: "保存副本未完成", content: uploaded ? `${detail}。已有 ${uploaded} 个素材上传到当前账号，可能占用存储空间，请检查后再重试。` : detail });
        } finally {
            if (copyAbortRef.current === controller) copyAbortRef.current = null;
            setCopying(false);
            setCopyProgress("");
        }
    };

    useEffect(() => () => copyAbortRef.current?.abort(), []);

    useEffect(() => {
        if (!user || loading || !allowCopy || !sessionStorage.getItem("canvas-share-copy-after-login")) return;
        let active = true;
        void shareCopyIntent(token).then((intent) => {
            if (!active || sessionStorage.getItem("canvas-share-copy-after-login") !== intent) return;
            sessionStorage.removeItem("canvas-share-copy-after-login");
            void copyCanvas();
        });
        return () => { active = false; };
    }, [user, loading, allowCopy, token]);

    useEffect(() => {
        const onMove = (event: MouseEvent) => {
            const drag = dragRef.current;
            if (!drag) return;
            const next = { x: (event.clientX - drag.startX) / viewportRef.current.k, y: (event.clientY - drag.startY) / viewportRef.current.k };
            if (Math.abs(event.clientX - drag.startX) > 3 || Math.abs(event.clientY - drag.startY) > 3) drag.moved = true;
            setDragOffset(next);
        };
        const onUp = (event: MouseEvent) => {
            const drag = dragRef.current;
            if (!drag) return;
            const offset = { x: (event.clientX - drag.startX) / viewportRef.current.k, y: (event.clientY - drag.startY) / viewportRef.current.k };
            if (drag.moved)
                setNodes((current) =>
                    current.map((node) => {
                        const origin = drag.origins.get(node.id);
                        return origin ? { ...node, position: { x: origin.x + offset.x, y: origin.y + offset.y } } : node;
                    }),
                );
            else setInfoNodeId(drag.primaryId);
            dragRef.current = null;
            setDragOffset(null);
            document.body.style.cursor = "default";
        };
        window.addEventListener("mousemove", onMove);
        window.addEventListener("mouseup", onUp);
        return () => {
            window.removeEventListener("mousemove", onMove);
            window.removeEventListener("mouseup", onUp);
            document.body.style.cursor = "default";
            if (toolbarTimerRef.current) clearTimeout(toolbarTimerRef.current);
        };
    }, []);

    const keepToolbar = useCallback((nodeId: string) => {
        if (toolbarTimerRef.current) clearTimeout(toolbarTimerRef.current);
        toolbarTimerRef.current = null;
        setToolbarNodeId(nodeId);
    }, []);
    const hideToolbar = useCallback(() => {
        if (toolbarTimerRef.current) clearTimeout(toolbarTimerRef.current);
        toolbarTimerRef.current = setTimeout(() => {
            toolbarTimerRef.current = null;
            setToolbarNodeId(null);
        }, 160);
    }, []);

    useEffect(() => {
        const onKeyDown = (event: KeyboardEvent) => {
            if ((event.metaKey || event.ctrlKey) && event.key.toLowerCase() === "s") {
                event.preventDefault();
                unauthorized();
            }
            if (event.key === "Escape") setContextMenu(null);
        };
        window.addEventListener("keydown", onKeyDown);
        return () => window.removeEventListener("keydown", onKeyDown);
    }, [unauthorized]);

    const onViewportChange = useCallback((next: ViewportTransform) => {
        viewportRef.current = next;
        setViewport(next);
        setContextMenu(null);
    }, []);
    const setZoom = (scale: number) => {
        const container = containerRef.current;
        if (!container) return;
        const rect = container.getBoundingClientRect();
        const current = viewportRef.current;
        onViewportChange({
            x: rect.width / 2 - ((rect.width / 2 - current.x) / current.k) * scale,
            y: rect.height / 2 - ((rect.height / 2 - current.y) / current.k) * scale,
            k: scale,
        });
    };
    const resetViewport = () => {
        const container = containerRef.current;
        if (!container || !nodes.length) return onViewportChange({ x: 0, y: 0, k: 1 });
        const rect = container.getBoundingClientRect();
        const left = Math.min(...nodes.map((node) => node.position.x));
        const top = Math.min(...nodes.map((node) => node.position.y));
        const right = Math.max(...nodes.map((node) => node.position.x + node.width));
        const bottom = Math.max(...nodes.map((node) => node.position.y + node.height));
        const scale = Math.min(1, Math.max(0.05, Math.min((rect.width - 120) / Math.max(right - left, 1), (rect.height - 140) / Math.max(bottom - top, 1))));
        onViewportChange({ x: rect.width / 2 - ((left + right) / 2) * scale, y: rect.height / 2 - ((top + bottom) / 2) * scale, k: scale });
    };
    const openContextMenu = (event: ReactMouseEvent, nodeId?: string) => {
        event.preventDefault();
        event.stopPropagation();
        const rect = containerRef.current?.getBoundingClientRect();
        if (!rect) return;
        const current = viewportRef.current;
        setContextMenu({
            x: event.clientX - rect.left,
            y: event.clientY - rect.top,
            world: { x: (event.clientX - rect.left - current.x) / current.k, y: (event.clientY - rect.top - current.y) / current.k },
            nodeId,
        });
    };
    const addNode = (type: CanvasNodeType) => {
        if (!contextMenu) return;
        const size = NODE_DEFAULT_SIZE[type];
        setNodes((current) => [
            ...current,
            {
                id: `shared-${nanoid()}`,
                type,
                title: type === CanvasNodeType.Text ? "临时文本" : type === CanvasNodeType.Image ? "临时图片" : type === CanvasNodeType.Video ? "临时视频" : "临时分镜",
                position: contextMenu.world,
                width: size.width,
                height: size.height,
                metadata: type === CanvasNodeType.Text ? { content: "此节点只存在于当前浏览器页面，刷新后消失。" } : {},
            },
        ]);
        setContextMenu(null);
        message.info("已添加临时节点，刷新页面后会消失");
    };
    const toggleFrame = (nodeId: string) =>
        setNodes((current) =>
            current.map((node) => {
                if (node.id !== nodeId || !isFrameNode(node)) return node;
                const collapsed = !node.metadata?.frame?.collapsed;
                const frame = node.metadata?.frame;
                return {
                    ...node,
                    width: collapsed ? (isCanvasFolderNode(node) ? FOLDER_COLLAPSED_WIDTH : 240) : frame?.expandedWidth || node.width,
                    height: collapsed ? (isCanvasFolderNode(node) ? FOLDER_COLLAPSED_HEIGHT : 144) : frame?.expandedHeight || node.height,
                    metadata: { ...node.metadata, frame: { collapsed, expandedWidth: collapsed ? node.width : frame?.expandedWidth || node.width, expandedHeight: collapsed ? node.height : frame?.expandedHeight || node.height } },
                };
            }),
        );
    const renderSharedNode = useCallback(
        (node: CanvasNodeData): ReactNode => {
            if (node.type === CanvasNodeType.Script) return <SharedScriptNode node={node} onUnauthorized={unauthorized} />;
            if (node.type === CanvasNodeType.BatchTable)
                return (
                    <CanvasBatchTableNodeContent
                        node={node}
                        nodes={nodes}
                        connections={connections}
                        batch={node.metadata?.generationBatches?.at(-1)}
                        theme={theme}
                        readOnly
                        onPatchTable={() => {}}
                        onAddRow={() => {}}
                        onRemoveRow={() => {}}
                        onUpdateRow={() => {}}
                        onFillRows={() => {}}
                        onGenerate={() => {}}
                        onRetryItem={() => {}}
                        onAddReferenceColumn={() => {}}
                        onReorderReferenceColumns={() => {}}
                        onMoveReferenceCell={() => {}}
                        onUploadReference={() => {}}
                        onConnectStart={() => {}}
                    />
                );
            return <SharedConfigNode node={node} onUnauthorized={unauthorized} />;
        },
        [connections, nodes, theme, unauthorized],
    );
    const toolbarNodeKey = selectedNodeId;
    const toolbarNode = toolbarNodeKey ? nodeById.get(toolbarNodeKey) || null : null;

    if (loading) return <FullScreenLoader label="正在打开共享画布" detail="读取节点、连线和视图状态" />;
    if (loadError)
        return (
            <div className="grid h-screen place-items-center px-5" style={{ background: theme.canvas.background }}>
                <WorkspaceState
                    icon="error"
                    title="分享链接不可用"
                    description={loadError}
                    action={
                        <Link to="/">
                            <Button>返回首页</Button>
                        </Link>
                    }
                />
            </div>
        );

    return (
        <main className="relative h-screen overflow-hidden" style={{ background: resolveCanvasAppearance(appearance, colorTheme).background, color: theme.node.text }}>
            <header className="pointer-events-none absolute inset-x-0 top-0 z-[var(--z-panel-floating)] flex h-16 items-center justify-between px-5">
                <div className="pointer-events-auto flex min-w-0 items-center gap-3">
                    <Share2 className="size-4" style={{ color: theme.node.muted }} />
                    <span className="max-w-[45vw] truncate text-base font-semibold">{title}</span>
                    <span className="inline-flex items-center gap-1 text-xs" style={{ color: theme.node.muted }}>
                        <Eye className="size-3.5" />
                        只读分享
                    </span>
                </div>
                <div className="pointer-events-auto flex items-center gap-2">
                {allowCopy ? <Button type="text" icon={<Copy className="size-4" />} loading={copying} onClick={() => void copyCanvas()}>{copyProgress || "保存到我的画布"}</Button> : null}
                {user ? (
                    <Link className="pointer-events-auto" to="/canvas"><Button type="text">我的画布</Button></Link>
                ) : (
                    <AuthLoginLink className="pointer-events-auto">
                        <Button type="text" icon={<LogIn className="size-4" />}>登录</Button>
                    </AuthLoginLink>
                )}
                </div>
            </header>

            <CanvasViewport
                containerRef={containerRef}
                viewport={viewport}
                appearance={appearance}
                backgroundMode={backgroundMode}
                onViewportChange={onViewportChange}
                onViewportPreviewChange={(next) => {
                    viewportRef.current = next;
                }}
                onCanvasDeselect={() => {
                    setSelectedNodeId(null);
                    setContextMenu(null);
                }}
                onContextMenu={(event) => openContextMenu(event)}
                onDrop={(event) => {
                    event.preventDefault();
                    unauthorized();
                }}
            >
                <svg
                    className="absolute overflow-visible"
                    viewBox={`${connectionBounds.left} ${connectionBounds.top} ${connectionBounds.width} ${connectionBounds.height}`}
                    style={{ left: connectionBounds.left, top: connectionBounds.top, width: connectionBounds.width, height: connectionBounds.height, pointerEvents: "none", zIndex: 0 }}
                >
                    {visibleConnections.map(({ connection, from, to }) => (
                        <ConnectionPath key={connection.id} connection={connection} from={from} to={to} active={false} onSelect={() => setInfoNodeId(to.id)} />
                    ))}
                </svg>
                {visibleNodes.map((node) =>
                    isFrameNode(node) ? (
                        <CanvasFrameNode
                            key={node.id}
                            data={node}
                            dragOffset={dragRef.current?.nodeIds.includes(node.id) && dragOffset ? dragOffset : undefined}
                            childNodes={frameChildrenById.get(node.id) || []}
                            scale={viewport.k}
                            isSelected={selectedNodeId === node.id}
                            isDropTarget={false}
                            readOnly
                            onMouseDown={(event, nodeId) => {
                                event.stopPropagation();
                                if (event.button !== 0) return;
                                setSelectedNodeId(nodeId);
                                setContextMenu(null);
                                const dragged = [node, ...(frameChildrenById.get(nodeId) || [])];
                                dragRef.current = { primaryId: nodeId, nodeIds: dragged.map((item) => item.id), startX: event.clientX, startY: event.clientY, origins: new Map(dragged.map((item) => [item.id, item.position])), moved: false };
                                document.body.style.cursor = "grabbing";
                            }}
                            onResize={() => undefined}
                            onToggleCollapsed={toggleFrame}
                            onFolderStyleChange={() => undefined}
                            onTitleChange={unauthorized}
                            onHoverStart={keepToolbar}
                            onHoverEnd={hideToolbar}
                            onContextMenu={(event, nodeId) => openContextMenu(event, nodeId)}
                        />
                    ) : (
                        <CanvasNode
                            key={node.id}
                            data={node}
                            dragOffset={dragRef.current?.nodeIds.includes(node.id) && dragOffset ? dragOffset : undefined}
                            scale={viewport.k}
                            isSelected={selectedNodeId === node.id}
                            isRelated={false}
                            isFocusRelated={false}
                            isConnectionTarget={false}
                            showImageInfo={false}
                            readOnly
                            mediaActive={playingVideoId === node.id}
                            onMediaPlayRequest={setPlayingVideoId}
                            renderNodeContent={renderSharedNode}
                            onMouseDown={(event, nodeId) => {
                                event.stopPropagation();
                                if (event.button !== 0) return;
                                const target = nodes.find((item) => item.id === nodeId);
                                if (!target) return;
                                setSelectedNodeId(nodeId);
                                setContextMenu(null);
                                dragRef.current = { primaryId: nodeId, nodeIds: [nodeId], startX: event.clientX, startY: event.clientY, origins: new Map([[nodeId, target.position]]), moved: false };
                                document.body.style.cursor = "grabbing";
                            }}
                            onHoverStart={keepToolbar}
                            onHoverEnd={hideToolbar}
                            onConnectStart={unauthorized}
                            onResize={() => undefined}
                            onContentChange={unauthorized}
                            onRetry={unauthorized}
                            onOpenTaskDetails={unauthorized}
                            onViewImage={(target) => setInfoNodeId(target.id)}
                            onContextMenu={(event, nodeId) => openContextMenu(event, nodeId)}
                        />
                    ),
                )}
            </CanvasViewport>

            <CanvasNodeToolbar
                node={dragRef.current ? null : toolbarNode}
                viewport={viewport}
                containerRef={containerRef}
                onKeep={keepToolbar}
                onLeave={hideToolbar}
                onInfo={(node) => setInfoNodeId(node.id)}
                onEditText={unauthorized}
                onDecreaseFont={unauthorized}
                onIncreaseFont={unauthorized}
                onToggleDialog={unauthorized}
                onAnnotate={unauthorized}
                onAnnotationEdit={unauthorized}
                onTextEdit={unauthorized}
                onGenerateImage={unauthorized}
                onUpload={unauthorized}
                onDownload={unauthorized}
                onSaveAsset={unauthorized}
                onRegionEdit={unauthorized}
                onRemoveBackground={unauthorized}
                onLayerDecomposition={unauthorized}
                onEmotion={unauthorized}
                onPortraitTexture={unauthorized}
                onCrop={unauthorized}
                onSplit={unauthorized}
                onUpscale={unauthorized}
                onSuperResolve={unauthorized}
                onAngle={unauthorized}
                onLighting={unauthorized}
                onPanorama={unauthorized}
                onPrevis={unauthorized}
                onViewImage={unauthorized}
                onExtractVideoFrames={unauthorized}
                onExtractAudioFromVideo={unauthorized}
                onTrimVideoSegments={unauthorized}
                extractingVideoFrames={false}
                extractingAudio={false}
                trimmingVideo={false}
                onReversePrompt={unauthorized}
                onRetry={unauthorized}
                onToggleFreeResize={unauthorized}
                onToggleLocked={unauthorized}
                onDelete={unauthorized}
                onNineGrid={unauthorized}
            />

            <div className="absolute bottom-5 left-5 z-[var(--z-panel-floating)]">
                <CanvasZoomControls scale={viewport.k} containerRef={containerRef} onScaleChange={setZoom} onFitContent={resetViewport} isMiniMapOpen={false} onToggleMiniMap={unauthorized} onOpenShortcuts={unauthorized} />
            </div>
            <div className="pointer-events-none absolute bottom-5 right-5 z-[var(--z-panel-floating)] max-w-[340px] text-right text-xs leading-5" style={{ color: theme.node.muted }}>
                访客操作仅在当前页面临时生效
                {pack?.copy?.sealGlyph ? (
                    <span className="shared-canvas-signature">
                        <i aria-hidden="true">{pack.copy.sealGlyph}</i>
                        {brandName}
                    </span>
                ) : null}
            </div>

            {contextMenu ? (
                <SharedContextMenu
                    menu={contextMenu}
                    onAdd={addNode}
                    onInfo={() => {
                        if (contextMenu.nodeId) setInfoNodeId(contextMenu.nodeId);
                        setContextMenu(null);
                    }}
                    onUnauthorized={() => {
                        setContextMenu(null);
                        unauthorized();
                    }}
                />
            ) : null}
            <CanvasNodeInfoModal node={infoNode} open={Boolean(infoNode)} onClose={() => setInfoNodeId(null)} readOnly onUnauthorized={unauthorized} />
        </main>
    );
}

function SharedContextMenu({ menu, onAdd, onInfo, onUnauthorized }: { menu: ContextMenu; onAdd: (type: CanvasNodeType) => void; onInfo: () => void; onUnauthorized: () => void }) {
    const theme = canvasThemes[useCanvasThemeStore((state) => state.theme)];
    return (
        <div
            data-canvas-no-zoom
            className="absolute z-[var(--z-modal)] min-w-48 rounded-lg border p-2 shadow-xl"
            style={{ left: menu.x, top: menu.y, background: theme.toolbar.panel, borderColor: theme.toolbar.border, color: theme.node.text }}
            onMouseDown={(event) => event.stopPropagation()}
        >
            {menu.nodeId ? (
                <>
                    <MenuButton icon={<Eye />} label="查看节点信息" onClick={onInfo} />
                    <MenuButton icon={<LockKeyhole />} label="编辑或生成" onClick={onUnauthorized} />
                </>
            ) : (
                <>
                    <div className="px-2 py-2 text-[var(--fs-label)]" style={{ color: theme.node.muted }}>
                        添加临时节点
                    </div>
                    <MenuButton icon={<FileText />} label="文本节点" onClick={() => onAdd(CanvasNodeType.Text)} />
                    <MenuButton icon={<ImageIcon />} label="图片节点" onClick={() => onAdd(CanvasNodeType.Image)} />
                    <MenuButton icon={<Video />} label="视频节点" onClick={() => onAdd(CanvasNodeType.Video)} />
                    <MenuButton icon={<Clapperboard />} label="分镜脚本" onClick={() => onAdd(CanvasNodeType.Script)} />
                </>
            )}
        </div>
    );
}

function MenuButton({ icon, label, onClick }: { icon: ReactNode; label: string; onClick: () => void }) {
    return (
        <button type="button" className="flex h-9 w-full items-center gap-2 rounded-md px-2 text-left text-sm transition hover:bg-black/5 dark:hover:bg-white/10" onClick={onClick}>
            <span className="grid size-4 place-items-center [&>svg]:size-4">{icon}</span>
            {label}
        </button>
    );
}

function SharedConfigNode({ node, onUnauthorized }: { node: CanvasNodeData; onUnauthorized: () => void }) {
    const theme = canvasThemes[useCanvasThemeStore((state) => state.theme)];
    return (
        <div className="flex h-full w-full flex-col overflow-hidden rounded-[var(--panel-radius)]">
            <div className="flex h-10 shrink-0 items-center gap-2 border-b px-4" style={{ background: theme.node.panel, borderColor: theme.node.stroke }}>
                <ImageIcon className="size-4" />
                <span className="min-w-0 flex-1 truncate text-sm font-semibold">{node.title}</span>
            </div>
            <div className="min-h-0 flex-1 whitespace-pre-wrap break-words p-4 text-sm leading-6" style={{ color: theme.node.muted }}>
                {nodeGenerationPrompt(node) || "未填写提示词"}
            </div>
            <div className="flex h-12 shrink-0 items-center justify-end border-t px-3" style={{ borderColor: theme.node.stroke }}>
                <Button
                    size="small"
                    icon={<Send className="size-3.5" />}
                    onMouseDown={(event) => event.stopPropagation()}
                    onClick={(event) => {
                        event.stopPropagation();
                        onUnauthorized();
                    }}
                >
                    生成
                </Button>
            </div>
        </div>
    );
}

function SharedScriptNode({ node, onUnauthorized }: { node: CanvasNodeData; onUnauthorized: () => void }) {
    const theme = canvasThemes[useCanvasThemeStore((state) => state.theme)];
    const rows = node.metadata?.storyboard?.rows || [];
    return (
        <div className="flex h-full w-full flex-col overflow-hidden rounded-[var(--panel-radius)]">
            <div className="flex h-10 shrink-0 items-center gap-2 border-b px-4" style={{ background: theme.node.panel, borderColor: theme.node.stroke }}>
                <Clapperboard className="size-4" />
                <span className="min-w-0 flex-1 truncate text-sm font-semibold">{node.title}</span>
                <span className="text-xs" style={{ color: theme.node.muted }}>
                    {rows.length} 镜
                </span>
                <button
                    type="button"
                    className="grid size-7 place-items-center rounded hover:bg-black/5 dark:hover:bg-white/10"
                    onMouseDown={(event) => event.stopPropagation()}
                    onClick={(event) => {
                        event.stopPropagation();
                        onUnauthorized();
                    }}
                    aria-label="一键创建视频节点"
                >
                    <Video className="size-3.5" />
                </button>
            </div>
            <div data-canvas-wheel-scroll className="min-h-0 flex-1 overflow-y-auto" onWheel={(event) => event.stopPropagation()}>
                {rows.length ? (
                    rows.map((row) => (
                        <div key={row.id} className="grid grid-cols-[52px_72px_minmax(180px,1fr)_minmax(150px,.8fr)] border-b text-xs leading-5" style={{ minHeight: 48, borderColor: theme.node.stroke }}>
                            <span className="grid place-items-center border-r" style={{ borderColor: theme.node.stroke, color: theme.node.muted }}>
                                #{row.shotNumber}
                            </span>
                            <span className="grid place-items-center border-r" style={{ borderColor: theme.node.stroke }}>
                                {row.durationSeconds}s
                            </span>
                            <span className="border-r px-3 py-2" style={{ borderColor: theme.node.stroke }}>
                                {row.plotDescription || "-"}
                            </span>
                            <span className="px-3 py-2" style={{ color: theme.node.muted }}>
                                {row.dialogue || "-"}
                            </span>
                        </div>
                    ))
                ) : (
                    <div className="grid h-full place-items-center text-sm" style={{ color: theme.node.muted }}>
                        暂无分镜
                    </div>
                )}
            </div>
            {nodeGenerationPrompt(node) ? (
                <div className="max-h-24 shrink-0 overflow-y-auto border-t px-3 py-2 text-xs leading-5" style={{ borderColor: theme.node.stroke, color: theme.node.muted }}>
                    {nodeGenerationPrompt(node)}
                </div>
            ) : null}
        </div>
    );
}
