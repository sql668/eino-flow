"use client";

import { useMemo, useState } from "react";

import { Button } from "@/components/ui/button";
import { Card, Field, Input, Select } from "@/components/ui/field";
import { useModelConfigStore } from "@/stores/model-config-store";
import type { ModelInstanceInfo, ModelKind, ProbeResultInfo } from "@/types/api";

// 模型清单：新增、测试、启停、删除、设为全局默认。
//
// 「设为默认」只对 embedding 开放：当前只有向量化模型真正被消费（入库与检索），
// llm / rerank 的实现分别在 V0.1 后半段与阶段 3 落地。
// 摆出不可用的开关比没有开关更糟——使用者会以为它生效了。

const kindLabels: Record<ModelKind, string> = {
  llm: "对话（LLM）",
  embedding: "向量化（Embedding）",
  rerank: "重排（Rerank）",
};

export function ModelSection() {
  const providers = useModelConfigStore((state) => state.providers);
  const models = useModelConfigStore((state) => state.models);
  const bindings = useModelConfigStore((state) => state.bindings);
  const pending = useModelConfigStore((state) => state.pending);
  const createModel = useModelConfigStore((state) => state.createModel);
  const setModelEnabled = useModelConfigStore((state) => state.setModelEnabled);
  const deleteModel = useModelConfigStore((state) => state.deleteModel);
  const testModel = useModelConfigStore((state) => state.testModel);
  const bindGlobal = useModelConfigStore((state) => state.bindGlobal);
  const unbind = useModelConfigStore((state) => state.unbind);

  const [modelType, setModelType] = useState<ModelKind>("embedding");
  const [provider, setProvider] = useState("");
  const [modelName, setModelName] = useState("");
  const [displayName, setDisplayName] = useState("");
  const [probes, setProbes] = useState<Record<number, ProbeResultInfo>>({});

  // 只有声明支持该能力的厂商才出现在下拉里，避免出现「选了不支持的厂商」。
  const candidates = useMemo(
    () => providers.filter((item) => item.kinds.includes(modelType)),
    [providers, modelType],
  );

  const globalBinding = bindings.find((item) => item.modelType === "embedding");

  const handleCreate = async () => {
    const ok = await createModel({
      provider: provider || candidates[0]?.provider || "",
      modelName,
      modelType,
      displayName,
    });
    if (ok) {
      setModelName("");
      setDisplayName("");
    }
  };

  return (
    <Card
      title="模型清单"
      description="同一厂商下可以有多个模型；向量化模型可设为全局默认，入库与检索都会用它。"
    >
      <div className="flex flex-col gap-4">
        <div className="grid gap-3 sm:grid-cols-4">
          <Field label="能力类型">
            <Select
              value={modelType}
              onChange={(event) => {
                setModelType(event.target.value as ModelKind);
                setProvider("");
              }}
            >
              {Object.entries(kindLabels).map(([value, label]) => (
                <option key={value} value={value}>
                  {label}
                </option>
              ))}
            </Select>
          </Field>
          <Field
            label="厂商"
            hint={candidates.length === 0 ? "没有支持该能力的厂商，请先保存厂商配置" : undefined}
          >
            <Select value={provider} onChange={(event) => setProvider(event.target.value)}>
              <option value="">请选择</option>
              {candidates.map((item) => (
                <option key={item.provider} value={item.provider}>
                  {item.displayName}
                  {item.configured ? "" : "（未配置凭证）"}
                </option>
              ))}
            </Select>
          </Field>
          <Field label="模型名" required hint="与服务端一致，如 bge-m3:latest">
            <Input
              value={modelName}
              onChange={(event) => setModelName(event.target.value)}
              placeholder="bge-m3:latest"
            />
          </Field>
          <Field label="显示名（可选）">
            <Input
              value={displayName}
              onChange={(event) => setDisplayName(event.target.value)}
              placeholder="中文向量模型"
            />
          </Field>
        </div>

        <div>
          <Button
            variant="primary"
            busy={pending === "model:create"}
            disabled={modelName.trim() === ""}
            onClick={handleCreate}
          >
            添加模型
          </Button>
        </div>

        <div className="overflow-hidden rounded-md border border-neutral-200">
          <table className="w-full text-sm">
            <thead className="bg-neutral-50 text-xs text-neutral-500">
              <tr>
                <th className="px-3 py-2 text-left font-medium">模型</th>
                <th className="px-3 py-2 text-left font-medium">类型</th>
                <th className="px-3 py-2 text-left font-medium">状态</th>
                <th className="px-3 py-2 text-right font-medium">操作</th>
              </tr>
            </thead>
            <tbody>
              {models.length === 0 ? (
                <tr>
                  <td colSpan={4} className="px-3 py-6 text-center text-xs text-neutral-400">
                    还没有模型，先在上方添加一个
                  </td>
                </tr>
              ) : (
                models.map((model) => (
                  <ModelRow
                    key={model.id}
                    model={model}
                    busy={pending === `model:${model.id}`}
                    isGlobalDefault={globalBinding?.instanceId === model.id}
                    probe={probes[model.id]}
                    onTest={async () => {
                      const result = await testModel(model.id);
                      if (result) {
                        setProbes((prev) => ({ ...prev, [model.id]: result }));
                      }
                    }}
                    onToggle={() => void setModelEnabled(model.id, !model.enabled)}
                    onDelete={() => {
                      if (window.confirm(`确定删除模型「${model.modelName}」吗？`)) {
                        void deleteModel(model.id);
                      }
                    }}
                    onBind={() => void bindGlobal("embedding", model.id)}
                    onUnbind={() => void unbind("embedding")}
                  />
                ))
              )}
            </tbody>
          </table>
        </div>
      </div>
    </Card>
  );
}

interface ModelRowProps {
  model: ModelInstanceInfo;
  busy: boolean;
  isGlobalDefault: boolean;
  probe?: ProbeResultInfo;
  onTest: () => void;
  onToggle: () => void;
  onDelete: () => void;
  onBind: () => void;
  onUnbind: () => void;
}

function ModelRow({
  model,
  busy,
  isGlobalDefault,
  probe,
  onTest,
  onToggle,
  onDelete,
  onBind,
  onUnbind,
}: ModelRowProps) {
  return (
    <tr className="border-t border-neutral-100 align-top">
      <td className="px-3 py-2">
        <div className="flex flex-col gap-0.5">
          <span className="font-medium text-neutral-900">
            {model.displayName || model.modelName}
            {isGlobalDefault ? (
              <span className="ml-2 rounded bg-blue-50 px-1.5 py-0.5 text-xs text-blue-700">
                全局默认
              </span>
            ) : null}
          </span>
          <span className="text-xs text-neutral-500">
            {model.modelName} · {model.providerName || model.provider}
            {model.bindingCount > 0 ? ` · 被 ${model.bindingCount} 处引用` : ""}
          </span>
          {probe ? (
            <span className={`text-xs ${probe.ok ? "text-emerald-700" : "text-amber-700"}`}>
              {probe.message}
            </span>
          ) : null}
        </div>
      </td>
      <td className="px-3 py-2 text-xs text-neutral-600">{kindLabels[model.modelType]}</td>
      <td className="px-3 py-2 text-xs">
        <span className={model.enabled ? "text-emerald-700" : "text-neutral-400"}>
          {model.enabled ? "已启用" : "已停用"}
        </span>
      </td>
      <td className="px-3 py-2">
        <div className="flex flex-wrap justify-end gap-2">
          <Button busy={busy} onClick={onTest}>
            测试
          </Button>
          <Button busy={busy} onClick={onToggle}>
            {model.enabled ? "停用" : "启用"}
          </Button>
          {model.modelType === "embedding" ? (
            isGlobalDefault ? (
              <Button busy={busy} onClick={onUnbind}>
                取消默认
              </Button>
            ) : (
              <Button busy={busy} disabled={!model.enabled} onClick={onBind}>
                设为默认
              </Button>
            )
          ) : null}
          <Button variant="danger" busy={busy} onClick={onDelete}>
            删除
          </Button>
        </div>
      </td>
    </tr>
  );
}
