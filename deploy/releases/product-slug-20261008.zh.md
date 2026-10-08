# 商品创建失败修复（2026-10-08）

北京时间 2026-10-08 15:41 上线，版本 `v1.0.0+product-slug.20261008`，代码提交 `f59099e7f5c69e45b8b62969475da5a2e47cf477`。

## 原因与修改

已删除商品仍占用全局唯一 Slug，但应用只检查未删除商品，导致用 `chatgpt-plus` 重新创建时触发 PostgreSQL `23505 / idx_products_slug`，前台显示“创建商品失败”。

商品模型改为仅对 `deleted_at IS NULL` 的记录建立唯一索引 `idx_products_active_slug`；启动迁移先建立新索引，再移除旧索引。历史商品及 ID、Slug 均保留，新建商品使用新 ID 和 SKU。在用商品继续禁止重名。仅发布这项后端修复，工作区其他未提交修改未包含在镜像中。

## 验证

- 修复前回归测试复现删除后重建的唯一键冲突。
- Linux Go 1.26.5：`go test -p 1 ./internal/modules/catalog/product/... ./internal/bootstrap/database/migrations -count=1` 全部通过。
- 独立 PostgreSQL 库通过后台 API 复现旧版错误；升级后同名重建、新 SKU、重复删除重建、在用 Slug 拦截、重启迁移全部通过，测试库已删除。
- 生产数据库事务验证 `chatgpt-plus` 可重用、在用 Slug 仍受唯一约束；事务回滚，商品数据指纹未变，没有留下测试商品。
- 公网 HTTPS 首页、健康检查、公共配置、商品列表均 HTTP 200；公共配置返回修复版本。应用容器健康，启动后容器日志未发现 error/fatal/panic。数据库与 Redis 容器、运行配置保持原样。

## 发布与恢复记录

- 镜像：`dujiao-next:fix-product-slug-20261008`。
- 镜像 ID：`sha256:a5710d138e588abfbbce9320953c7056e22df0c6865ada9ded359a0c8911f3eb`。
- 旧镜像：`sha256:c70aa645b6e8e0a3885af66e2fb1c480dd5ff2113b7c27f812a1ee866392a253`。
- 部署前备份：`/opt/dujiao-next/backups/dujiao-next-20261008T074110Z.tar.gz`；PostgreSQL 备份已恢复至临时库验证，共 61 张表。
- 服务端证据、源码、测试及构建日志：`/opt/dujiao-next/releases/product-slug-20261008/`；运行记录：`/opt/dujiao-next/deployed.json`。

回退旧二进制时，需要先用旧索引名创建同样的部分唯一索引，避免旧版自动迁移重新建立全局唯一索引。此兼容步骤和旧版重启已在独立 PostgreSQL 库验证。生产未执行回退，也未恢复旧数据库。

```bash
# 只读预检
python3 /opt/dujiao-next/releases/product-slug-20261008/rollback.py --check

# 需要回退时执行；保留当前业务数据和仅在用商品唯一的规则
python3 /opt/dujiao-next/releases/product-slug-20261008/rollback.py
```

本次没有调整 ESA 网络路径；此前另行发现的网络连接问题不在这项修复内。
