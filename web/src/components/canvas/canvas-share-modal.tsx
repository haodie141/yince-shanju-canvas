import { useCallback, useEffect, useMemo, useState } from "react";
import { App, Button, Input, Modal, Spin, Switch } from "antd";
import { Copy, Link2, RefreshCw, Share2, Unlink } from "lucide-react";
import copyToClipboard from "copy-to-clipboard";

import { canvasThemes } from "@/lib/canvas-theme";
import { createCanvasShare, deleteCanvasShare, getCanvasShare, setCanvasShareCopyPermission, type CanvasShareStatus } from "@/services/api/canvas-share";
import { useActiveTheme } from "@/stores/canvas/use-canvas-theme-store";
import { Select } from "@/components/ui/base/select";

export function CanvasShareModal({ projectId, open, onClose, beforeCreate }: { projectId: string; open: boolean; onClose: () => void; beforeCreate: () => Promise<boolean | void> }) {
    const { message, modal } = App.useApp();
    const theme = canvasThemes[useActiveTheme()];
    const [share, setShare] = useState<CanvasShareStatus>({ enabled: false });
    const [expiresDays, setExpiresDays] = useState(0);
    const [allowCopy, setAllowCopy] = useState(false);
    const [loading, setLoading] = useState(false);
    const [submitting, setSubmitting] = useState(false);
    const shareUrl = useMemo(() => share.token ? `${window.location.origin}/share/canvas/${share.token}` : "", [share.token]);

    const load = useCallback(async () => {
        setLoading(true);
        try {
            const result = await getCanvasShare(projectId);
            setShare(result.share);
            setAllowCopy(Boolean(result.share.allowCopy));
        } catch (error) {
            message.error(error instanceof Error ? error.message : "读取分享状态失败");
        } finally {
            setLoading(false);
        }
    }, [message, projectId]);

    useEffect(() => {
        if (open) void load();
    }, [load, open]);

    const copy = async (value = shareUrl) => {
        if (!value) return;
        if (!copyToClipboard(value)) { message.warning("复制失败，请手动复制分享链接"); return; }
        message.success("分享链接已复制");
    };

    const create = async (rotate = false) => {
        setSubmitting(true);
        try {
            const saved = await beforeCreate();
            if (saved === false) return;
            const result = await createCanvasShare(projectId, { expiresDays, rotate, allowCopy, preserveExpiry: share.enabled });
            setShare(result.share);
            const url = result.share.token ? `${window.location.origin}/share/canvas/${result.share.token}` : "";
            await copy(url);
        } catch (error) {
            message.error(error instanceof Error ? error.message : "创建分享链接失败");
        } finally {
            setSubmitting(false);
        }
    };

    const revoke = () => modal.confirm({
        title: "停止公开分享？",
        content: "现有分享链接会立即失效，原画布内容不会被删除。",
        okText: "停止分享",
        okButtonProps: { danger: true },
        cancelText: "取消",
        onOk: async () => {
            await deleteCanvasShare(projectId);
            setShare({ enabled: false });
            message.success("已停止分享");
        },
    });

    return (
        <Modal title={<span className="inline-flex items-center gap-2"><Share2 className="size-4" />分享画布</span>} open={open} onCancel={onClose} footer={null} centered width={520} destroyOnHidden>
            <Spin spinning={loading}>
                <div className="border-t pt-5" style={{ borderColor: theme.node.stroke }}>
                    <p className="mb-4 text-sm leading-6" style={{ color: theme.node.muted }}>
                        获得链接的人无需登录即可查看分享时的画布。之后的修改不会自动同步，可随时更新分享内容。
                    </p>
                    <div className="mb-4 flex items-center justify-between gap-3 text-sm">
                        <span>允许登录用户保存画布副本</span>
                        <Switch checked={allowCopy} disabled={submitting} onChange={(checked) => {
                            if (!share.enabled) { setAllowCopy(checked); return; }
                            setSubmitting(true);
                            void setCanvasShareCopyPermission(projectId, checked).then(({ share: updated }) => { setShare(updated); setAllowCopy(Boolean(updated.allowCopy)); }).catch((error) => message.error(error instanceof Error ? error.message : "更新分享权限失败")).finally(() => setSubmitting(false));
                        }} />
                    </div>
                    {share.enabled && shareUrl ? (
                        <div className="space-y-4">
                            <Input value={shareUrl} readOnly suffix={<Button type="text" className="!h-7 !w-7 !min-w-7 !p-0" icon={<Copy className="size-3.5" />} onClick={() => void copy()} aria-label="复制分享链接" />} />
                            <div className="flex flex-wrap items-center justify-between gap-3">
                                <span className="text-xs" style={{ color: theme.node.muted }}>{share.snapshotAt ? `内容更新于 ${new Date(share.snapshotAt).toLocaleString("zh-CN")} · ` : ""}{share.expiresAt ? `有效至 ${new Date(share.expiresAt).toLocaleString("zh-CN")}` : "长期有效，直至手动停止分享"}</span>
                                <div className="flex gap-2">
                                    <Button loading={submitting} onClick={() => void create(false)}>更新分享内容</Button>
                                    <Button icon={<RefreshCw className="size-3.5" />} loading={submitting} onClick={() => void create(true)}>重新生成链接</Button>
                                    <Button danger icon={<Unlink className="size-3.5" />} onClick={revoke}>停止分享</Button>
                                </div>
                            </div>
                        </div>
                    ) : (
                        <div className="flex items-center gap-2">
                            <Select value={expiresDays} onChange={setExpiresDays} className="min-w-40" options={[{ value: 0, label: "长期有效" }, { value: 7, label: "7 天有效" }, { value: 30, label: "30 天有效" }]} />
                            <Button type="primary" icon={<Link2 className="size-4" />} loading={submitting} onClick={() => void create(false)}>创建并复制链接</Button>
                        </div>
                    )}
                </div>
            </Spin>
        </Modal>
    );
}
