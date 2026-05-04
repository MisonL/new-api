import test from "node:test";
import assert from "node:assert/strict";

import {
  ADMIN_USERNAME_MAX_LENGTH,
  validateAdminSetupValues,
} from "../classic/src/components/setup/setupValidation.js";

test("validateAdminSetupValues counts unicode characters instead of UTF-16 code units", () => {
  assert.equal(
    validateAdminSetupValues({
      username: "𠮷".repeat(ADMIN_USERNAME_MAX_LENGTH),
      password: "DesktopTest123!",
      confirmPassword: "DesktopTest123!",
    }),
    null,
  );

  assert.deepEqual(
    validateAdminSetupValues({
      username: "𠮷".repeat(ADMIN_USERNAME_MAX_LENGTH + 1),
      password: "DesktopTest123!",
      confirmPassword: "DesktopTest123!",
    }),
    {
      key: "用户名长度不能超过{{max}}个字符",
      params: { max: ADMIN_USERNAME_MAX_LENGTH },
    },
  );
});
