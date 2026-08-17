import assert from "node:assert/strict";
import test from "node:test";

import {
  relatedGroupLabel,
  uniqueRelatedWorks,
} from "../src/lib/relatedWorksPresentation.ts";

test("关联作品排除当前作品和重复候选", () => {
  const items = [
    { candidate_id: "current", relation_label: "作者甲" },
    { candidate_id: "work-1", relation_label: "作者甲" },
    { candidate_id: "work-1", relation_label: "作者甲" },
    { candidate_id: "work-2", relation_label: "作者甲" },
  ];
  assert.deepEqual(uniqueRelatedWorks(items, "current").map((item) => item.candidate_id), ["work-1", "work-2"]);
});

test("关联标题只在服务端依据一致时展示具体名称", () => {
  assert.equal(relatedGroupLabel([
    { candidate_id: "work-1", relation_label: "作者甲" },
    { candidate_id: "work-2", relation_label: "作者甲" },
  ]), "作者甲");
  assert.equal(relatedGroupLabel([
    { candidate_id: "work-1", relation_label: "作者甲" },
    { candidate_id: "work-2" },
  ]), "");
  assert.equal(relatedGroupLabel([
    { candidate_id: "work-1", relation_label: "作者甲" },
    { candidate_id: "work-2", relation_label: "作者乙" },
  ]), "");
});
