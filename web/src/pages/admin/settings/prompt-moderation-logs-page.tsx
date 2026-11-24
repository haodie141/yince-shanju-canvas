import { App, Button, Table } from "antd";
import { useEffect, useState } from "react";
import { useNavigate } from "react-router";
import { getPromptModerationLogs, type PromptModerationDetails, type PromptModerationLog } from "@/services/api/prompt-moderation";
import { AdminPageFrame } from "../components/admin-shell";
import { SettingsSectionCard } from "../components/admin-ui";

function reviewDetails(row: PromptModerationLog): PromptModerationDetails {
    try { const value = JSON.parse(row.responseBody || "{}"); return value && typeof value === "object" && !Array.isArray(value) ? value : {}; } catch { return {}; }
}

function decisionText(decision?: { action: string; reason?: string }, model?: string, duration?: number) {
    if (!decision?.action) return "未执行";
    return [model, decision.action === "pass" ? "通过" : `拒绝：${decision.reason || "未提供原因"}`, duration != null ? `${duration} 毫秒` : ""].filter(Boolean).join(" · ");
}

export default function PromptModerationLogsPage() {
    const { message } = App.useApp();
    const navigate = useNavigate();
    const [page, setPage] = useState(1);
    const [total, setTotal] = useState(0);
    const [logs, setLogs] = useState<PromptModerationLog[]>([]);
    const [loading, setLoading] = useState(true);
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
    return (
        <AdminPageFrame title="内容审核记录" description="查看初审、复审及最终结果" scroll>
            <div className="admin-settings-stack">
                <SettingsSectionCard title="审核记录" description="记录每次审核结论和原因，不记录完整提示词。">
                    <div className="p-4">
                        <div className="mb-4">
                            <Button onClick={() => navigate("/admin/settings/prompt-moderation")}>返回审核配置</Button>
                        </div>
                        <Table<PromptModerationLog>
                            rowKey="id"
                            loading={loading}
                            dataSource={logs}
                            scroll={{ x: 760 }}
                            pagination={{ current: page, pageSize: 30, total, onChange: setPage }}
                            columns={[
                                { title: "时间", dataIndex: "createdAt", render: (value: string) => new Date(value).toLocaleString("zh-CN") },
                                { title: "用户", render: (_, row) => row.displayName || row.username || row.userId },
                                { title: "账号", render: (_, row) => row.username || row.userId },
                                { title: "类型", dataIndex: "capability", render: (value: string) => (value === "image" ? "图片" : value === "video" ? "视频" : value) },
                                { title: "结果", dataIndex: "errorCode", render: (value: string) => (value === "prompt_review_pass" ? "通过" : value === "prompt_review_unavailable" ? "审核不可用" : value === "prompt_review_invalid_input" ? "输入无效" : "审核拒绝") },
                                { title: "初审", render: (_, row) => { const d = reviewDetails(row); return d.stage === "hard_block" ? `硬拦截：${d.initial?.reason || "敏感词"}` : d.stage === "initial" && d.failure ? `不可用：${d.failure}` : decisionText(d.initial, d.initialModel, d.initialDurationMs); } },
                                { title: "复审", render: (_, row) => { const d = reviewDetails(row); return d.stage === "review" && d.failure ? `不可用：${d.failure}` : decisionText(d.review, d.reviewModel, d.reviewDurationMs); } },
                                { title: "具体原因", dataIndex: "error" },
                            ]}
                        />
                    </div>
                </SettingsSectionCard>
            </div>
        </AdminPageFrame>
    );
}
