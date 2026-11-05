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

// GET 去重：把同一 URL 的并发请求合并成一个 Promise，避免首屏同时拉取同一份
// 数据。执行顺序与豁免条件保持与 web/default 的 api.ts 一致：
//
// - disableDuplicate 显式退出合并。
// - 带 AbortSignal 的请求也必须退出：共享 Promise 无法承载两个独立的取消
//   生命周期——加入方的 signal 永远传不到实际请求上，创建方取消又会连带取消
//   所有加入方。classic 此前缺少这条豁免，只是现有调用点恰好都同时传了
//   disableDuplicate，属机制缺口而不是已满足的契约。
//
// 依赖由调用方注入，模块本身不 import axios，便于 node --test 直接验证。

export function installGetDeduplication(instance) {
  const originalGet = instance.get.bind(instance);
  const inFlightGetRequests = new Map();

  const genKey = (url, config = {}) => {
    const params = config.params ? JSON.stringify(config.params) : "{}";
    return `${url}?${params}`;
  };

  instance.get = (url, config = {}) => {
    if (config?.disableDuplicate) {
      return originalGet(url, config);
    }
    if (config?.signal) {
      return originalGet(url, config);
    }

    const key = genKey(url, config);
    if (inFlightGetRequests.has(key)) {
      return inFlightGetRequests.get(key);
    }

    const reqPromise = originalGet(url, config).finally(() => {
      inFlightGetRequests.delete(key);
    });

    inFlightGetRequests.set(key, reqPromise);
    return reqPromise;
  };
}
