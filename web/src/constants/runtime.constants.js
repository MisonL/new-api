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

export const IS_READONLY_FRONTEND =
  import.meta.env.VITE_REACT_APP_READONLY_MODE === 'true';

export const READONLY_FRONTEND_MESSAGE =
  import.meta.env.VITE_REACT_APP_READONLY_MESSAGE ||
  '当前是只读前端联调环境：仅允许查看正式后端数据，已阻止登录、登出、绑定和所有写入请求。';
