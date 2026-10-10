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
  llm: "bg-violet-50 text-violet-700",
  embedding: "bg-emerald-50 text-emerald-700",
  rerank: "bg-amber-50 text-amber-700",
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
  const credentialPayload = () => {
    const filled = Object.fromEntries(
      Object.entries(credentials).filter(([, value]) => value.trim() !== ""),
    );
    return Object.keys(filled).length > 0 ? filled : undefined;
  };

  return (
    <section className="overflow-hidden rounded-xl border border-neutral-200 bg-white">
      <header className="flex flex-wrap items-start gap-3 px-4 py-3">
        <div
          className="flex h-9 w-9 shrink-0 items-center justify-center rounded-lg border border-neutral-200 text-sm font-semibold text-neutral-500"
          aria-hidden="true"
        >
          {provider.displayName.slice(0, 1)}
        </div>

        <div className="flex min-w-0 flex-1 flex-col gap-2">
          <div className="flex flex-wrap items-center gap-2">
            <h3 className="text-sm font-semibold text-neutral-900">{provider.displayName}</h3>
            {provider.configured ? (
              <span className="flex items-center gap-1 rounded-md bg-emerald-50 px-1.5 py-0.5 text-[11px] text-emerald-700">
                <span className="h-1.5 w-1.5 rounded-full bg-emerald-500" />
                {provider.credentialMask ? provider.credentialMask : "已配置"}
              </span>
            ) : (
              <span className="rounded-md bg-amber-50 px-1.5 py-0.5 text-[11px] text-amber-700">
                未配置
              </span>
            )}
          </div>
          <div className="flex flex-wrap gap-1.5">
            {provider.kinds.map((kind) => (
              <span
                key={kind}
                className={`rounded px-1.5 py-0.5 text-[11px] font-medium ${kindStyles[kind]}`}
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
        <div className="flex flex-col gap-3 border-t border-neutral-100 bg-neutral-50/60 px-4 py-4">
          <Field label="服务地址" hint={`留空使用默认地址：${provider.defaultBaseUrl}`}>
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
              hint={
                provider.credentialMask
                  ? `已保存：${provider.credentialMask}（留空表示不修改）`
                  : "凭证以 AES-256-GCM 加密存储，接口只回掩码"
              }
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

      <div className="border-t border-neutral-100 px-4 py-2.5">
        <button
          type="button"
          className="flex w-full items-center gap-1.5 text-xs text-neutral-600 hover:text-neutral-900"
          onClick={() => setExpanded((prev) => !prev)}
        >
          <span className={`transition-transform ${expanded ? "rotate-90" : ""}`}>▸</span>
          显示模型（{models.length}）
        </button>
      </div>

      {expanded ? (
        <div className="flex flex-col gap-3 border-t border-neutral-100 px-4 py-3">
          {!provider.configured ? (
            <p className="rounded-md bg-amber-50 px-3 py-2 text-xs leading-5 text-amber-800">
              请先保存凭证。模型不知道自己该连哪个地址，没有厂商配置就无法添加模型。
            </p>
          ) : null}

          {models.length === 0 ? (
            <p className="text-xs text-neutral-400">该厂商下还没有模型</p>
          ) : (
            <ul className="flex flex-col divide-y divide-neutral-100">
              {models.map((model) => {
                const isDefault = bindings.some(
                  (binding) => binding.modelType === model.modelType && binding.instanceId === model.id,
                );
                return (
                  <li key={model.id} className="flex flex-wrap items-center gap-2 py-2">
                    <div className="flex min-w-0 flex-1 flex-col gap-0.5">
                      <div className="flex items-center gap-2">
                        <span className="truncate text-sm font-medium text-neutral-900">
                          {model.displayName || model.modelName}
                        </span>
                        {isDefault ? (
                          <span className="rounded bg-blue-50 px-1.5 py-0.5 text-[11px] text-blue-700">
                            默认
                          </span>
                        ) : null}
                        {!model.enabled ? (
                          <span className="rounded bg-neutral-100 px-1.5 py-0.5 text-[11px] text-neutral-500">
                            已停用
                          </span>
                        ) : null}
                      </div>
                      <span className="truncate text-xs text-neutral-500">{model.modelName}</span>
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
                      {model.modelType === "embedding" || model.modelType === "rerank" ? (
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
                      ) : (
                        <span
                          className="text-[11px] text-neutral-400"
                          title="对话模型还没有消费方（随流式对话落地）"
                        >
                          暂不可设为默认
                        </span>
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
            <div className="flex flex-wrap items-end gap-2 rounded-lg bg-neutral-50 px-3 py-3">
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
                <Field label="模型名" required hint="与服务端一致，如 bge-m3:latest">
                  <Input
                    value={newModel.name}
                    onChange={(event) =>
                      setNewModel((prev) => ({ ...prev, name: event.target.value }))
                    }
                    placeholder="bge-m3:latest"
                  />
                </Field>
              </div>
              <div className="min-w-40 flex-1">
                <Field label="显示名（可选）">
                  <Input
                    value={newModel.display}
                    onChange={(event) =>
                      setNewModel((prev) => ({ ...prev, display: event.target.value }))
                    }
                    placeholder="中文向量模型"
                  />
                </Field>
              </div>
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
      className={`mt-0.5 rounded-md px-2 py-1 text-[11px] leading-5 ${
        probe.ok ? "bg-emerald-50 text-emerald-800" : "bg-amber-50 text-amber-800"
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
