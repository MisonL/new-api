# 历史分支恢复审计

## 范围

- 仓库：`new-api`
- 审计日期：`2026-04-11`
- 基线分支：`main`
- 复核目标引用：
  - `origin/feat/auth-cas-sso`
  - `origin/feat/auth-cas-sso-phase3`
  - `origin/feat/auth-jwt-sso-mvp`
  - `origin/feat/dalle-extra`
  - `origin/feat/suno`

## 方法

- 核实用户历史提交过 PR 的上游分支，确认哪些已经成为 `main` 的祖先提交。
- 使用 `git merge-base`、`git log`、`git diff --name-status` 以及关键文件差异，对 `main` 和目标分支逐一对比。
- 以当前代码和测试为事实基线，以 Git 历史为观测依据，只对剩余缺口施加最小改动。

## 发现

### 1. 历史上游 PR 分支

用户历史上真正作为上游 PR head 使用过的 12 个分支，都已经是 `main` 的祖先提交。这一组不需要继续恢复。

### 2. 截图中涉及的认证分支

#### `origin/feat/auth-jwt-sso-mvp`

- 这是较早期的 `JWT Direct` 阶段分支。
- 与 `main` 直接对比可见，它缺少后续补上的浏览器回调加固、`trusted_header`、CAS 支持以及补充测试。
- 结论：不要合并该分支，`main` 已经整体覆盖并超越它。

#### `origin/feat/auth-cas-sso`

- 这个分支位于 `JWT Direct MVP` 和后续 `trusted_header` / CAS 打磨阶段之间。
- 与 `main` 直接对比可见，如果原样合入，会回退较新的浏览器回调和 CAS 代码。
- 结论：不要合并该分支，`main` 已经整体覆盖并超越它。

#### `origin/feat/auth-cas-sso-phase3`

- 该分支包含较成熟的认证能力线：`JWT Direct`、`Trusted Header`、CAS。
- 当前 `main` 已经具备 CAS、`Trusted Header`、`JWT Direct` 的核心运行文件和路由。
- 审计时识别出的剩余差异：
  - README 当时没有明确列出 CAS 支持。
  - `validateCustomOAuthProvider` 中 CAS 后端默认 external ID 映射未恢复。
  - `main` 缺少该阶段分支中的 CAS 定向测试。
- 结论：只恢复剩余缺口，不整分支合并。

### 3. 长生命周期功能分叉

#### `origin/feat/dalle-extra`

- 该引用是长期分叉分支，与 `main` 相比已经漂移出大量提交。
- 不适合直接合并，也不适合做整体恢复。

#### `origin/feat/suno`

- 该引用同样是长期分叉分支，历史漂移非常大。
- 不适合直接合并，也不适合做整体恢复。

## 已做的最小恢复

- 恢复了后端创建 CAS Provider 时的默认 external ID 映射。
- 恢复了面向 CAS 的单元测试和控制器测试。
- 更新了 README 中企业 SSO 相关说明，使其与当时的 `main` 能力一致。

## 门禁结论

- `origin/feat/auth-jwt-sso-mvp`：拒绝合并，已被 `main` 覆盖
- `origin/feat/auth-cas-sso`：拒绝合并，已被 `main` 覆盖
- `origin/feat/auth-cas-sso-phase3`：通过最小补丁方式部分恢复
- `origin/feat/dalle-extra`：暂不处理，需后续单独拆解
- `origin/feat/suno`：暂不处理，需后续单独拆解
