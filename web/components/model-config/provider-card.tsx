"use client";

import { useState } from "react";

import { Button } from "@/components/ui/button";
import { Card, Field, Input } from "@/components/ui/field";
import { useModelConfigStore } from "@/stores/model-config-store";
import type { ModelProviderInfo, ProbeResultInfo } from "@/types/api";

// 单个厂商的配置卡片。
//
// 交互顺序刻意做成「先测通再保存」：测试支持带未保存的凭证，
// 这样密钥写错时不会先落一份坏配置，也避免测试本身产生垃圾数据。

const kindLabels: Record<string, string> = {
  llm: "对话",
  embedding: "向量化",
  rerank: "重排",
};

export function ProviderCard({ provider }: { provider: ModelProviderInfo }) {
  const pending = useModelConfigStore((state) => state.pending);
  const saveProvider = useModelConfigStore((state) => state.saveProvider);
  const deleteProvider = useModelConfigStore((state) => state.deleteProvider);
  const testProvider = useModelConfigStore((state) => state.testProvider);

  // 表单是纯局部状态：只在卡片内部使用，不需要进 store。
  const [baseUrl, setBaseUrl] = useState(provider.baseUrl);
  const [credentials, setCredentials] = useState<Record<string, string>>({});
  const [probe, setProbe] = useState<ProbeResultInfo | null>(null);

  const busy = pending === `provider:${provider.provider}`;
  // 凭证输入框留空表示「不改动已存凭证」，因此提交时要区分「空对象」与「全空值」。
  const credentialPayload = () => {
    const filled = Object.fromEntries(
      Object.entries(credentials).filter(([, value]) => value.trim() !== ""),
    );
    return Object.keys(filled).length > 0 ? filled : undefined;
  };

  const handleTest = async () => {
    setProbe(null);
    const result = await testProvider(provider.provider, {
      baseUrl,
      credentials: credentialPayload(),
    });
    if (result) {
      setProbe(result);
    }
  };

  const handleSave = async () => {
    setProbe(null);
    const ok = await saveProvider(provider.provider, {
      baseUrl,
      credentials: credentialPayload(),
    });
    if (ok) {
      // 保存成功后清空输入框：界面上改为展示掩码，避免明文停留在 DOM 里。
      setCredentials({});
    }
  };

  return (
    <Card
      title={provider.displayName}
      description={
        <span>
          支持：{provider.kinds.map((kind) => kindLabels[kind] ?? kind).join(" / ")}
          {provider.configured ? (
            <span className="ml-2 rounded bg-emerald-50 px-1.5 py-0.5 text-emerald-700">
              已配置
            </span>
          ) : (
            <span className="ml-2 rounded bg-neutral-100 px-1.5 py-0.5 text-neutral-500">
              未配置
            </span>
          )}
        </span>
      }
      actions={
        provider.configured ? (
          <Button
            variant="danger"
            busy={busy}
            onClick={() => {
              // 删除是不可逆动作，先确认再执行。
              if (window.confirm(`确定删除「${provider.displayName}」的配置吗？`)) {
                void deleteProvider(provider.provider);
              }
            }}
          >
            删除配置
          </Button>
        ) : null
      }
    >
      <div className="flex flex-col gap-3">
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
                : "凭证只以密文保存，接口只回掩码"
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
          <Button busy={busy} onClick={handleTest}>
            测试连接
          </Button>
          <Button variant="primary" busy={busy} onClick={handleSave}>
            保存配置
          </Button>
        </div>

        {probe ? (
          <p
            className={`rounded-md px-3 py-2 text-xs leading-5 ${
              probe.ok
                ? "bg-emerald-50 text-emerald-800"
                : "bg-amber-50 text-amber-800"
            }`}
          >
            {probe.message}
            {probe.latencyMs > 0 ? `（${probe.latencyMs} ms）` : ""}
          </p>
        ) : null}
      </div>
    </Card>
  );
}
