# Rom-Updater

ROM OTA 更新服务端，Go 标准库实现，无第三方依赖。

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

默认使用完整版本号 `0.2.2`。升级到其他版本时，在 `.env` 设置 `OTA_IMAGE_TAG=X.Y.Z` 后再执行升级命令。正式镜像由 `vX.Y.Z` Git 标签发布，对应镜像标签 `X.Y.Z`；`latest` 与 `X.Y` 是可变别名，`edge` 是 `main` 分支开发构建。

### Docker

```bash
docker run -d --name ota -p 8080:8080 \
  -v ota-data:/data \
  -e OTA_ADMIN_TOKEN=换成随机长字符串 \
  ghcr.io/night-stars-1/rom-updater:0.2.2 \
  -base-url https://ota.example.com
```

容器监听 8080 端口、使用纯 HTTP，需在前面放 HTTPS 反向代理。数据目录 `/data`：

```
/data/releases.json   发布清单（不存在时为空，可通过发布接口生成）
/data/files/*.zip     OTA 包
```

### 直接运行

```bash
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

## 发布新 ROM

最简单的方式是打开 `https://ota.example.com/admin/`，用 `OTA_ADMIN_TOKEN` 登录后上传并发布。

后台登录使用固定账号 `admin`，管理令牌作为密码。认证成功后，支持 Credential Management API 的浏览器会收到密码保存请求；其他浏览器通过标准用户名/密码字段识别凭据。是否显示保存提示由浏览器设置和站点保存策略决定，生产环境应使用 HTTPS。登录会话仍只保存在当前标签页的 `sessionStorage`，退出会清除；密码管理器中保存的凭据由浏览器管理。

网页下拉菜单、文件选择按钮、上传进度、日期时间选择和操作确认均使用统一的自定义组件，支持键盘操作。构建时间按浏览器本地时区选择，发布时转换为 Unix 秒。系统文件选择窗口和上传期间离开页面的安全提示仍由浏览器提供。

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
