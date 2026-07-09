import test from "node:test";
import assert from "node:assert/strict";

import { normalizeHeaderTemplateContent } from "../classic/src/helpers/headerOverrideUserAgent.js";

test("normalize header template content rejects non-object json", () => {
  assert.deepEqual(
    normalizeHeaderTemplateContent("[]", { allowEmpty: false }),
    {
      ok: false,
      message: "请求头覆盖必须是 JSON 对象！",
    },
  );
});

test("normalize header template content formats object json", () => {
  assert.deepEqual(
    normalizeHeaderTemplateContent('{"Authorization":"Bearer {api_key}"}', {
      allowEmpty: false,
    }),
    {
      ok: true,
      value: JSON.stringify(
        {
          Authorization: "Bearer {api_key}",
        },
        null,
        2,
      ),
    },
  );
});
