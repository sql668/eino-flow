"use client";

import { useEffect, useState } from "react";

import { Field, Input } from "@/components/ui/field";

// 自定义分隔符输入：一个输入框，多个分隔符用英文逗号隔开。
//
// 抽成组件的原因：分段工作台与知识库设置页都要用，且「怎么切分输入」这件事
// 必须和后端的规范一致（见 model.normalizeSeparators）。两边各写一份，
// 迟早出现「工作台能填、设置页填了没用」这种不一致。
//
// 用户可见的语法只有两条：
//   - 英文逗号分隔多条；
//   - 分隔符本身是逗号时写成 \,
// \n、\t 这类字符转义交给后端还原（界面上保持「看得见的写法」更好核对），
// 因此这里原样透传。

export interface SeparatorFieldProps {
  value: string[];
  onChange: (separators: string[]) => void;
  disabled?: boolean;
}

export function SeparatorField({ value, onChange, disabled = false }: SeparatorFieldProps) {
  // 输入框里存的是「原始文本」而不是数组：用户刚敲完 \,
  // 时如果立刻按数组重新渲染，输入内容会被吃掉。
  const [text, setText] = useState(() => formatSeparators(value));

  // 外部值变化（恢复默认、切换文档）时同步文本；
  // 与当前文本等价时不覆盖，否则用户每敲一个字符都会被重排一遍。
  useEffect(() => {
    if (!isSameSeparators(parseSeparators(text), value)) {
      setText(formatSeparators(value));
    }
    // text 刻意不进依赖：它只在「外部值变了」这一刻用于比较。
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [value]);

  const parsed = parseSeparators(text);
  const emptyButTyped = text.trim() !== "" && parsed.length === 0;

  return (
    <Field
      label="自定义分隔符"
      required
      hint={
        <>
          多个分隔符用英文逗号隔开，例如 <code className="reading">\n\n, ###</code>
          （换行写作 <code className="reading">\n</code>，制表符 <code className="reading">\t</code>）。
          分隔符本身是逗号时写成 <code className="reading">\,</code>。
          切分按「长分隔符优先」，因此 <code className="reading">\n\n</code> 不会被{" "}
          <code className="reading">\n</code> 抢先切开。
        </>
      }
    >
      <Input
        value={text}
        disabled={disabled}
        placeholder={String.raw`\n\n, ###`}
        onChange={(event) => {
          setText(event.target.value);
          onChange(parseSeparators(event.target.value));
        }}
      />
      {emptyButTyped ? (
        <span className="mt-1 block text-[11px] leading-5 text-warn">
          只看到逗号、没有分隔符内容：如果要用英文逗号本身当分隔符，请写成{" "}
          <code className="reading">\,</code>
        </span>
      ) : null}
    </Field>
  );
}

// formatSeparators 把分隔符数组还原成输入框里的文本。
export function formatSeparators(separators: string[] | undefined): string {
  return (separators ?? []).join(", ");
}

// parseSeparators 把输入文本拆成分隔符数组。
//
// 只处理「列表语法」：按未被转义的英文逗号拆分。字符转义（\n / \t / \\ / \,）
// 由后端统一还原——两处都做会让人分不清到底哪边生效。
export function parseSeparators(text: string): string[] {
  const items: string[] = [];
  let current = "";
  const runes = Array.from(text);
  for (let i = 0; i < runes.length; i++) {
    const char = runes[i];
    // 反斜杠连同后一个字符一起保留：\, 不能被当成逗号分隔点。
    if (char === "\\" && i + 1 < runes.length) {
      current += char + runes[i + 1];
      i++;
      continue;
    }
    if (char === ",") {
      items.push(current);
      current = "";
      continue;
    }
    current += char;
  }
  items.push(current);
  return items.map((item) => item.trim()).filter((item) => item !== "");
}

// isSameSeparators 判断两个分隔符列表是否等价（顺序敏感，与切分优先级有关）。
function isSameSeparators(left: string[], right: string[] | undefined): boolean {
  const target = right ?? [];
  if (left.length !== target.length) {
    return false;
  }
  return left.every((item, index) => item === target[index]);
}
