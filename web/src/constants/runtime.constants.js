export const IS_READONLY_FRONTEND =
  import.meta.env.VITE_REACT_APP_READONLY_MODE === 'true';

export const READONLY_FRONTEND_MESSAGE =
  import.meta.env.VITE_REACT_APP_READONLY_MESSAGE ||
  '当前是只读前端联调环境：仅允许查看正式后端数据，已阻止登录、登出、绑定和所有写入请求。';
