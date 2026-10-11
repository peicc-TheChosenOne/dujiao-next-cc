# Docker Compose 部署指南

本目录提供通用部署模板，不记录任何实际服务器、账号、证书、备份或上线结果。域名 `example.com`、数据库 `appdb` 和数据库用户 `app` 均为示例。

## 准备配置

1. 安装 Docker Engine 和 Docker Compose，选择已发布的代码标签，在仓库根目录构建应用镜像：

   ```bash
   git checkout <发布标签>
   docker build --build-arg APP_VERSION=<发布标签> -t dujiao-next:<发布标签> .
   ```

2. 将本目录的 `.env.example` 复制为 `.env`，把 `APP_IMAGE` 设置为刚构建的镜像；生产环境应固定经过验证的镜像版本或摘要。
3. 在本目录创建 `config/`、`secrets/`、`data/uploads/` 和 `data/logs/`。将根目录 `config.yml.example` 复制到 `config/config.yml`，填写实际配置。
4. 分别生成 PostgreSQL 管理密码、应用数据库密码和 Redis 密码，保存为 `secrets/postgres_password`、`secrets/app_db_password`、`secrets/redis_password`。密码文件只保存密码本身，不提交到 Git。
5. 在 `config/redis-users.acl` 中配置 Redis 用户，例如 `user default on >替换为Redis密码 ~* &* +@all`。ACL 密码与 Redis 密码文件保持一致。

应用配置需要同步调整：

- `app.secret_key`、`jwt.secret`、`user_jwt.secret`：分别使用独立的强随机值。
- `database.driver`：`postgres`；DSN 使用容器主机 `postgres`、端口 `5432`、示例数据库 `appdb` 和示例用户 `app`，密码与应用数据库密码文件一致。若调整数据库名称或用户，须同步修改 Compose 和初始化脚本。
- 数据库连接池：按实际资源设置；不要沿用 SQLite 的单连接配置。
- `redis`、`queue`：启用并使用容器主机 `redis`、端口 `6379` 和对应密码；缓存与队列使用不同 DB。
- `bootstrap`：配置首次初始化管理员。已有管理员不会因重启重新初始化。
- `server.mode`：生产使用 `release`；可信代理列表须依据实际网络填写。
- `web.admin_path`、站点域名、SMTP 和其他业务设置：按自己的环境配置。

建议使用 `openssl rand -hex 32` 分别生成密码与密钥，配置文件权限设置为仅管理员可读。

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

## 运维记录与发布

实际服务器地址、账号、后台入口、访问策略、证书、密钥、备份路径和部署验收结果应保存在仓库外的私有运维目录。不要将实际记录填回本指南或其他公开文档。

升级与回滚步骤见 [发布流程](发布流程.zh.md)。`backup.sh`、`renew-cert.sh` 是脚本模板，使用前须核对示例目录、域名、证书来源和保留策略，不应直接套用。
