// CSS 结构检查：捕获拆分/合并样式表时静默产生的破损规则。
//
// 浏览器对 CSS 有容错解析，一条被截断的规则（缺右花括号）不会报错，
// 只会把后续规则整段吞进同一个块里，导致这些规则全部失效——页面大面积走样
// 却没有任何构建错误。本脚本按 CSS 词法规则做一次严格校验：
//   1. 花括号必须配平，不能出现游离的声明或多余的右花括号；
//   2. 普通规则块内不允许再出现嵌套块（@media 等条件组规则除外）；
//   3. 每个选择器都必须非空。
import { readFileSync, readdirSync } from "node:fs";
import { join } from "node:path";
import { pathToFileURL } from "node:url";

const CSS_DIR = "web/static/css";

function stripComments(source) {
  return source.replace(/\/\*[\s\S]*?\*\//g, "");
}

/** @returns {string[]} 问题列表，空数组表示通过 */
export function checkCss(source) {
  const problems = [];
  const clean = stripComments(source);
  /** @type {{ selector: string, line: number, nested: number, isGroup: boolean }[]} */
  const stack = [];
  let depth = 0;
  let selectorStart = 0;
  let selectorLine = 1;
  let line = 1;

  for (let i = 0; i < clean.length; i++) {
    const ch = clean[i];
    if (ch === "\n") line++;

    if (ch === "{") {
      const selector = clean.slice(selectorStart, i).trim().replace(/\s+/g, " ");
      if (selector === "") {
        problems.push(`第 ${selectorLine} 行：花括号前缺少选择器`);
      }
      const isGroup = selector.startsWith("@");
      stack.push({ selector, line: selectorLine, nested: 0, isGroup });
      if (stack.length > 1) stack[stack.length - 2].nested++;
      depth++;
    } else if (ch === "}") {
      const block = stack.pop();
      depth--;
      if (!block) {
        problems.push(`第 ${line} 行：多余的右花括号`);
        depth = 0;
        selectorStart = i + 1;
        selectorLine = line;
        continue;
      }
      // 普通规则块里出现嵌套块，说明这条规则被截断，吞掉了后面的规则
      if (!block.isGroup && block.nested > 0) {
        problems.push(
          `第 ${block.line} 行：规则 "${block.selector.slice(0, 60)}" 未闭合，` +
            `吞掉了后续 ${block.nested} 条规则`,
        );
      }
      if (depth === 0) {
        selectorStart = i + 1;
        selectorLine = line;
      }
    } else if (ch === ";" && depth === 0) {
      const fragment = clean.slice(selectorStart, i).trim();
      if (fragment !== "" && !fragment.startsWith("@")) {
        problems.push(
          `第 ${selectorLine} 行：顶层出现游离声明 ${JSON.stringify(fragment.slice(0, 60))}`,
        );
      }
      selectorStart = i + 1;
      selectorLine = line;
    }

    if (depth === 0 && ch !== ";" && ch !== "}" && ch !== "{" && !/\s/.test(ch)) {
      if (selectorStart === i) selectorLine = line;
    }
    if (depth === 0 && (ch === ";" || ch === "}")) {
      selectorStart = i + 1;
      selectorLine = line;
    }
  }

  if (depth !== 0) {
    problems.push(`文件末尾仍有 ${depth} 个未闭合的花括号`);
  }
  const tail = clean.slice(selectorStart).trim();
  if (tail !== "" && !tail.startsWith("@")) {
    problems.push(`文件末尾有无法解析的内容 ${JSON.stringify(tail.slice(0, 60))}`);
  }
  return problems;
}

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  const files = readdirSync(CSS_DIR).filter((name) => name.endsWith(".css"));
  let failed = 0;
  for (const name of files) {
    const problems = checkCss(readFileSync(join(CSS_DIR, name), "utf8"));
    if (problems.length > 0) {
      failed += problems.length;
      console.error(`${name}:`);
      for (const problem of problems) console.error(`  - ${problem}`);
    }
  }
  if (failed > 0) {
    console.error(`\nCSS 结构检查未通过，共 ${failed} 处问题`);
    process.exit(1);
  }
  console.log(`CSS 结构检查通过（${files.length} 个文件）`);
}
