# Rom-Updater

ROM OTA 更新服务端，Go 标准库实现，无第三方 Go 依赖。

- `GET /ota/{device}/{channel}.json`：查询当前发布的版本
- `GET|HEAD /ota/files/{file}`：下载 OTA 包，支持 `Range`/206、`Content-Range`、`Accept-Ranges`、`If-Range`；ETag 为包的 SHA-256
- `GET /admin/`：网页管理后台，可上传、发布、编辑、下架、删除包（需设置 `OTA_ADMIN_TOKEN`）
- `PUT /admin/files/{file}.zip`、`DELETE /admin/files/{file}`：上传、删除包（被发布引用的包不能删除）
- `GET /admin/state`、`POST /admin/releases`、`DELETE /admin/releases/{device}/{channel}`：查看状态、发布、下架

`build_timestamp`、`incremental`、`type` 从包内 `META-INF/com/android/metadata` 读取（`post-timestamp`、`post-build-incremental`、`pre-build`），`size`、`sha256` 由服务端计算，不需要手填。

## 运行

### Docker Compose

```bash
cp .env.example .env   # 填写 OTA_BASE_URL 和 OTA_ADMIN_TOKEN
docker compose up -d
```

`compose.yaml` 只把端口绑定在 `127.0.0.1:8080`，由本机的 HTTPS 反向代理转发；数据存放在命名卷 `ota-data`。升级：`docker compose pull && docker compose up -d`。

默认使用完整版本号 `0.2.3`。升级到其他版本时，在 `.env` 设置 `OTA_IMAGE_TAG=X.Y.Z` 后再执行升级命令。正式镜像仅由 `vX.Y.Z` Git 标签发布，对应镜像标签 `X.Y.Z`；`latest` 与 `X.Y` 是可变别名。普通 `main` 推送和拉取请求仅执行构建检查，不自动发布 `edge` 镜像。

从当前源码构建并本地预览时，将 `.env` 的 `OTA_IMAGE_TAG` 设为 `local`，`OTA_BASE_URL` 设为本地地址（例如 `http://localhost:8080`），然后执行：

```bash
docker build -t ghcr.io/night-stars-1/rom-updater:local .
docker compose up -d --pull never
```

Docker 构建会自动安装前端依赖、生成页面资源并嵌入 Go 二进制，无需在宿主机安装 Node.js。此 HTTP 配置只用于本地预览，生产环境仍需 HTTPS。

### Docker

```bash
docker run -d --name ota -p 8080:8080 \
  -v ota-data:/data \
  -e OTA_ADMIN_TOKEN=换成随机长字符串 \
  ghcr.io/night-stars-1/rom-updater:0.2.3 \
  -base-url https://ota.example.com
```

容器监听 8080 端口、使用纯 HTTP，需在前面放 HTTPS 反向代理。数据目录 `/data`：

```
/data/releases.json   发布清单（不存在时为空，可通过发布接口生成）
/data/files/*.zip     OTA 包
```

### 直接运行

从源码构建需要 Node.js 22.12+（推荐 22 LTS）和 npm。先生成 `web/dist/`，Go 才能嵌入管理界面；前端依赖版本由 `web/package-lock.json` 锁定。

```bash
npm --prefix web ci
npm --prefix web run build
go build -o ota-server .
OTA_ADMIN_TOKEN=... ./ota-server -base-url https://ota.example.com \
  -tls-cert cert.pem -tls-key key.pem -addr :443
```

| 参数 | 默认值 | 说明 |
|---|---|---|
| `-base-url` | 必填 | 对外地址，用于生成包下载 URL |
| `-addr` | `:8443` | 监听地址 |
| `-manifest` | `releases.json` | 发布清单 |
| `-files` | `files` | OTA 包目录 |
| `-tls-cert` / `-tls-key` | 空 | 证书；不填则为纯 HTTP |
| `-max-upload` | 8 GiB | 上传大小上限（字节） |

环境变量 `OTA_ADMIN_TOKEN` 未设置时，管理接口不注册（返回 404）。

### 前端开发

管理界面源码位于 `web/src/`，使用 Vue 3、TypeScript、Naive UI 和 Vite。先按上方步骤安装依赖并构建一次前端，再用两个终端启动：

```bash
# 终端一：本地 Go 管理 API（填写管理令牌）
OTA_ADMIN_TOKEN=... go run . -addr 127.0.0.1:8080 -base-url http://localhost:8080

# 终端二：Vite 热更新
npm --prefix web run dev
```

打开 `http://127.0.0.1:5173/admin/`。Vite 只将管理 API 请求代理到 `127.0.0.1:8080`；生产资源使用 `/admin/` 作为基础路径。`web/node_modules/` 和 `web/dist/` 不提交到版本库，CI 在 Go 检查前构建前端。

## 发布新 ROM

最简单的方式是打开 `https://ota.example.com/admin/`，用 `OTA_ADMIN_TOKEN` 登录后上传并发布。

登录页只需输入 `OTA_ADMIN_TOKEN` 管理令牌，固定账号 `admin` 仅作为浏览器凭据的用户名，无需填写。认证成功后，支持 Credential Management API 的浏览器会收到密码保存请求；其他浏览器通过标准用户名 / 密码字段识别凭据。是否显示保存提示由浏览器设置和站点保存策略决定，生产环境应使用 HTTPS。登录会话仍只保存在当前标签页的 `sessionStorage`，退出会清除；密码管理器中保存的凭据由浏览器管理。

管理后台使用 [Vue 3](https://vuejs.org/) 与 [Naive UI](https://www.naiveui.com/)，采用“版本 / 文件”两个视图，数量直接显示在切换标签上，不再常驻侧栏或统计卡。列表显示关键字段，更新说明、SHA-256 和完整元数据通过行内“详情”展开。上传在弹窗中完成；关闭窗口不会中断正在上传的文件，可通过工具条的“上传中”入口重新查看进度或取消，成功后自动进入发布窗口。生产构建输出到 `web/dist/`，随 Go 二进制嵌入；CSS / JavaScript 通过 `/admin/assets/` 同源提供，哈希资源可长期缓存，无外部 CDN 或字体服务依赖。Vue 与 Naive UI 的许可保留在 `web/public/assets/licenses.txt`。

上传窗口使用 `NUpload / NUploadDragger`，可拖入 ZIP 或点击选择文件；每次选择一个文件，再次选择会替换当前文件。拖入后不会自动提交，可先填写可选 SHA-256，再点击“上传”。上传期间选择器禁用，原有流式传输、进度、取消与上传后自动进入发布的行为保持不变。

新建发布的“设备 / 通道”使用 `NAutoComplete`：可以自由输入新值，也可筛选并选择已有值。设备候选来自当前发布记录及上传包的设备元数据；通道候选来自当前发布记录，优先排列所选设备用过的通道，其他已有通道仍可选。编辑现有发布时，设备和通道作为主键继续只读。

界面跟随系统浅色 / 深色模式，窄屏列表可局部横向滚动。列表数量与更新时间来自最近一次成功加载的清单；发布、上传、下架和删除后会重新加载，刷新按钮可手动同步。异常提示持续保留，不代表实时服务监控。

登录的空令牌、认证成功 / 失败、退出、手动刷新、上传取消及操作结果统一使用右上角 Naive UI 通知，支持手动关闭；普通通知默认显示 3 秒，失败通知默认显示 8 秒。通知在发布与确认窗口上方也可操作，并提供屏幕阅读器播报。发布 / 上传字段校验、包与设备不匹配警告和清单异常继续显示在对应表单或页面内。

页面的可见交互使用 Naive UI：视图切换为 `NTabs / NTab`，列表为 `NTable`，构建时间为 `NDatePicker` 的 `datetime` 面板，不使用手写日历或浏览器原生日期 / 时间输入。日期和时分秒可选择或手动输入，支持“确认 / 清除 / 此刻”；Escape 逐层关闭时间菜单和日期面板，不误关发布窗口。显示格式为 `yyyy-MM-dd HH:mm:ss`，内部保留本地时间字符串并在发布时转换为 Unix 秒；无效日期或夏令时跳过的时间不会被归一化或沿用旧值发布。系统文件选择窗口和上传期间离开页面的安全提示仍由浏览器提供。

命令行方式：

1. 上传包，`sha256` 可选，填写后服务端会校验：

   ```bash
   curl -T uwu_diting-20261008.zip \
     -H "Authorization: Bearer $OTA_ADMIN_TOKEN" \
     "https://ota.example.com/admin/files/uwu_diting-20261008.zip?sha256=$(sha256sum uwu_diting-20261008.zip | cut -d' ' -f1)"
   ```

   返回 201 和包的元数据。上传后不对外公开；同名文件已存在返回 409，不允许覆盖。

2. 发布：

   ```bash
   curl -X POST -H "Authorization: Bearer $OTA_ADMIN_TOKEN" -H "Content-Type: application/json" \
     -d '{"device":"diting","channel":"release","version":"17.0-20261008","changelog":"更新说明","file":"uwu_diting-20261008.zip"}' \
     https://ota.example.com/admin/releases
   ```

   服务端检查包内 `pre-device` 包含 `device`，然后写回 `releases.json`，立即生效。

也可以手动把包放进 `files/` 并编辑 `releases.json`（格式见 `releases.example.json`），服务端会自动重新加载。务必先放包、再改清单。

## 注意

- 每个版本用新的文件名，不要覆盖正在被下载的包。
- 旧包不会自动删除，确认没有客户端在下载后再手动清理。
- 首次加载某个包时需要计算 SHA-256，期间其他请求会等待；通过上传接口进来的包已在上传时计算过。
