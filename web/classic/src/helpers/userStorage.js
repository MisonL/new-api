/*
Copyright (C) 2025 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/

// 读取本地缓存的登录用户 id，供请求头 New-API-User 使用。
//
// 调用方是 apiCore.js 的模块顶层（axios.create 的默认请求头），因此这里任何
// 抛错都会让整个模块加载失败、界面白屏。localStorage 可能被写坏、被其他页面
// 版本改过结构，或在受限环境里访问即抛异常；一律降级成"未登录"（-1），与缓存
// 不存在时的返回值一致。
//
// storage 参数供测试注入；默认取运行时 localStorage。

export function getUserIdFromLocalStorage(storage = globalThis.localStorage) {
  let raw;
  try {
    raw = storage ? storage.getItem("user") : null;
  } catch {
    return -1;
  }
  if (!raw) {
    return -1;
  }
  try {
    const user = JSON.parse(raw);
    const id = Number(user && user.id);
    return Number.isInteger(id) ? id : -1;
  } catch {
    return -1;
  }
}
