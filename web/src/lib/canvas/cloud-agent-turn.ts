type TurnMessage = { id: string; role: string; interjection?: string };
type TurnRun = { id: string; idempotencyKey?: string };

/** 输入区状态只属于当前运行；插话仍属于原运行，不视作新一轮。 */
export function currentAgentTurnMessages<T extends TurnMessage>(messages: readonly T[], run: TurnRun | null): readonly T[] {
    if (!run) return [];
    let start = -1;
    for (let index = messages.length - 1; index >= 0; index -= 1) {
        if (messages[index].role === "user" && !messages[index].interjection) {
            start = index;
            break;
        }
    }
    if (start < 0) return messages;
    const userId = messages[start].id;
    if (userId !== `user-${run.id}` && (!run.idempotencyKey || userId !== `user-${run.idempotencyKey}`)) return [];
    return messages.slice(start);
}
