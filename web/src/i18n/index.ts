import { createElement, Fragment, type ReactNode } from "react";
import { en } from "./en.ts";
import { ko } from "./ko.ts";

export type Lang = "en" | "ko";
export type MessageKey = keyof typeof ko;
/** `${K}_one`이 함께 있는 키. tn()이 수(단수·복수)에 따라 고른다. */
export type PluralKey = { [K in MessageKey]: `${K}_one` extends MessageKey ? K : never }[MessageKey];

const DICTS: Record<Lang, Record<MessageKey, string>> = { en, ko };

let current: Lang | null = null;

/** 문서의 <html lang>으로 언어를 정한다. "ko"면 한국어, 그 밖(값이 없거나 DOM이 없는 테스트 포함)은 영어. */
function detect(): Lang {
  const l = typeof document === "undefined" ? "" : document.documentElement.lang;
  return l.toLowerCase() === "ko" ? "ko" : "en";
}

/** 현재 언어. 페이지를 불러올 때 한 번 정해지고 바뀌지 않는다. */
export function lang(): Lang {
  current ??= detect();
  return current;
}

/** 언어를 바꾼다. 테스트에서만 쓴다(화면은 새로 그리지 않는다). */
export function setLanguage(l: Lang): void {
  current = l;
}

/** Intl에 넘길 로캘. */
export function locale(l: Lang = lang()): string {
  return l === "ko" ? "ko-KR" : "en-US";
}

export type Vars = Readonly<Record<string, string | number>>;

const PLACEHOLDER = /\{(\w+)\}/g;

function fill(msg: string, vars?: Vars): string {
  if (!vars) return msg;
  return msg.replace(PLACEHOLDER, (m, name: string) => (name in vars ? String(vars[name]) : m));
}

/** 메시지 문구. {name} 자리는 vars 값으로 바꾼다. */
export function t(key: MessageKey, vars?: Vars): string {
  return fill(DICTS[lang()][key], vars);
}

function pluralKey(key: PluralKey, n: number): MessageKey {
  return new Intl.PluralRules(locale()).select(n) === "one" ? (`${key}_one` as MessageKey) : key;
}

/** 수에 따라 단수(`${key}_one`)나 복수(key) 문구를 고른다. {n}에는 n이 들어간다. */
export function tn(key: PluralKey, n: number, vars?: Vars): string {
  return t(pluralKey(key, n), { n, ...vars });
}

// ---- 서식 있는 문구 ----
// 메시지 안의 <b>..</b>(→ <strong>)와 <code>..</code>만 요소로 만든다. 나머지 꺾쇠는 글자 그대로다.
// HTML로 해석하지 않고 React 요소를 만들기 때문에 vars 값에 태그가 있어도 글자로 보인다.

export type RichNode = string | { var: string } | { tag: "b" | "code"; children: RichNode[] };

const TAG = /<(\/?)(b|code)>/g;

/** 메시지를 글자·자리·태그 트리로 나눈다. 짝이 맞지 않는 닫는 태그는 글자로 남기고, 닫히지 않은 태그는 끝에서 닫는다. */
export function parseRich(msg: string): RichNode[] {
  const root: RichNode[] = [];
  const stack: { tag: "b" | "code"; children: RichNode[] }[] = [];
  const out = () => stack[stack.length - 1]?.children ?? root;
  const text = (s: string) => {
    let last = 0;
    for (const m of s.matchAll(PLACEHOLDER)) {
      if (m.index > last) out().push(s.slice(last, m.index));
      out().push({ var: m[1] ?? "" });
      last = m.index + m[0].length;
    }
    if (last < s.length) out().push(s.slice(last));
  };
  let last = 0;
  for (const m of msg.matchAll(TAG)) {
    const tag = m[2] as "b" | "code";
    const closing = m[1] === "/";
    if (closing && stack[stack.length - 1]?.tag !== tag) continue; // 글자로 남긴다
    text(msg.slice(last, m.index));
    last = m.index + m[0].length;
    if (closing) {
      stack.pop();
    } else {
      const node = { tag, children: [] as RichNode[] };
      out().push(node);
      stack.push(node);
    }
  }
  text(msg.slice(last));
  return root;
}

export type RichVars = Readonly<Record<string, ReactNode>>;

function render(nodes: readonly RichNode[], vars: RichVars): ReactNode[] {
  return nodes.map((n) => {
    if (typeof n === "string") return n;
    if ("var" in n) return n.var in vars ? vars[n.var] : `{${n.var}}`;
    return createElement(n.tag === "b" ? "strong" : "code", null, ...render(n.children, vars));
  });
}

/** <b>·<code>가 든 메시지를 React 요소로 보여 준다. vars 값은 글자나 React 요소(링크 등)다. */
export function T({ k, vars = {} }: { k: MessageKey; vars?: RichVars }): ReactNode {
  return createElement(Fragment, null, ...render(parseRich(DICTS[lang()][k]), vars));
}

export const DICTIONARIES: Readonly<Record<Lang, Readonly<Record<MessageKey, string>>>> = DICTS;
