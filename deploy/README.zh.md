# 生产环境部署

当前服务器：`8.218.23.21`，2 核 / 4 GB，宝塔管理 Nginx。
部署目录：`/opt/dujiao-next`，Compose 项目名：`dujiao-next`。

## 已安装的服务

| 服务 | 已验证版本 | 容器名 | 内网地址 |
| --- | --- | --- | --- |
| 应用（前台 / 后台 / API / Worker） | 当前仓库源码构建 | dujiao-next | app:8080 |
| PostgreSQL | 17.11 | postgres | postgres:5432 |
| Redis | 7.4.11 | redis | redis:6379 |

服务都连接到 `dujiao-next-network`；应用端口仅发布到 `127.0.0.1:8080`。
基础编排不发布数据库端口；可通过 `compose.public.yml` 启用下文的 TLS 客户端连接。
宝塔 Docker 容器列表可以查看三个容器。
服务器 `.env` 固定了实际拉取的镜像摘要，避免镜像标签变化影响重建。

当前应用版本为 `sha-f71e07974027`，对应源码提交 `f71e079740279b4aa077e9836fd8c03153c75179`。
已验证 HTTPS 健康检查、前台 / 后台及其静态资源、管理员登录、公共商品接口和未登录访问限制。
首次启动完成 61 张业务表的迁移并初始化管理员；站点地址已设置为 `https://aiccpay.com`。

## 配置与数据位置

| 服务器路径 | 用途 |
| --- | --- |
| `/opt/dujiao-next/compose.yml` | 服务编排、健康检查、资源限制、重启策略 |
| `/opt/dujiao-next/compose.public.yml` | PostgreSQL / Redis 的 TLS 端口与证书挂载 |
| `/opt/dujiao-next/.env` | 固定的应用 / PostgreSQL / Redis 镜像版本 |
| `/opt/dujiao-next/config/config.yml` | 应用完整配置，包含凭证和运行密钥 |
| `/opt/dujiao-next/secrets/` | 随机生成的 PostgreSQL 管理密码、应用数据库密码、Redis 密码 |
| `/opt/dujiao-next/data/postgres/` | PostgreSQL 数据 |
| `/opt/dujiao-next/data/redis/` | Redis AOF / RDB 数据 |
| `/opt/dujiao-next/data/uploads/` | 应用上传目录 |
| `/opt/dujiao-next/data/logs/` | 应用日志目录 |
| `/opt/dujiao-next/backups/` | 数据库、Redis、应用与 Nginx 配置、密钥和上传文件的备份 |

凭证只保存在服务器，未放进本仓库。不要提交运行时配置或私钥。
Compose 文件中的 secrets 是本机文件挂载；密码文件不通过环境变量或启动参数传递。

## 资源配置

- PostgreSQL：内存上限 768 MiB、共享缓冲 128 MiB、最大连接 50。
- 应用数据库账号 `dujiao`：拥有 `dujiao_next` 数据库，不拥有超级用户、创建数据库或创建角色权限。
- 应用连接池：最大连接 20、空闲连接 5、连接最长使用 1200 秒、空闲 300 秒。
- Redis：内存上限 512 MiB，数据预算 192 MiB，为 AOF 缓冲和快照留出空间。
- Redis 开启 AOF，每秒同步，使用 `noeviction`。达到数据预算时拒绝新增写入，避免任务队列被缓存淘汰策略删除；需要根据后续队列积压调整预算。
- 应用缓存使用 Redis DB 0，任务队列使用 DB 1，任务并发数 5。
- 应用容器内存上限 1 GiB、CPU 上限 1.5 核；前台、后台、API 和 Worker 运行在同一容器。
- 应用配置采用 `release` 模式；三个运行密钥互不相同，管理员密码和后台路径均为随机生成。
- 当前可信代理包含本机回环与 Docker 网关 `172.19.0.1/32`。更换网络后须核对实际网关。
- SMTP 尚未配置，因此邮件功能暂时关闭。按当前选择保留注册邮箱验证码验证，配置 SMTP 前，验证码注册和找回密码不可用。
- 按 Redis 官方建议，将宿主机 `vm.overcommit_memory` 持久设置为 1，并通过 `dujiao-next-host-tuning.service` 在启动时关闭透明大页。

## 常用操作

在服务器执行：

```bash
cd /opt/dujiao-next
docker compose ps
docker compose logs --tail=100 app postgres redis
docker compose up -d --wait
```

PostgreSQL 管理终端：

```bash
cd /opt/dujiao-next
docker compose exec postgres psql -U postgres -d dujiao_next
```

Redis 认证检查：

```bash
cd /opt/dujiao-next
docker compose exec redis sh -c 'REDISCLI_AUTH="$(cat /run/secrets/redis_password)" redis-cli ping'
```

应用挂载 `./config/config.yml:/app/config.yml:ro`，应用端口绑定 `127.0.0.1:8080`，由宝塔 Nginx 反代。

## 数据库客户端公网连接

服务器已启用下列公网端口，阿里云安全组已放行。
已通过电脑上的 HTTP 代理链路验证两个公网端口的 TLS 1.3、密码认证，
并确认 PostgreSQL 拒绝公网非 TLS 连接、Redis 拒绝未认证请求。
当前电脑不经过代理直连服务器仍超时，抓包显示服务器收到请求并发送回包，
需要继续排查回程或本机网络路径；尚不能确认这台电脑的 Navicat 直连成功。
按当前选择，公网来源不设 IP 白名单，使用密码与 TLS 认证。

| 客户端参数 | PostgreSQL | Redis |
| --- | --- | --- |
| 主机 | `aiccpay.com` | `aiccpay.com` |
| 公网端口 | `15432` | `16379` |
| 用户 | `dujiao` | `default` |
| 数据库 | `dujiao_next` | DB 0（缓存）/ DB 1（任务队列） |
| SSL / TLS | 启用，验证 CA 与主机名 | 启用，验证 CA 与主机名 |

密码沿用原有服务器凭证，不写入仓库。Navicat 的 SSH 隧道关闭，SSL 页启用加密和证书验证；
使用系统可信 CA 或提供的 CA 文件，客户端证书与客户端私钥留空。证书覆盖域名，主机填写 `aiccpay.com`。

服务器 `.env` 使用 `COMPOSE_FILE=compose.yml:compose.public.yml`；
`DB_PUBLIC_BIND_IP=0.0.0.0` 启用 IPv4 公网映射，改为 `127.0.0.1` 后重建数据库容器可收回公网映射。
阿里云安全组放行 TCP `15432`、`16379`；Docker 端口发布可能绕过 UFW，公网范围以安全组为准。
PostgreSQL 的 `config/pg_hba.conf` 只允许公网使用 TLS、`dujiao` 账号和 `dujiao_next` 数据库，
公网非 TLS 连接及其他数据库账号被拒绝。Redis 的公网端口映射到 TLS `6380`；
应用仍在 Docker 内网访问 PostgreSQL `5432`、Redis `6379`。

数据库证书是 `tls/aliyun/` 中站点证书的副本，分别放在 `tls/postgres/` 和 `tls/redis/`。
更新阿里云证书时需要同步两份副本；目录和文件所有者使用相应容器服务用户的 UID/GID，
私钥权限为 `600`，证书为 `644`。随后重建 PostgreSQL / Redis 并验证客户端 TLS 连接。
`redis.conf` 权限为 `644`，密码单独保存在 secrets 文件中。

```bash
cd /opt/dujiao-next
docker compose up -d --no-deps --wait --wait-timeout 180 postgres redis
curl --fail http://127.0.0.1:8080/health
```

## 源码构建与更新

部署分支为 `codex/deploy-aiccpay`。服务器使用该分支指定提交的源码快照构建，
Dockerfile 会编译两套前端并嵌入 Go 二进制，构建时启用 `release,fullstack`。
镜像标签包含源码提交号，服务器 `.env` 的 `APP_IMAGE` 使用构建完成的镜像 ID，避免标签被覆盖后部署内容改变。
SSH 私钥、运行配置和数据库密码不进入 GitHub 仓库或构建上下文。

在源码快照目录构建（将 `COMMIT` 替换为实际完整提交号）：

```bash
docker build --build-arg APP_VERSION=sha-COMMIT --tag dujiao-next:sha-COMMIT .
docker image inspect dujiao-next:sha-COMMIT --format '{{.Id}}'
```

更新前先备份数据库，记录旧的 `APP_IMAGE`；确认新镜像后修改 `.env`，执行：

```bash
cd /opt/dujiao-next
./backup.sh
docker compose up -d --no-deps --wait --wait-timeout 180 app
curl --fail http://127.0.0.1:8080/health
```

首次启动会执行数据库迁移和管理员初始化。镜像版本及源码提交记录在
`/opt/dujiao-next/deployed.json`。回退旧镜像前须确认数据库迁移兼容；不兼容时需一起恢复部署前数据库备份。

## 域名与 HTTPS

入口为 `https://aiccpay.com`；HTTP 和 `https://www.aiccpay.com` 都跳转到此地址。
宝塔站点配置位于 `/www/server/panel/vhost/nginx/aiccpay.com.conf`，保留已配置的证书和跳转规则。
应用反代通过 `/www/server/panel/vhost/nginx/extension/aiccpay.com/dujiao-app.conf` 接入，
内容对应本目录的 `nginx-app.conf`。它同时保留 `.well-known` 文件验证目录。
`nginx.conf` 是完整反代配置参考，更新应用反代时不需要覆盖宝塔主站点配置。

使用已从阿里云取得并上传到 `/ssl` 的 DigiCert 证书，覆盖 `aiccpay.com` 和 `www.aiccpay.com`。
证书链和匹配的私钥已部署到 `/opt/dujiao-next/tls/aliyun/`，并通过软链接接入宝塔标准证书目录：

- `/www/server/panel/vhost/cert/aiccpay.com/fullchain.pem`
- `/www/server/panel/vhost/cert/aiccpay.com/privkey.pem`

私钥权限为 `600`。证书到期时间为北京时间 **2027-01-01 07:59:59**。
当前使用的阿里云证书需要从阿里云续期或重新签发后替换，再执行 `nginx -t` 和重载。
此前准备的 `renew-cert.sh` 仅针对备用的 Let's Encrypt 证书，不会续期当前阿里云证书；没有启用续期定时任务。

```bash
/www/server/nginx/sbin/nginx -t
/www/server/nginx/sbin/nginx -s reload
curl -I https://aiccpay.com
curl -I https://www.aiccpay.com
```

## 备份

每天北京时间 03:00，通过 `dujiao-next-backup.timer` 执行备份，保留 14 天。
备份包含 PostgreSQL 自定义格式导出、Redis RDB 快照、应用配置、Nginx 站点和扩展配置、凭证、证书和上传文件。
备份文件权限限制为 root 可读写；目前保存在同一台服务器，正式上线时应增加异地备份。

```bash
systemctl list-timers dujiao-next-backup.timer
systemctl start dujiao-next-backup.service
journalctl -u dujiao-next-backup.service --since today
```

安装时已验证密码认证、业务账号权限、容器重建后的 PostgreSQL / Redis 数据持久化，
并把 PostgreSQL 备份恢复到临时数据库检查，随后清理测试数据。
另外使用独立临时容器检查内网 DNS 和密码连接，并验证 Redis 备份 RDB 校验和。
PostgreSQL 导出不包含角色，恢复到新服务器时需先准备同名应用账号和数据库，再导入对象。
应用加密根密钥必须与对应数据库备份一起恢复，不可随意更换。

## 参考

- [独角 Next Docker Compose 部署](https://dujiao-next.com/deploy/docker-compose)
- [独角 Next 配置建议](https://dujiao-next.com/config/config-yml)
- [独角 Next 备份与恢复](https://dujiao-next.com/deploy/backup)
- [Redis 生产环境配置建议](https://redis.io/docs/latest/operate/oss_and_stack/management/admin/)

官方示例使用 PostgreSQL 16；本部署使用此前选定的 PostgreSQL 17，并完成了数据库基础验证。
应用发布时检查数据库迁移、管理员登录、前台 / 后台页面及公共接口；支付业务需填写渠道参数后另行验证。
