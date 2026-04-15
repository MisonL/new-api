# Electron 桌面端说明

本目录是 `new-api` 的 Electron 封装，用于提供带托盘能力的桌面版应用，支持 Windows、macOS、Linux。

## 前置要求

### 1. Go 二进制文件

Electron 启动时依赖上级目录中的 `new-api` 可执行文件。

如果已经有现成二进制，可直接复制：

```bash
cp ../new-api-macos ../new-api
```

如果没有，需要先在项目根目录完成后端构建。

### 2. 安装依赖

```bash
cd electron
npm install
```

## 开发运行

```bash
npm start
```

启动后会：

- 在 `3000` 端口启动 Go 后端
- 打开 Electron 窗口
- 创建系统托盘图标
- 将开发环境数据库写入 `../data/new-api.db`

## 构建

```bash
ls ../new-api
npm run build
npm run build:mac
npm run build:win
npm run build:linux
```

构建产物输出到 `electron/dist/`：

- macOS：`.dmg`、`.zip`
- Windows：`.exe`
- Linux：`.AppImage`、`.deb`

## 配置

### 端口

默认端口是 `3000`。如需修改，可编辑 `main.js` 中的 `PORT` 常量。

### 数据库位置

- 开发环境：`../data/new-api.db`
- 生产环境：
  - macOS：`~/Library/Application Support/New API/data/`
  - Windows：`%APPDATA%/New API/data/`
  - Linux：`~/.config/New API/data/`
