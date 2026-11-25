import { App, Button, Select, Skeleton } from "antd";
import { useEffect, useState } from "react";
import { AdminPageFrame } from "../components/admin-shell";
import { SettingsSectionCard } from "../components/admin-ui";
import { getAdminDefaultModels, updateAdminDefaultModels, type DefaultModels } from "@/services/api/default-models";
import { listAdminAllChannelModels, type AdminChannelModelListItem } from "@/services/api/wallet";

type SlotKey = keyof DefaultModels;

const slots: Array<{ key: SlotKey; title: string; capability: AdminChannelModelListItem["capability"]; hint: string }> = [
    { key: "imageModel", title: "默认生图模型", capability: "image", hint: "新用户首次进入生图时默认选中的模型。" },
    { key: "videoModel", title: "默认视频模型", capability: "video", hint: "新用户首次进入视频创作时默认选中的模型。" },
    { key: "agentTextModel", title: "默认 Agent 文本模型", capability: "text", hint: "Agent 对话与文本任务的默认模型。" },
    { key: "optimizerModel", title: "默认润色模型", capability: "text", hint: "提示词润色的默认模型；留空则跟随 Agent 文本模型。" },
    { key: "audioModel", title: "默认音频模型", capability: "audio", hint: "语音、音效等音频任务的默认模型。" },
];

const emptyModels: DefaultModels = { imageModel: "", videoModel: "", agentTextModel: "", optimizerModel: "", audioModel: "" };

function pickModels(source: DefaultModels): DefaultModels {
    return {
        imageModel: source.imageModel,
        videoModel: source.videoModel,
        agentTextModel: source.agentTextModel,
        optimizerModel: source.optimizerModel,
        audioModel: source.audioModel,
    };
}

export default function DefaultModelsSettingsPage() {
    const { message } = App.useApp();
    const [models, setModels] = useState<AdminChannelModelListItem[]>([]);
    const [value, setValue] = useState<DefaultModels | null>(null);
    const [saving, setSaving] = useState(false);

    useEffect(() => {
        let active = true;
        Promise.all([getAdminDefaultModels(), listAdminAllChannelModels()])
            .then(([defaults, catalog]) => {
                if (!active) return;
                setValue(pickModels(defaults.defaults));
                setModels(catalog.models.filter((model) => model.channelEnabled && model.enabled));
            })
            .catch(() => {
                if (active) message.error("读取默认模型配置失败，请刷新重试");
            });
        return () => {
            active = false;
        };
    }, [message]);

    const save = async () => {
        if (!value) return;
        setSaving(true);
        try {
            const saved = await updateAdminDefaultModels(value);
            setValue(pickModels(saved.defaults));
            message.success("默认模型已保存，新注册用户首次登录时生效");
        } catch (error) {
            message.error(error instanceof Error ? error.message : "保存默认模型失败");
        } finally {
            setSaving(false);
        }
    };

    return (
        <AdminPageFrame title="默认模型" description="为新注册用户指定首次登录使用的默认模型；用户仍可在账号设置里修改" scroll>
            {!value ? (
                <Skeleton active />
            ) : (
                <div className="admin-settings-stack">
                    <SettingsSectionCard title="用户默认模型" description="只影响首次登录且本地没有配置的新用户；已有用户和用户手动修改过的配置不会被覆盖。留空时回退到该能力的第一个可用模型。">
                        <div className="flex flex-col gap-4 p-4">
                            {slots.map((slot) => {
                                const options = models
                                    .filter((model) => model.capability === slot.capability)
                                    .map((model) => ({
                                        value: `${model.channelId}::${model.modelKey}`,
                                        label: `${model.channelName} · ${model.displayName || model.modelKey}`,
                                    }));
                                return (
                                    <label key={slot.key}>
                                        {slot.title}
                                        <Select
                                            className="mt-2 w-full"
                                            allowClear
                                            showSearch
                                            optionFilterProp="label"
                                            value={value[slot.key] || undefined}
                                            placeholder="未设置，使用第一个可用模型"
                                            options={options}
                                            onChange={(next?: string) => setValue({ ...value, [slot.key]: next || "" })}
                                        />
                                        <span className="mt-1 block text-xs text-foreground/50">{slot.hint}</span>
                                    </label>
                                );
                            })}
                            <div className="flex flex-wrap gap-3">
                                <Button type="primary" loading={saving} onClick={() => void save()}>
                                    保存默认模型
                                </Button>
                                <Button disabled={saving} onClick={() => setValue({ ...emptyModels })}>
                                    清空全部
                                </Button>
                            </div>
                        </div>
                    </SettingsSectionCard>
                </div>
            )}
        </AdminPageFrame>
    );
}
