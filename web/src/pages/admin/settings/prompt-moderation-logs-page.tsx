import { App, Button, Descriptions, Spin, Table } from "antd";
import { Copy, Eye } from "lucide-react";
import { useEffect, useRef, useState } from "react";
import { useNavigate } from "react-router";
import { useCopyText } from "@/hooks/use-copy-text";
import { AdminDrawer } from "@/pages/admin/ui/overlays";
import { getPromptModerationLog, getPromptModerationLogs, type PromptModerationDetails, type PromptModerationLog, type PromptModerationLogDetail } from "@/services/api/prompt-moderation";
import { AdminPageFrame } from "../components/admin-shell";
import { SettingsSectionCard } from "../components/admin-ui";

function formatTime(value?: string) {
    return value ? new Date(value).toLocaleString("zh-CN", { hour12: false }) : "—";
}

function reviewDetails(row: { responseBody?: string }): PromptModerationDetails {
    try {
        const value = JSON.parse(row.responseBody || "{}");
        return value && typeof value === "object" && !Array.isArray(value) ? value : {};
    } catch {
        return {};
    }
}

function decisionText(decision?: { action: string; reason?: string }, model?: string, duration?: number) {
    if (!decision?.action) return "未执行";
    return [model, decision.action === "pass" ? "通过" : `拒绝：${decision.reason || "未提供原因"}`, duration != null ? `${duration} 毫秒` : ""].filter(Boolean).join(" · ");
}

function initialText(row: { responseBody?: string }) {
    const details = reviewDetails(row);
    if (details.stage === "hard_block") return `硬拦截：${details.initial?.reason || "敏感词"}`;
    if (details.stage === "initial" && details.failure) return `不可用：${details.failure}`;
    return decisionText(details.initial, details.initialModel, details.initialDurationMs);
}

function reviewText(row: { responseBody?: string }) {
    const details = reviewDetails(row);
    return details.stage === "review" && details.failure ? `不可用：${details.failure}` : decisionText(details.review, details.reviewModel, details.reviewDurationMs);
}

function capabilityText(value: string) {
    return value === "image" ? "图片" : value === "video" ? "视频" : value;
}

function resultText(value: string) {
    return value === "prompt_review_pass" ? "通过" : value === "prompt_review_unavailable" ? "审核不可用" : value === "prompt_review_invalid_input" ? "输入无效" : "审核拒绝";
}

export default function PromptModerationLogsPage() {
    const { message } = App.useApp();
    const copyText = useCopyText();
    const navigate = useNavigate();
    const [page, setPage] = useState(1);
    const [total, setTotal] = useState(0);
    const [logs, setLogs] = useState<PromptModerationLog[]>([]);
    const [loading, setLoading] = useState(true);
    const [detailOpen, setDetailOpen] = useState(false);
    const [detailLoading, setDetailLoading] = useState(false);
    const [detail, setDetail] = useState<PromptModerationLogDetail | null>(null);
    const detailRequestRef = useRef(0);
    useEffect(() => {
        let active = true;
        setLoading(true);
        getPromptModerationLogs(page)
            .then((result) => {
                if (active) {
                    setLogs(result.logs || []);
                    setTotal(result.total);
                }
            })
            .catch(() => {
                if (active) message.error("读取审核记录失败");
            })
            .finally(() => {
                if (active) setLoading(false);
            });
        return () => {
            active = false;
        };
    }, [page, message]);

    const openDetail = (row: PromptModerationLog) => {
        const request = detailRequestRef.current + 1;
        detailRequestRef.current = request;
        setDetailOpen(true);
        setDetail(null);
        setDetailLoading(true);
        getPromptModerationLog(row.id)
            .then((result) => {
                if (request === detailRequestRef.current) setDetail(result.log);
            })
            .catch((error) => {
                if (request === detailRequestRef.current) message.error(error instanceof Error ? error.message : "读取审核详情失败");
            })
            .finally(() => {
                if (request === detailRequestRef.current) setDetailLoading(false);
            });
    };

    const closeDetail = () => {
        detailRequestRef.current += 1;
        setDetailOpen(false);
        setDetail(null);
        setDetailLoading(false);
    };

    const detailInfo = detail ? reviewDetails(detail) : {};
    const detailItems = detail
        ? [
              { key: "time", label: "时间", children: formatTime(detail.createdAt) },
              { key: "user", label: "用户", children: detail.displayName || detail.username || detail.userId },
              { key: "account", label: "账号", children: detail.username || detail.userId },
              { key: "capability", label: "类型", children: capabilityText(detail.capability) },
              { key: "result", label: "结果", children: resultText(detail.errorCode) },
              { key: "reason", label: "具体原因", children: detail.error || "—" },
              { key: "initial", label: "初审", children: initialText(detail) },
              { key: "review", label: "复审", children: reviewText(detail) },
              ...(detailInfo.stage === "hard_block" ? [{ key: "stage", label: "阶段", children: "敏感词硬拦截（未调用审核模型）" }] : []),
              ...(detailInfo.failure ? [{ key: "failure", label: "审核失败", children: detailInfo.failure }] : []),
          ]
        : [];

    return (
        <AdminPageFrame title="内容审核记录" description="查看初审、复审及最终结果" scroll>
            <div className="admin-settings-stack">
                <SettingsSectionCard title="审核记录" description="列表只显示结论和提示词摘要，点开详情可查看本次审核的完整提示词。">
                    <div className="p-4">
                        <div className="mb-4">
                            <Button onClick={() => navigate("/admin/settings/prompt-moderation")}>返回审核配置</Button>
                        </div>
                        <Table<PromptModerationLog>
                            rowKey="id"
                            loading={loading}
                            dataSource={logs}
                            scroll={{ x: 1080 }}
                            pagination={{ current: page, pageSize: 30, total, onChange: setPage }}
                            columns={[
                                { title: "时间", dataIndex: "createdAt", width: 170, render: (value: string) => formatTime(value) },
                                { title: "用户", render: (_, row) => row.displayName || row.username || row.userId },
                                { title: "账号", render: (_, row) => row.username || row.userId },
                                { title: "类型", dataIndex: "capability", width: 80, render: (value: string) => capabilityText(value) },
                                { title: "结果", dataIndex: "errorCode", width: 100, render: (value: string) => resultText(value) },
                                { title: "提示词", dataIndex: "promptPreview", ellipsis: true, render: (value: string) => value || "—" },
                                { title: "初审", render: (_, row) => initialText(row) },
                                { title: "复审", render: (_, row) => reviewText(row) },
                                { title: "具体原因", dataIndex: "error", ellipsis: true },
                                {
                                    title: "操作",
                                    key: "actions",
                                    width: 90,
                                    fixed: "right" as const,
                                    render: (_, row) => (
                                        <Button size="small" icon={<Eye className="size-3.5" />} onClick={() => openDetail(row)}>
                                            详情
                                        </Button>
                                    ),
                                },
                            ]}
                        />
                    </div>
                </SettingsSectionCard>
            </div>
            <AdminDrawer title="审核详情" open={detailOpen} onClose={closeDetail} size="min(760px, 100vw)" rootClassName="admin-drawer">
                {detailLoading && !detail ? (
                    <div className="flex justify-center py-12">
                        <Spin />
                    </div>
                ) : detail ? (
                    <div className="space-y-5">
                        <Descriptions bordered size="small" column={{ xs: 1, sm: 2 }} items={detailItems} />
                        <div>
                            <div className="mb-2 flex items-center justify-between gap-3">
                                <div className="text-sm font-medium">用户提示词</div>
                                <div className="flex items-center gap-2">
                                    <span className="text-xs text-foreground/50">{detail.promptLength} 字</span>
                                    <Button size="small" icon={<Copy className="size-3.5" />} disabled={!detail.prompt} onClick={() => copyText(detail.prompt || "", "提示词已复制")}>
                                        复制
                                    </Button>
                                </div>
                            </div>
                            <div className="max-h-[360px] overflow-auto whitespace-pre-wrap break-words rounded-md border border-border p-3 text-sm leading-6" style={{ background: "var(--admin-surface-2)" }}>
                                {detail.prompt || "该记录没有保存提示词。"}
                            </div>
                        </div>
                    </div>
                ) : (
                    <div className="py-12 text-center text-sm text-foreground/50">审核记录不存在或已被清理。</div>
                )}
            </AdminDrawer>
        </AdminPageFrame>
    );
}
