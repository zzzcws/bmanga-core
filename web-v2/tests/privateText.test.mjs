import assert from "node:assert/strict";
import test from "node:test";

import { sanitizePrivateText } from "../src/lib/privateText.ts";

function windowsFixture(...segments) {
  return ["C:", "library", "root", ...segments].join("\\");
}

function windowsSlashFixture(...segments) {
  return ["C:", "library", "root", ...segments].join("/");
}

function uncFixture(...segments) {
  return ["", "", "storage.example.invalid", "fixture", ...segments].join("\\");
}

function posixFixture(...segments) {
  return ["", "example", "fixture", ...segments].join("/");
}

function fileFixture(...segments) {
  return ["file:", "", "", "example", "fixture", ...segments].join("/");
}

function shareFixture(...segments) {
  return ["smb:", "", "storage.example.invalid", "fixture", ...segments].join("/");
}

test("遮蔽 Windows 与 UNC 绝对路径", () => {
  const result = sanitizePrivateText(`缓存 ${windowsFixture("cache")}；镜像 ${uncFixture("book.cbz")}。 `);
  assert.equal(result, "缓存 本地路径；镜像 本地路径。");
});

test("遮蔽正斜杠 Windows 与 POSIX 路径", () => {
  const result = sanitizePrivateText(
    `缓存 ${windowsSlashFixture("cache")}；样例：${posixFixture("library")}。root=${posixFixture("private")}，镜像,${posixFixture("cache")}。 `,
  );
  assert.equal(result, "缓存 本地路径；样例：本地路径。root=本地路径，镜像,本地路径。");
});

test("完整遮蔽含空格与括号的本地路径", () => {
  const result = sanitizePrivateText(
    `缓存 ${windowsFixture("Private Books", "Book (Final)", "fixture.cbz")}；样例：${posixFixture("Private Library", "(2026)", "book.cbz")}。 `,
  );
  assert.equal(result, "缓存 本地路径；样例：本地路径。");
});

test("保留包围路径的括号并遮蔽含空格的本地协议", () => {
  const result = sanitizePrivateText(
    `说明（${windowsFixture("Private Books", "book (final).cbz")}）保留；文件 ${fileFixture("Private Library", "book.cbz")}；共享 ${shareFixture("Private Share", "book.cbz")}。 `,
  );
  assert.equal(result, "说明（本地路径）保留；文件 本地路径；共享 本地路径。");
});

test("区分外部地址与本地协议", () => {
  const result = sanitizePrivateText(
    `参考 https://example.test/run；文件 ${fileFixture("a")}；共享 ${shareFixture("private")}。 `,
  );
  assert.equal(result, "参考 外部地址；文件 本地路径；共享 本地路径。");
});

test("不误伤普通斜杠文本并限制长度", () => {
  assert.equal(sanitizePrivateText("标题/作者 · 1/2"), "标题/作者 · 1/2");
  assert.equal(sanitizePrivateText("请选择 全部 / 同人 / 漫画（2026）"), "请选择 全部 / 同人 / 漫画（2026）");
  assert.equal(sanitizePrivateText("abcdef", "", 3), "abc");
});
