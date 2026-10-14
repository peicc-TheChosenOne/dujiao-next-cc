# Docker Compose 部署指南

本目录提供通用部署模板，不记录任何实际服务器、账号、证书、备份或上线结果。域名 `example.com`、数据库 `appdb` 和数据库用户 `app` 均为示例。

## 准备配置

1. 安装 Docker Engine 和 Docker Compose，选择已发布的代码标签，在仓库根目录构建应用镜像：

   ```bash
   git checkout <发布标签>
   docker build --build-arg APP_VERSION=<发布标签> -t dujiao-next:<发布标签> .
   ```

2. 将本目录的 `.env.example` 复制为 `.env`，把 `APP_IMAGE` 设置为刚构建的镜像；生产环境应固定经过验证的镜像版本或摘要。
3. 在本目录创建 `config/`、`secrets/`、`data/uploads/` 和 `data/logs/`。将根目录 `config.yml.example` 复制到 `config/config.yml`，将 `config.private.yml.example` 复制到 `secrets/config.private.yml`。普通参数写在前者，真实连接地址、密码、JWT 和加密密钥写在后者；两者只保存在部署机器，不进入仓库或镜像。
4. 分别生成 PostgreSQL 管理密码、应用数据库密码和 Redis 密码，保存为 `secrets/postgres_password`、`secrets/app_db_password`、`secrets/redis_password`。密码文件只保存密码本身，不提交到 Git。
5. 在 `config/redis-users.acl` 中配置 Redis 用户，例如 `user default on >替换为Redis密码 ~* &* +@all`。ACL 密码与 Redis 密码文件保持一致。

应用配置需要同步调整，下面的敏感值写入 `secrets/config.private.yml`：

- `app.secret_key`、`jwt.secret`、`user_jwt.secret`：分别使用独立的强随机值。
- `database.driver`：`postgres`；DSN 使用容器主机 `postgres`、端口 `5432`、示例数据库 `appdb` 和示例用户 `app`，密码与应用数据库密码文件一致。若调整数据库名称或用户，须同步修改 Compose 和初始化脚本。
- 数据库连接池：按实际资源设置；不要沿用 SQLite 的单连接配置。
- `redis`、`queue`：启用并使用容器主机 `redis`、端口 `6379` 和对应密码；缓存与队列使用不同 DB。
- `bootstrap`：配置首次初始化管理员。已有管理员不会因重启重新初始化。
- `server.mode`：生产使用 `release`；可信代理列表须依据实际网络填写。
- `web.admin_path`、站点域名、SMTP 和其他业务设置：按自己的环境配置。

首次部署建议使用 `openssl rand -hex 32` 分别生成密码与密钥，私有文件权限设为 `0600`，并确保运行用户能读取。已有环境拆分配置时原值迁移，尤其不得重新生成 `app.secret_key`；数据库中的历史加密数据依赖该密钥。

应用按默认值、`config.yml`、`config.private.yml`、环境变量的顺序覆盖配置。Compose 只读挂载普通配置和私有配置。服务器自己的两份配置不随版本升级覆盖；本地开发机保留自己的两份文件，分支合并只同步代码和脱敏模板。旧版单文件部署仍兼容；要采用本模板的新挂载项，先准备私有文件再启动容器。

## 启动与访问

在本目录执行：

```bash
docker compose config --quiet
docker compose up -d
docker compose ps
curl --fail http://127.0.0.1:8080/health
```

模板的应用端口绑定到本机回环地址，由同机 Nginx 提供 HTTPS 访问。`nginx.conf` 和 `nginx-app.conf` 中的域名、证书路径及站点目录都是示例，启用前必须替换。

首次创建 PostgreSQL 数据目录时才会执行 `postgres-init.sh`；已有数据目录不会再次初始化。应用启动时仍会按代码执行数据库迁移，升级前应备份并检查迁移兼容性。

## 客户端连接数据库

默认 Compose 不发布数据库端口。需要宿主机客户端连接时，可启用 `compose.public.yml`：

```bash
docker compose -f compose.yml -f compose.public.yml config --quiet
docker compose -f compose.yml -f compose.public.yml up -d
```

该模板默认仅绑定本机回环地址，宿主机端口由 `.env` 中的参数控制。外部连接需另外配置绑定地址、防火墙或安全组、密码认证及传输保护；实际地址和访问策略仅保存在本机配置中。

## 分销商自有域名（Cloudflare for SaaS）

分销商把根域名和 `www` 的 CNAME 指向平台入口、在分销后台提交根域名后，应用通过 Cloudflare API 创建自定义主机名，DNS 和 HTTPS 都就绪后再交给管理员审核。以下以 `example.com` 为平台域名：

1. 平台域名的 NS 托管到 Cloudflare。迁移前导出原有全部解析记录，先在 Cloudflare 建成仅 DNS（灰云），确认未开启 DNSSEC 后再修改 NS；SSH、数据库、邮件等非网页记录始终保持灰云。
2. 在该 zone 开通 Cloudflare for SaaS，前 100 个自定义主机名免费。新建 `origin.example.com` 指向服务器（代理开启）并设为 fallback origin，再建 `entry.example.com` CNAME 到它（代理开启），作为分销商的 CNAME 目标。
3. 该 zone 的 SSL/TLS 模式设为「完全」。自定义主机名回源时 SNI 是分销商域名，平台证书无法匹配，因此不用「完全（严格）」，改为在源站只接受 Cloudflare 的连接。
4. 源站 Nginx 参照 `nginx-cloudflare-saas.conf.example` 增加 `default_server`：只放行 Cloudflare 官方公布的网段，用 `CF-Connecting-IP` 还原访客 IP，原样传递 Host。网段列表放在服务器本地的 include 文件中；源站证书可使用 Cloudflare Origin CA。全部稳定后，云服务器安全组的 80/443 也只放行 Cloudflare 网段。
5. 新建 API Token，只授予该 zone 的「SSL and Certificates: Edit」，写入 `secrets/config.private.yml` 的 `reseller.domain_binding.cloudflare.api_token`。
6. 在 `config/config.yml` 中启用 `reseller.domain_binding`，填写 `cname_target: entry.example.com` 和 zone ID（在 Cloudflare 该 zone 概览页右侧），`custom_origin` 留空；`reseller.enabled`、`queue.enabled` 必须为 `true`，`reseller.trusted_forwarded_host` 保持 `false`。
7. 用一个开启代理的测试域名（SSL 设为「完全（严格）」）和一个未开启代理的测试域名各走一遍提交、自动验证、审核、访问和禁用。

默认每个分销商最多绑定 5 组根域名 / www，平台自动接入上限为 100 个主机名；未验证域名在 7 天后禁用并清理。可用 domain_binding 中的额度与到期配置调整，但提高总额度前必须核对 Cloudflare 计费。

服务器迁移时只需修改 `origin.example.com` 等平台记录指向的地址，分销商的 CNAME 和已创建的自定义主机名不受影响。

## 运维记录与发布

实际服务器地址、账号、后台入口、访问策略、证书、密钥、备份路径和部署验收结果应保存在仓库外的私有运维目录。不要将实际记录填回本指南或其他公开文档。

升级与回滚步骤见 [发布流程](发布流程.zh.md)。`backup.sh`、`renew-cert.sh` 是脚本模板，使用前须核对示例目录、域名、证书来源和保留策略，不应直接套用。
