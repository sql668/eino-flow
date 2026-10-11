"use client";

import { useState } from "react";

import { Button } from "@/components/ui/button";
import { Field, Input, Select } from "@/components/ui/field";
import { useModelConfigStore } from "@/stores/model-config-store";
import type { ModelInstanceInfo, ModelKind, ModelProviderInfo, ProbeResultInfo } from "@/types/api";

// 厂商卡片，结构对齐 Dify 的「模型供应商」页：
//   一行标题（图标 + 名称 + 右侧凭据状态与主操作）
//   一行能力标签（LLM / TEXT EMBEDDING / RERANK）
//   一行「显示模型 ▸」折叠区，展开后是该厂商下的模型清单与「添加模型」
//
// 与 Dify 的差异：Dify 把「配置凭据」放在独立弹窗，这里做成卡片内联面板——
// 本项目只有一个凭证表单，内联少一层跳转，也少一份弹窗状态。

const kindLabels: Record<ModelKind, string> = {
  llm: "对话",
  embedding: "向量化",
  rerank: "重排",
};

const kindStyles: Record<ModelKind, string> = {
  llm: "bg-pri-soft text-pri-ink",
  embedding: "bg-ok-soft text-ok",
  rerank: "bg-warn-soft text-warn",
};

export function ProviderCard({
  provider,
  models,
}: {
  provider: ModelProviderInfo;
  models: ModelInstanceInfo[];
}) {
  const pending = useModelConfigStore((state) => state.pending);
  const saveProvider = useModelConfigStore((state) => state.saveProvider);
  const deleteProvider = useModelConfigStore((state) => state.deleteProvider);
  const testProvider = useModelConfigStore((state) => state.testProvider);
  const createModel = useModelConfigStore((state) => state.createModel);
  const setModelEnabled = useModelConfigStore((state) => state.setModelEnabled);
  const deleteModel = useModelConfigStore((state) => state.deleteModel);
  const testModel = useModelConfigStore((state) => state.testModel);
  const bindGlobal = useModelConfigStore((state) => state.bindGlobal);
  const unbind = useModelConfigStore((state) => state.unbind);
  const bindings = useModelConfigStore((state) => state.bindings);

  const [editing, setEditing] = useState(false);
  const [expanded, setExpanded] = useState(provider.configured);
  const [baseUrl, setBaseUrl] = useState(provider.baseUrl);
  const [credentials, setCredentials] = useState<Record<string, string>>({});
  const [probe, setProbe] = useState<ProbeResultInfo | null>(null);
  const [newModel, setNewModel] = useState({ name: "", kind: defaultKindFor(provider), display: "" });
  const [modelProbes, setModelProbes] = useState<Record<number, ProbeResultInfo>>({});

  const busy = pending === `provider:${provider.provider}`;
  // 按当前选中的能力给示例，比固定写 bge-m3:latest 更不容易误导
  // （重排模型填成 bge-m3 是很容易犯的错）。
  const modelNamePlaceholder =
    newModel.kind === "rerank"
      ? "BAAI/bge-reranker-v2-m3"
      : newModel.kind === "llm"
        ? "qwen3:8b"
        : "bge-m3:latest";
  const credentialPayload = () => {
    const filled = Object.fromEntries(
      Object.entries(credentials).filter(([, value]) => value.trim() !== ""),
    );
    return Object.keys(filled).length > 0 ? filled : undefined;
  };

  return (
    <section className="rounded border border-grid bg-white">
      <header className="flex flex-wrap items-start gap-3 px-4 py-3">
        <div
          className="reading flex h-8 w-8 shrink-0 items-center justify-center rounded border border-grid text-xs font-semibold text-ink-soft"
          aria-hidden="true"
        >
          {provider.displayName.slice(0, 1)}
        </div>

        <div className="flex min-w-0 flex-1 flex-col gap-1.5">
          <div className="flex flex-wrap items-center gap-2">
            <h3 className="text-sm font-semibold text-ink">{provider.displayName}</h3>
            {provider.configured ? (
              <span className="flex items-center gap-1 rounded-sm bg-ok-soft px-1.5 py-0.5 text-[11px] text-ok">
                <span className="h-1.5 w-1.5 rounded-full bg-ok" />
                {provider.credentialMask ? provider.credentialMask : "已配置"}
              </span>
            ) : (
              <span className="rounded-sm bg-warn-soft px-1.5 py-0.5 text-[11px] text-warn">
                未配置
              </span>
            )}
          </div>
          {provider.description ? (
            <p className="text-xs leading-5 text-ink-soft">{provider.description}</p>
          ) : null}
          <div className="flex flex-wrap gap-1.5">
            {provider.kinds.map((kind) => (
              <span
                key={kind}
                className={`rounded-sm px-1.5 py-0.5 text-[11px] font-medium ${kindStyles[kind]}`}
              >
                {kindLabels[kind]}
              </span>
            ))}
          </div>
        </div>

        <div className="flex items-center gap-2">
          <Button
            variant={provider.configured ? "secondary" : "primary"}
            busy={busy}
            onClick={() => {
              setEditing((prev) => !prev);
              setProbe(null);
            }}
          >
            {provider.configured ? "配置" : "添加凭证"}
          </Button>
          {provider.configured ? (
            <Button
              variant="ghost"
              busy={busy}
              onClick={() => {
                if (window.confirm(`确定删除「${provider.displayName}」的配置吗？`)) {
                  void deleteProvider(provider.provider);
                }
              }}
            >
              删除
            </Button>
          ) : null}
        </div>
      </header>

      {editing ? (
        <div className="flex flex-col gap-3 border-t border-grid-soft bg-paper px-4 py-4">
          <Field label="服务地址" hint={`留空则使用默认地址 ${provider.defaultBaseUrl}`}>
            <Input
              value={baseUrl}
              onChange={(event) => setBaseUrl(event.target.value)}
              placeholder={provider.defaultBaseUrl}
            />
          </Field>

          {provider.credentialFields.map((field) => (
            <Field
              key={field.key}
              label={field.label}
              required={field.required}
              hint="凭证以 AES-256-GCM 加密存储，接口与页面都只回掩码；留空表示不修改已保存的值。"
              help={provider.credentialMask ? `已保存：${provider.credentialMask}` : undefined}
            >
              <Input
                type={field.secret ? "password" : "text"}
                autoComplete="off"
                value={credentials[field.key] ?? ""}
                placeholder={field.placeholder ?? ""}
                onChange={(event) =>
                  setCredentials((prev) => ({ ...prev, [field.key]: event.target.value }))
                }
              />
            </Field>
          ))}

          <div className="flex flex-wrap items-center gap-2">
            <Button
              busy={busy}
              onClick={async () => {
                setProbe(null);
                const result = await testProvider(provider.provider, {
                  baseUrl,
                  credentials: credentialPayload(),
                });
                if (result) {
                  setProbe(result);
                }
              }}
            >
              测试连接
            </Button>
            <Button
              variant="primary"
              busy={busy}
              onClick={async () => {
                const ok = await saveProvider(provider.provider, {
                  baseUrl,
                  credentials: credentialPayload(),
                });
                if (ok) {
                  // 保存后清空输入：界面上改为展示掩码，明文不留在 DOM 里。
                  setCredentials({});
                  setEditing(false);
                  setExpanded(true);
                }
              }}
            >
              保存
            </Button>
          </div>

          {probe ? <ProbeHint probe={probe} /> : null}
        </div>
      ) : null}

      <div className="border-t border-grid-soft px-4 py-2.5">
        <button
          type="button"
          className="flex w-full items-center gap-1.5 text-xs text-ink-muted hover:text-ink"
          onClick={() => setExpanded((prev) => !prev)}
        >
          <span className={`transition-transform ${expanded ? "rotate-90" : ""}`}>▸</span>
          显示模型（{models.length}）
        </button>
      </div>

      {expanded ? (
        <div className="flex flex-col gap-3 border-t border-grid-soft px-4 py-3">
          {!provider.configured ? (
            <p className="rounded border border-warn/30 bg-warn-soft px-3 py-2 text-xs leading-5 text-warn">
              请先保存凭证。模型不知道自己该连哪个地址，没有厂商配置就无法添加模型。
            </p>
          ) : null}

          {models.length === 0 ? (
            <p className="text-xs text-ink-soft">该厂商下还没有模型</p>
          ) : (
            <ul className="flex flex-col divide-y divide-grid-soft">
              {models.map((model) => {
                const isDefault = bindings.some(
                  (binding) => binding.modelType === model.modelType && binding.instanceId === model.id,
                );
                return (
                  <li key={model.id} className="flex flex-wrap items-center gap-2 py-2">
                    <div className="flex min-w-0 flex-1 flex-col gap-0.5">
                      <div className="flex items-center gap-2">
                        <span className="truncate text-sm font-medium text-ink">
                          {model.displayName || model.modelName}
                        </span>
                        {isDefault ? (
                          <span className="rounded-sm bg-pri-soft px-1.5 py-0.5 text-[11px] text-pri-ink">
                            默认
                          </span>
                        ) : null}
                        {!model.enabled ? (
                          <span className="rounded-sm bg-grid-soft px-1.5 py-0.5 text-[11px] text-ink-muted">
                            已停用
                          </span>
                        ) : null}
                      </div>
                      <span className="reading truncate text-xs text-ink-soft">{model.modelName}</span>
                      {modelProbes[model.id] ? <ProbeHint probe={modelProbes[model.id]} /> : null}
                    </div>

                    <div className="flex flex-wrap items-center gap-1.5">
                      <Button
                        busy={pending === `model:${model.id}`}
                        onClick={async () => {
                          const result = await testModel(model.id);
                          if (result) {
                            setModelProbes((prev) => ({ ...prev, [model.id]: result }));
                          }
                        }}
                      >
                        测试
                      </Button>
                      {(
                        isDefault ? (
                          <Button
                            busy={pending === `bind:${model.modelType}`}
                            onClick={() => void unbind(model.modelType)}
                          >
                            取消默认
                          </Button>
                        ) : (
                          <Button
                            busy={pending === `bind:${model.modelType}`}
                            disabled={!model.enabled}
                            onClick={() => void bindGlobal(model.modelType, model.id)}
                          >
                            设为默认
                          </Button>
                        )
                      )}
                      <Button
                        busy={pending === `model:${model.id}`}
                        onClick={() => void setModelEnabled(model.id, !model.enabled)}
                      >
                        {model.enabled ? "停用" : "启用"}
                      </Button>
                      <Button
                        variant="danger"
                        busy={pending === `model:${model.id}`}
                        onClick={() => {
                          if (window.confirm(`确定删除模型「${model.modelName}」吗？`)) {
                            void deleteModel(model.id);
                          }
                        }}
                      >
                        删除
                      </Button>
                    </div>
                  </li>
                );
              })}
            </ul>
          )}

          {provider.configured ? (
            // 单行排列：标签在控件上方、控件等高，因此「添加模型」按钮与三个输入框天然对齐。
            // 说明文字走标签旁的悬浮图标，不占额外行高（见 components/ui/field.tsx）。
            <div className="flex flex-wrap items-end gap-3 rounded border border-grid-soft bg-paper px-3 py-3">
              <div className="w-32">
                <Field label="能力类型">
                  <Select
                    value={newModel.kind}
                    onChange={(event) =>
                      setNewModel((prev) => ({ ...prev, kind: event.target.value as ModelKind }))
                    }
                  >
                    {provider.kinds.map((kind) => (
                      <option key={kind} value={kind}>
                        {kindLabels[kind]}
                      </option>
                    ))}
                  </Select>
                </Field>
              </div>
              <div className="min-w-48 flex-1">
                <Field label="模型名" required hint="要和对方服务里的名字完全一致，区分大小写。">
                  <Input
                    value={newModel.name}
                    onChange={(event) =>
                      setNewModel((prev) => ({ ...prev, name: event.target.value }))
                    }
                    placeholder={modelNamePlaceholder}
                  />
                </Field>
              </div>
              <div className="min-w-40 flex-1">
                <Field label="显示名" hint="留空则界面直接显示模型名。">
                  <Input
                    value={newModel.display}
                    onChange={(event) =>
                      setNewModel((prev) => ({ ...prev, display: event.target.value }))
                    }
                    placeholder="中文向量模型"
                  />
                </Field>
              </div>
              {/* 空标签占位：与 Field 的「标签行 + 间距」等高，保证按钮和输入框在同一条基线 */}
              <div className="flex flex-col gap-1.5">
                <span className="h-4" aria-hidden="true" />
                <Button
                  variant="primary"
                  busy={pending === "model:create"}
                  disabled={newModel.name.trim() === ""}
                  onClick={async () => {
                    const ok = await createModel({
                      provider: provider.provider,
                      modelName: newModel.name,
                      modelType: newModel.kind,
                      displayName: newModel.display,
                    });
                    if (ok) {
                      setNewModel((prev) => ({ ...prev, name: "", display: "" }));
                    }
                  }}
                >
                  添加模型
                </Button>
              </div>
            </div>
          ) : null}
        </div>
      ) : null}
    </section>
  );
}

/** ProbeHint 展示一次探测结果。 */
function ProbeHint({ probe }: { probe: ProbeResultInfo }) {
  return (
    <p
      className={`mt-0.5 rounded-sm px-2 py-1 text-[11px] leading-5 ${
        probe.ok ? "bg-ok-soft text-ok" : "bg-warn-soft text-warn"
      }`}
    >
      {probe.message}
      {probe.latencyMs > 0 ? `（${probe.latencyMs} ms）` : ""}
    </p>
  );
}

/** defaultKindFor 选出该厂商第一个可用的能力类型作为默认选项。 */
function defaultKindFor(provider: ModelProviderInfo): ModelKind {
  return provider.kinds.includes("embedding") ? "embedding" : provider.kinds[0];
}
